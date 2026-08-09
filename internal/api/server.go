package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/zaolin/framework-powerd/internal/gpu"
	"github.com/zaolin/framework-powerd/internal/ollama"
	"github.com/zaolin/framework-powerd/internal/power"
	"github.com/zaolin/framework-powerd/internal/smartd"

	"github.com/golang-jwt/jwt/v5"
)

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
}

// Server handles API requests
type Server struct {
	pm            *power.PowerManager
	powerMonitor  *power.PowerMonitor
	ollamaMonitor *ollama.Monitor
	smartdMonitor *smartd.Monitor
	gpuMonitor    *gpu.Monitor
	jwtSecret     []byte
}

// NewServer creates a new API server
func NewServer(pm *power.PowerManager, monitor *power.PowerMonitor, jwtSecret string, ollamaMon *ollama.Monitor, smartdMon *smartd.Monitor, gpuMon *gpu.Monitor) *Server {
	return &Server{
		pm:            pm,
		powerMonitor:  monitor,
		ollamaMonitor: ollamaMon,
		smartdMonitor: smartdMon,
		gpuMonitor:    gpuMon,
		jwtSecret:     []byte(jwtSecret),
	}
}

func (s *Server) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// allow localhost without auth for backward compatibility or ease of use?
		// User asked for "jwt authentication", usually implies enforcement.
		// Let's enforce it if a secret is configured.
		if len(s.jwtSecret) == 0 {
			next(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return s.jwtSecret, nil
		})

		if err != nil {
			log.Printf("Auth Error: %v", err)
			http.Error(w, fmt.Sprintf("Invalid token: %v", err), http.StatusUnauthorized)
			return
		}

		if !token.Valid {
			log.Println("Auth Error: Token invalid")
			http.Error(w, "Invalid token", http.StatusUnauthorized)
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to set mode: %v", err)})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Mode set successfully", "mode": req.Mode})
}

func (s *Server) HandleStatus(w http.ResponseWriter, r *http.Request) {
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
	}

	// Include Ollama stats if enabled
	if s.ollamaMonitor != nil {
		stats := s.ollamaMonitor.GetStats()
		resp.Ollama = &stats
	}

	// Include Smartd stats if enabled
	if s.smartdMonitor != nil {
		stats := s.smartdMonitor.GetStats()
		resp.Smartd = &stats
	}

	// Include GPU stats if enabled
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

	s.pm.TriggerActivity()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Activity triggered, idle timer reset"})
}

// HandleOllamaStats returns Ollama usage statistics
func (s *Server) HandleOllamaStats(w http.ResponseWriter, r *http.Request) {
	if s.ollamaMonitor == nil {
		http.Error(w, "Ollama monitoring not enabled", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.ollamaMonitor.GetStats())
}

// HandleSmartdStats returns SMART health statistics
func (s *Server) HandleSmartdStats(w http.ResponseWriter, r *http.Request) {
	if s.smartdMonitor == nil {
		http.Error(w, "Smartd monitoring not enabled", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.smartdMonitor.GetStats())
}
