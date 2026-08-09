package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/power"
	"github.com/zaolin/framework-powerd/internal/smartd"
)

// fakeRunner is an injectable execRunner for tests. It never shells out.
type fakeRunner struct {
	available map[string]bool // tools reported as present by LookPath
	runErr    error           // error returned by Run (nil = success)
	lastCall  string          // last command name invoked via Run
}

func (f *fakeRunner) Run(name string, args ...string) error {
	f.lastCall = name
	if f.runErr != nil {
		return f.runErr
	}
	return nil
}

func (f *fakeRunner) LookPath(name string) bool { return f.available[name] }

// withRunner swaps power.commandRunner for the test and restores it on cleanup.
func withRunner(t *testing.T, r power.ExecRunner) {
	t.Helper()
	orig := power.CommandRunner()
	power.SetCommandRunner(r)
	t.Cleanup(func() { power.SetCommandRunner(orig) })
}

func TestNewServer(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()

	server := NewServer(pm, powerMon, "", nil, nil, nil)

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
	server := NewServer(pm, powerMon, secret, nil, nil, nil)

	if string(server.jwtSecret) != secret {
		t.Error("JWT secret not set correctly")
	}
}

func TestNewServer_WithOllamaAndSmartd(t *testing.T) {
	pm := power.NewPowerManager()
	powerMon := power.NewPowerMonitor()

	server := NewServer(pm, powerMon, "", nil, nil, nil)

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
	server := NewServer(pm, powerMon, "", nil, nil, nil)

	// Inject a fake runner so powerprofilesctl "succeeds" without shelling out.
	withRunner(t, &fakeRunner{available: map[string]bool{"powerprofilesctl": true}})

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
	server := NewServer(pm, powerMon, "", nil, nil, nil)

	withRunner(t, &fakeRunner{available: map[string]bool{"powerprofilesctl": true}})

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

// TestHandleMode_PowerProfilectlFails verifies A7: when powerprofilesctl fails,
// SetPerformance returns the error and HandleMode responds 500 instead of
// falsely reporting success.
func TestHandleMode_PowerProfilectlFails(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil)

	withRunner(t, &fakeRunner{
		available: map[string]bool{"powerprofilesctl": true},
		runErr:    errPowerProfilectl,
	})

	req := ModeRequest{Mode: "performance"}
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/mode", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleMode(w, httpReq)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status 500 when powerprofilesctl fails, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid error body: %v", err)
	}
	if !strings.Contains(resp["error"], "power profile") {
		t.Errorf("expected error to mention power profile, got %q", resp["error"])
	}

	// currentMode must NOT have been committed on failure.
	if got := pm.GetCurrentMode(); got == "performance" {
		t.Errorf("mode must not advance to 'performance' when the primary command failed, got %q", got)
	}
}

var errPowerProfilectl = &errString{"simulated powerprofilesctl failure"}

type errString struct{ s string }

func (e *errString) Error() string { return e.s }

func TestHandleMode_Invalid(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil)

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
	server := NewServer(pm, powerMon, "", nil, nil, nil)

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
	server := NewServer(pm, powerMon, "", nil, nil, nil)

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
	server := NewServer(pm, powerMon, "", nil, nil, nil)

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
	server := NewServer(pm, powerMon, "", nil, nil, nil)

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
	server := NewServer(pm, powerMon, "", nil, nil, nil)

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

	server := NewServer(pm, powerMon, "", nil, smartdMon, nil)

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
	server := NewServer(pm, powerMon, "", nil, nil, nil)

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
	server := NewServer(pm, powerMon, "secret", nil, nil, nil)

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
	server := NewServer(pm, powerMon, "secret", nil, nil, nil)

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
	server := NewServer(pm, powerMon, "secret", nil, nil, nil)

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

// TestHandleStatus_IncludesIsGameRunning verifies A1: the /status response
// must include is_game_running so the HA integration can use it directly
// instead of inferring it from game_pid > 0.
func TestHandleStatus_IncludesIsGameRunning(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(true, false, true, 4242, false) // idle, not RP, game running, pid 4242, not paused
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil)

	httpReq := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	server.HandleStatus(w, httpReq)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	// A1: is_game_running present and correct.
	gotRunning, ok := resp["is_game_running"]
	if !ok {
		t.Fatal("is_game_running missing from status response")
	}
	if b, _ := gotRunning.(bool); !b {
		t.Errorf("is_game_running = %v, want true", gotRunning)
	}

	// A9: seconds_until_idle must serialize as a JSON number (float64 round-trips).
	if _, ok := resp["seconds_until_idle"]; !ok {
		t.Fatal("seconds_until_idle missing from status response")
	}
}
