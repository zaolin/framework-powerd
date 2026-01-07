package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/zaolin/framework-powerd/internal/power"

	"github.com/golang-jwt/jwt/v5"
)

type Server struct {
	pm        *power.PowerManager
	jwtSecret []byte
}

func NewServer(pm *power.PowerManager, jwtSecret string) *Server {
	return &Server{
		pm:        pm,
		jwtSecret: []byte(jwtSecret),
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
	case "auto":
		err = s.pm.AutoDetect()
	default:
		http.Error(w, "Invalid mode. Use 'performance', 'powersave', or 'auto'", http.StatusBadRequest)
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
	connected, _ := s.pm.IsHDMIConnected()
	mode := s.pm.GetCurrentMode()
	status := map[string]interface{}{
		"hdmi_connected": connected,
		"mode":           mode,
	}
	json.NewEncoder(w).Encode(status)
}
