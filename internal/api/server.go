package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zaolin/framework-powerd/internal/gpu"
	"github.com/zaolin/framework-powerd/internal/llm"
	"github.com/zaolin/framework-powerd/internal/nvme"
	"github.com/zaolin/framework-powerd/internal/power"
	"github.com/zaolin/framework-powerd/internal/smartd"
	"github.com/zaolin/framework-powerd/internal/sysinfo"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

// maxRequestBody limits JSON body size to prevent memory exhaustion (S4).
const maxRequestBody = 1 << 20 // 1 MiB

// rateBucket tracks a per-client limiter plus when it was last seen so stale
// buckets can be pruned from memory.
type rateBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// StatusResponse represents the system status
type StatusResponse struct {
	Mode             string                      `json:"mode"`
	TunableMode      string                      `json:"tunable_mode"`
	IsIdle           bool                        `json:"is_idle"`
	SecondsUntilIdle float64                     `json:"seconds_until_idle"`
	IsGameRunning    bool                        `json:"is_game_running"`
	IsGamePaused     bool                        `json:"is_game_paused"`
	IsRemotePlay     bool                        `json:"is_remote_play"`
	GamePID          int                         `json:"game_pid"`
	Uptime           string                      `json:"uptime"`
	UptimeSeconds    float64                     `json:"uptime_seconds"`
	Power            power.PowerStatus           `json:"power"`
	Memory           sysinfo.MemoryInfo          `json:"memory"`
	NetworkDevices   []power.NetworkDeviceStatus `json:"network_devices"`
	Smartd           *smartd.Stats               `json:"smartd,omitempty"`
	NVMe             *nvme.Stats                 `json:"nvme,omitempty"`
	LLM              *llm.Stats                  `json:"llm,omitempty"`
	GPU              *gpu.Stats                  `json:"gpu,omitempty"`
	SystemInfo       sysinfo.SystemInfo          `json:"system_info"`
	Health           HealthResponse              `json:"health"`
}

// MonitorHealth summarizes the state of the optional background monitors.
type MonitorHealth struct {
	Turbostat string `json:"turbostat"` // ok | stale | waiting
	Smartd    bool   `json:"smartd"`
	NVMe      string `json:"nvme"` // disabled | ok | warning | critical
	LLM       string `json:"llm"`  // disabled | ok | waiting | error
	GPU       bool   `json:"gpu"`
}

// HealthResponse is the unauthenticated /healthz payload.
type HealthResponse struct {
	Status                string                  `json:"status"` // ok | degraded
	Version               string                  `json:"version"`
	UptimeSeconds         float64                 `json:"uptime_seconds"`
	IsIdle                bool                    `json:"is_idle"`
	PowerSampleAgeSeconds float64                 `json:"power_sample_age_seconds"`
	MemoryUsedPercent     float64                 `json:"memory_used_percent"`
	Monitors              MonitorHealth           `json:"monitors"`
	MetricsState          power.PersistenceStatus `json:"metrics_state"`
}

// Server handles API requests
type Server struct {
	pm            *power.PowerManager
	powerMonitor  *power.PowerMonitor
	smartdMonitor *smartd.Monitor
	nvmeMonitor   *nvme.Monitor
	llmMonitor    *llm.Monitor
	gpuMonitor    *gpu.Monitor
	jwtSecret     []byte
	sysInfo       sysinfo.SystemInfo
	startedAt     time.Time

	// rateLimiter per-client token bucket for /mode and /activity (S9).
	rateMu      sync.Mutex
	rateBuckets map[string]*rateBucket
}

// NewServer creates a new API server
func NewServer(pm *power.PowerManager, monitor *power.PowerMonitor, jwtSecret string, smartdMon *smartd.Monitor, nvmeMon *nvme.Monitor, llmMon *llm.Monitor, gpuMon *gpu.Monitor, sysInfo sysinfo.SystemInfo) *Server {
	return &Server{
		pm:            pm,
		powerMonitor:  monitor,
		smartdMonitor: smartdMon,
		nvmeMonitor:   nvmeMon,
		llmMonitor:    llmMon,
		gpuMonitor:    gpuMon,
		jwtSecret:     []byte(jwtSecret),
		sysInfo:       sysInfo,
		startedAt:     time.Now(),
		rateBuckets:   make(map[string]*rateBucket),
	}
}

// writeJSON centralizes JSON responses and prevents caching of live state.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: encode failed: %v", err)
	}
}

// jwtClaims is the typed claims struct enforced by ParseWithClaims (S6).
type jwtClaims struct {
	Authorized bool `json:"authorized"`
	jwt.RegisteredClaims
}

// AuthMiddleware validates a JWT Bearer token with strict claim enforcement (S6, S7).
func (s *Server) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(s.jwtSecret) == 0 {
			next(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// S7: Enforce Bearer scheme — reject non-Bearer headers explicitly.
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))

		// S6: ParseWithClaims with a typed claims struct. jwt/v5 validates
		// exp/nbf/iat automatically via RegisteredClaims; we additionally
		// require the "authorized" claim to be true.
		claims := &jwtClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return s.jwtSecret, nil
		})

		if err != nil || !token.Valid {
			// S12+S17: Log the real error server-side; return generic message.
			if err != nil {
				log.Printf("Auth error: %v", err)
			}
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// S6: Enforce the "authorized" claim.
		if !claims.Authorized {
			log.Println("Auth error: token missing 'authorized' claim")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// RateLimit returns a per-client rate limiter middleware (S9). Allows 1
// request/sec with a burst of 5.
func (s *Server) RateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := clientHost(r.RemoteAddr)
		now := time.Now()

		s.rateMu.Lock()
		bucket, ok := s.rateBuckets[clientIP]
		if !ok {
			bucket = &rateBucket{limiter: rate.NewLimiter(rate.Limit(1), 5)}
			s.rateBuckets[clientIP] = bucket
		}
		bucket.lastSeen = now
		// Prune idle clients so a long-running daemon cannot grow this map
		// without bound.
		if len(s.rateBuckets) > 1024 {
			for ip, b := range s.rateBuckets {
				if now.Sub(b.lastSeen) > 10*time.Minute {
					delete(s.rateBuckets, ip)
				}
			}
		}
		limiter := bucket.limiter
		s.rateMu.Unlock()

		if !limiter.Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// clientHost strips the ephemeral port from a RemoteAddr. Without this, every
// new TCP connection would get its own rate-limit bucket, making the limiter
// ineffective and letting the bucket map grow.
func clientHost(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

type ModeRequest struct {
	Mode string `json:"mode"`
}

func (s *Server) HandleMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// S4: Limit body size.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req ModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var err error
	switch req.Mode {
	case "performance":
		err = s.pm.SetPerformance("API Request")
	case "powersave":
		err = s.pm.SetPowersave("API Request")
	default:
		http.Error(w, "Invalid mode. Use 'performance' or 'powersave'", http.StatusBadRequest)
		return
	}

	if err != nil {
		// S12: Log details server-side, return generic message.
		log.Printf("HandleMode: failed to set mode %q: %v", req.Mode, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to set mode"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Mode set successfully",
		"mode":    "balanced",
	})
}

func (s *Server) HandleStatus(w http.ResponseWriter, r *http.Request) {
	// S14: Enforce GET method.
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.pm.GetStatus()

	resp := StatusResponse{
		Mode:             status.Mode,
		TunableMode:      status.TunableMode,
		IsIdle:           status.IsIdle,
		SecondsUntilIdle: status.SecondsUntilIdle,
		IsGameRunning:    status.IsGameRunning,
		IsGamePaused:     status.IsGamePaused,
		IsRemotePlay:     status.IsRemotePlay,
		GamePID:          status.GamePID,
		Uptime:           power.GetUptime(),
		UptimeSeconds:    power.GetUptimeSeconds(),
		Power:            s.powerMonitor.GetStatus(),
		Memory:           sysinfo.CollectMemory(),
		NetworkDevices:   status.NetworkDevices,
		SystemInfo:       s.sysInfo,
	}

	if s.smartdMonitor != nil {
		stats := s.smartdMonitor.GetStats()
		resp.Smartd = &stats
	}
	if s.nvmeMonitor != nil {
		stats := s.nvmeMonitor.GetStats()
		resp.NVMe = &stats
	}
	if s.llmMonitor != nil {
		stats := s.llmMonitor.GetStats()
		resp.LLM = &stats
	}
	if s.gpuMonitor != nil {
		stats := s.gpuMonitor.GetStats()
		resp.GPU = &stats
	}

	resp.Health = s.buildHealth(status.IsIdle)

	writeJSON(w, http.StatusOK, resp)
}

// buildHealth assembles the health payload for /healthz and /status.
func (s *Server) buildHealth(isIdle bool) HealthResponse {
	age := s.powerMonitor.LastSampleAge()
	turbostat := "waiting"
	sampleAge := 0.0
	if age >= 0 {
		sampleAge = age.Seconds()
		if age <= 15*time.Second {
			turbostat = "ok"
		} else {
			turbostat = "stale"
		}
	}

	uptime := power.GetUptimeSeconds()
	memory := sysinfo.CollectMemory()
	metrics := s.powerMonitor.PersistenceStatus()

	nvmeHealth := "disabled"
	if s.nvmeMonitor != nil {
		nvmeHealth = s.nvmeMonitor.GetStats().HealthSummary()
	}

	llmHealth := "disabled"
	if s.llmMonitor != nil {
		llmHealth = s.llmMonitor.Health()
	}

	status := healthStatus(turbostat, time.Since(s.startedAt), metrics.LastError, llmHealth, nvmeHealth)

	return HealthResponse{
		Status:                status,
		Version:               s.sysInfo.DaemonVersion,
		UptimeSeconds:         uptime,
		IsIdle:                isIdle,
		PowerSampleAgeSeconds: sampleAge,
		MemoryUsedPercent:     memory.UsedPercent,
		Monitors: MonitorHealth{
			Turbostat: turbostat,
			Smartd:    s.smartdMonitor != nil,
			NVMe:      nvmeHealth,
			LLM:       llmHealth,
			GPU:       s.gpuMonitor != nil,
		},
		MetricsState: metrics,
	}
}

// healthStatus derives the overall status from the individual monitors.
// turbostat "waiting" is tolerated during the first 30 seconds of uptime.
func healthStatus(turbostat string, uptime time.Duration, metricsErr, llmHealth, nvmeHealth string) string {
	if turbostat == "stale" ||
		(turbostat == "waiting" && uptime > 30*time.Second) ||
		metricsErr != "" ||
		llmHealth == "error" ||
		(nvmeHealth != "disabled" && nvmeHealth != nvme.HealthOK) {
		return "degraded"
	}
	return "ok"
}

// HandleHealth reports daemon liveness without exposing any control-plane
// data. It is registered without auth and is safe for systemd/monitoring
// probes.
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.pm.GetStatus()
	writeJSON(w, http.StatusOK, s.buildHealth(status.IsIdle))
}

func (s *Server) HandleActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// S4: Drain and limit body even though we don't read it.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	io.Copy(io.Discard, r.Body)

	s.pm.TriggerActivity()

	writeJSON(w, http.StatusOK, map[string]string{"message": "Activity triggered, idle timer reset"})
}

// HandleSmartdStats returns SMART health statistics.
func (s *Server) HandleSmartdStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.smartdMonitor == nil {
		http.Error(w, "Smartd monitoring not enabled", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, s.smartdMonitor.GetStats())
}

// HandleLLMStats returns LLM request accounting statistics.
func (s *Server) HandleLLMStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.llmMonitor == nil {
		http.Error(w, "LLM accounting not enabled", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, s.llmMonitor.GetStats())
}

// HandleNVMeStats returns direct NVMe health statistics.
func (s *Server) HandleNVMeStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.nvmeMonitor == nil {
		http.Error(w, "NVMe monitoring not enabled", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, s.nvmeMonitor.GetStats())
}
