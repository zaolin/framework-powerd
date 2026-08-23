package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/zaolin/framework-powerd/internal/gpu"
	"github.com/zaolin/framework-powerd/internal/ollama"
	"github.com/zaolin/framework-powerd/internal/power"
	"github.com/zaolin/framework-powerd/internal/smartd"
	"github.com/zaolin/framework-powerd/internal/sysinfo"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

// maxRequestBody limits JSON body size to prevent memory exhaustion (S4).
const maxRequestBody = 1 << 20 // 1 MiB

// StatusResponse represents the system status
type StatusResponse struct {
	Mode             string                      `json:"mode"`
	IsIdle           bool                        `json:"is_idle"`
	SecondsUntilIdle float64                     `json:"seconds_until_idle"`
	IsGameRunning    bool                        `json:"is_game_running"`
	IsGamePaused     bool                        `json:"is_game_paused"`
	IsRemotePlay     bool                        `json:"is_remote_play"`
	GamePID          int                         `json:"game_pid"`
	Uptime           string                      `json:"uptime"`
	UptimeSeconds    float64                     `json:"uptime_seconds"`
	Power            power.PowerStatus           `json:"power"`
	NetworkDevices   []power.NetworkDeviceStatus `json:"network_devices"`
	Ollama           *ollama.Stats               `json:"ollama,omitempty"`
	Smartd           *smartd.Stats               `json:"smartd,omitempty"`
	GPU              *gpu.Stats                  `json:"gpu,omitempty"`
	SystemInfo       sysinfo.SystemInfo          `json:"system_info"`
}

// Server handles API requests
type Server struct {
	pm            *power.PowerManager
	powerMonitor  *power.PowerMonitor
	ollamaMonitor *ollama.Monitor
	smartdMonitor *smartd.Monitor
	gpuMonitor    *gpu.Monitor
	jwtSecret     []byte
	sysInfo       sysinfo.SystemInfo

	// rateLimiter per-client token bucket for /mode and /activity (S9).
	rateMu      sync.Mutex
	rateBuckets map[string]*rate.Limiter
}

// NewServer creates a new API server
func NewServer(pm *power.PowerManager, monitor *power.PowerMonitor, jwtSecret string, ollamaMon *ollama.Monitor, smartdMon *smartd.Monitor, gpuMon *gpu.Monitor, sysInfo sysinfo.SystemInfo) *Server {
	return &Server{
		pm:            pm,
		powerMonitor:  monitor,
		ollamaMonitor: ollamaMon,
		smartdMonitor: smartdMon,
		gpuMonitor:    gpuMon,
		jwtSecret:     []byte(jwtSecret),
		sysInfo:       sysInfo,
		rateBuckets:   make(map[string]*rate.Limiter),
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
		clientIP := r.RemoteAddr
		s.rateMu.Lock()
		limiter, ok := s.rateBuckets[clientIP]
		if !ok {
			limiter = rate.NewLimiter(rate.Limit(1), 5)
			s.rateBuckets[clientIP] = limiter
		}
		s.rateMu.Unlock()

		if !limiter.Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to set mode"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Mode set successfully", "mode": req.Mode})
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
		IsIdle:           status.IsIdle,
		SecondsUntilIdle: status.SecondsUntilIdle,
		IsGameRunning:    status.IsGameRunning,
		IsGamePaused:     status.IsGamePaused,
		IsRemotePlay:     status.IsRemotePlay,
		GamePID:          status.GamePID,
		Uptime:           power.GetUptime(),
		UptimeSeconds:    power.GetUptimeSeconds(),
		Power:            s.powerMonitor.GetStatus(),
		NetworkDevices:   status.NetworkDevices,
		SystemInfo:       s.sysInfo,
	}

	if s.ollamaMonitor != nil {
		stats := s.ollamaMonitor.GetStats()
		resp.Ollama = &stats
	}
	if s.smartdMonitor != nil {
		stats := s.smartdMonitor.GetStats()
		resp.Smartd = &stats
	}
	if s.gpuMonitor != nil {
		stats := s.gpuMonitor.GetStats()
		resp.GPU = &stats
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Activity triggered, idle timer reset"})
}

// HandleOllamaStats returns Ollama usage statistics
func (s *Server) HandleOllamaStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.ollamaMonitor == nil {
		http.Error(w, "Ollama monitoring not enabled", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.ollamaMonitor.GetStats())
}

// HandleSmartdStats returns SMART health statistics
func (s *Server) HandleSmartdStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.smartdMonitor == nil {
		http.Error(w, "Smartd monitoring not enabled", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.smartdMonitor.GetStats())
}