package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/power"
	"github.com/zaolin/framework-powerd/internal/smartd"
)

func TestNewServer(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()

	server := NewServer(pm, powerMon, "", nil, nil)

	if server.pm != pm {
		t.Error("PowerManager not set correctly")
	}
	if server.powerMonitor != powerMon {
		t.Error("PowerMonitor not set correctly")
	}
	if len(server.jwtSecret) != 0 {
		t.Error("JWT secret should be empty")
	}
}

func TestNewServer_WithJWT(t *testing.T) {
	pm := power.NewPowerManager()
	powerMon := power.NewPowerMonitor()

	secret := "test-secret"
	server := NewServer(pm, powerMon, secret, nil, nil)

	if string(server.jwtSecret) != secret {
		t.Error("JWT secret not set correctly")
	}
}

func TestNewServer_WithOllamaAndSmartd(t *testing.T) {
	pm := power.NewPowerManager()
	powerMon := power.NewPowerMonitor()

	server := NewServer(pm, powerMon, "", nil, nil)

	if server.ollamaMonitor != nil {
		t.Error("OllamaMonitor should be nil")
	}
	if server.smartdMonitor != nil {
		t.Error("SmartdMonitor should be nil")
	}
}

func TestHandleMode_Performance(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil)

	req := ModeRequest{Mode: "performance"}
	body, _ := json.Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/mode", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleMode(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["mode"] != "performance" {
		t.Errorf("Expected mode 'performance', got '%s'", resp["mode"])
	}
}

func TestHandleMode_Powersave(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil)

	req := ModeRequest{Mode: "powersave"}
	body, _ := json.Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/mode", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleMode(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["mode"] != "powersave" {
		t.Errorf("Expected mode 'powersave', got '%s'", resp["mode"])
	}
}

func TestHandleMode_Invalid(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil)

	req := ModeRequest{Mode: "invalid"}
	body, _ := json.Marshal(req)

	httpReq := httptest.NewRequest(http.MethodPost, "/mode", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleMode(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandleMode_WrongMethod(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil)

	httpReq := httptest.NewRequest(http.MethodGet, "/mode", nil)
	w := httptest.NewRecorder()

	server.HandleMode(w, httpReq)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandleActivity(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil)

	httpReq := httptest.NewRequest(http.MethodPost, "/activity", nil)
	w := httptest.NewRecorder()

	server.HandleActivity(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["message"] != "Activity triggered, idle timer reset" {
		t.Errorf("Unexpected message: %s", resp["message"])
	}
}

func TestHandleActivity_WrongMethod(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil)

	httpReq := httptest.NewRequest(http.MethodGet, "/activity", nil)
	w := httptest.NewRecorder()

	server.HandleActivity(w, httpReq)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandleOllamaStats_NotEnabled(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil)

	httpReq := httptest.NewRequest(http.MethodGet, "/ollama/stats", nil)
	w := httptest.NewRecorder()

	server.HandleOllamaStats(w, httpReq)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestHandleSmartdStats_NotEnabled(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil)

	httpReq := httptest.NewRequest(http.MethodGet, "/smartd/stats", nil)
	w := httptest.NewRecorder()

	server.HandleSmartdStats(w, httpReq)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestHandleSmartdStats_WithMonitor(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()

	smartdCfg := config.SmartdConfig{
		Enabled:       true,
		ServiceUnit:   "smartd.service",
		NotifyService: "notify.mobile_phone",
	}
	smartdMon := smartd.NewMonitor(smartdCfg)

	server := NewServer(pm, powerMon, "", nil, smartdMon)

	httpReq := httptest.NewRequest(http.MethodGet, "/smartd/stats", nil)
	w := httptest.NewRecorder()

	server.HandleSmartdStats(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp smartd.Stats
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.NotifyService != "notify.mobile_phone" {
		t.Errorf("Expected NotifyService 'notify.mobile_phone', got '%s'", resp.NotifyService)
	}
}

func TestAuthMiddleware_NoSecret(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil)

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	httpReq := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	handler(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 (no auth needed), got %d", w.Code)
	}
}

func TestAuthMiddleware_WithSecret_NoToken(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "secret", nil, nil)

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	httpReq := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	handler(w, httpReq)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_WithSecret_InvalidToken(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "secret", nil, nil)

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	httpReq := httptest.NewRequest(http.MethodGet, "/status", nil)
	httpReq.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	handler(w, httpReq)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_WithSecret_ValidToken(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "secret", nil, nil)

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	token := generateTestToken("secret")

	httpReq := httptest.NewRequest(http.MethodGet, "/status", nil)
	httpReq.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func generateTestToken(secret string) string {
	token := jwt.New(jwt.SigningMethodHS256)
	claims := token.Claims.(jwt.MapClaims)
	claims["authorized"] = true
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	tokenString, _ := token.SignedString([]byte(secret))
	return tokenString
}
