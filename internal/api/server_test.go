package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/llm"
	"github.com/zaolin/framework-powerd/internal/nvme"
	"github.com/zaolin/framework-powerd/internal/power"
	"github.com/zaolin/framework-powerd/internal/smartd"
	"github.com/zaolin/framework-powerd/internal/sysinfo"
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

	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

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
	server := NewServer(pm, powerMon, secret, nil, nil, nil, nil, sysinfo.SystemInfo{})

	if string(server.jwtSecret) != secret {
		t.Error("JWT secret not set correctly")
	}
}

func TestNewServer_WithSmartd(t *testing.T) {
	pm := power.NewPowerManager()
	powerMon := power.NewPowerMonitor()

	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

	if server.smartdMonitor != nil {
		t.Error("SmartdMonitor should be nil")
	}
	if server.gpuMonitor != nil {
		t.Error("GPUMonitor should be nil")
	}
	if server.nvmeMonitor != nil {
		t.Error("NVMeMonitor should be nil")
	}
}

func TestHandleMode_Performance(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

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

	if resp["mode"] != "balanced" {
		t.Errorf("Expected mode 'balanced', got '%s'", resp["mode"])
	}
}

func TestHandleMode_Powersave(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

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

	if resp["mode"] != "balanced" {
		t.Errorf("Expected mode 'balanced', got '%s'", resp["mode"])
	}
}

// TestHandleMode_PowerProfilectlFails verifies A7: when powerprofilesctl fails,
// SetPerformance returns the error and HandleMode responds 500 instead of
// falsely reporting success.
func TestHandleMode_PowerProfilectlFails(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

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
	if !strings.Contains(resp["error"], "Failed to set mode") {
		t.Errorf("expected generic error 'Failed to set mode', got %q", resp["error"])
	}

	// currentMode must NOT have been committed on failure. With the balanced
	// pinning, an uncommitted mode reports "" (not "balanced").
	if got := pm.GetCurrentMode(); got == "balanced" {
		t.Errorf("mode must not advance to 'balanced' when the primary command failed, got %q", got)
	}
}

var errPowerProfilectl = &errString{"simulated powerprofilesctl failure"}

type errString struct{ s string }

func (e *errString) Error() string { return e.s }

func TestHandleMode_Invalid(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

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
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

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
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

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
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

	httpReq := httptest.NewRequest(http.MethodGet, "/activity", nil)
	w := httptest.NewRecorder()

	server.HandleActivity(w, httpReq)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandleSmartdStats_NotEnabled(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

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

	server := NewServer(pm, powerMon, "", smartdMon, nil, nil, nil, sysinfo.SystemInfo{})

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
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

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
	server := NewServer(pm, powerMon, "secret", nil, nil, nil, nil, sysinfo.SystemInfo{})

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
	server := NewServer(pm, powerMon, "secret", nil, nil, nil, nil, sysinfo.SystemInfo{})

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
	server := NewServer(pm, powerMon, "secret", nil, nil, nil, nil, sysinfo.SystemInfo{})

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
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

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

// TestRateLimit_BurstExceeded verifies S9: the rate limiter allows a burst of
// 5 requests then returns 429 on the 6th.
func TestRateLimit_BurstExceeded(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

	called := 0
	handler := server.RateLimit(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: got %d, want 200", i, w.Code)
		}
	}
	if called != 5 {
		t.Errorf("handler called %d times after burst, want 5", called)
	}

	// 6th request should get 429.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("6th request: got %d, want 429", w.Code)
	}
	if called != 5 {
		t.Errorf("handler called %d times after rate limit, want 5 (not called)", called)
	}
}

// TestHandleStatus_NilMonitors verifies HandleStatus doesn't panic when
// smartd/gpu monitors are nil.
func TestHandleStatus_NilMonitors(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	server.HandleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

// TestHandleStatus_WrongMethod verifies HandleStatus rejects non-GET.
func TestHandleStatus_WrongMethod(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})
	req := httptest.NewRequest(http.MethodPost, "/status", nil)
	w := httptest.NewRecorder()
	server.HandleStatus(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

// TestHandleSmartdStats_WrongMethod verifies the endpoint rejects non-GET.
func TestHandleSmartdStats_WrongMethod(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})
	req := httptest.NewRequest(http.MethodPost, "/smartd/stats", nil)
	w := httptest.NewRecorder()
	server.HandleSmartdStats(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

// TestAuthMiddleware_UnauthorizedClaim verifies S6: a token with authorized=false
// is rejected.
func TestAuthMiddleware_UnauthorizedClaim(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "secret", nil, nil, nil, nil, sysinfo.SystemInfo{})

	// Generate a token with authorized=false
	claims := jwt.MapClaims{
		"authorized": false,
		"exp":        time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("secret"))

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401 for authorized=false token", w.Code)
	}
}

// TestAuthMiddleware_NonBearerScheme verifies S7: a non-Bearer Authorization
// header is rejected.
func TestAuthMiddleware_NonBearerScheme(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "secret", nil, nil, nil, nil, sysinfo.SystemInfo{})

	handler := server.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Authorization", "Basic some-token")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401 for non-Bearer scheme", w.Code)
	}
}

// TestHandleHealth verifies the unauthenticated health endpoint exposes the
// expected liveness fields and no control-plane secrets.
func TestHandleHealth(t *testing.T) {
	pm := power.NewPowerManager()
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "secret", nil, nil, nil, nil, sysinfo.SystemInfo{DaemonVersion: "1.6.0"})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	server.HandleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.Version != "1.6.0" {
		t.Errorf("version = %q, want 1.6.0", resp.Version)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok during the startup grace period", resp.Status)
	}
	if resp.Monitors.Turbostat != "waiting" {
		t.Errorf("turbostat = %q, want waiting before the first sample", resp.Monitors.Turbostat)
	}
	if resp.Monitors.Smartd || resp.Monitors.GPU {
		t.Errorf("optional monitors should be disabled, got %+v", resp.Monitors)
	}
	if resp.Monitors.LLM != "disabled" {
		t.Errorf("llm = %q, want disabled", resp.Monitors.LLM)
	}
	if resp.MetricsState.Enabled {
		t.Error("metrics persistence should be disabled without a state path")
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Error("health response must not leak the JWT secret")
	}
}

// TestHealthStatus pins the degradation policy, including the LLM monitor.
func TestHealthStatus(t *testing.T) {
	tests := []struct {
		name       string
		turbostat  string
		uptime     time.Duration
		metricsErr string
		llmHealth  string
		nvmeHealth string
		want       string
	}{
		{"all healthy", "ok", time.Minute, "", "ok", nvme.HealthOK, "ok"},
		{"turbostat waiting during grace", "waiting", 10 * time.Second, "", "waiting", "disabled", "ok"},
		{"turbostat waiting after grace", "waiting", time.Minute, "", "waiting", "disabled", "degraded"},
		{"turbostat stale", "stale", time.Minute, "", "ok", "disabled", "degraded"},
		{"metrics error", "ok", time.Minute, "disk full", "ok", "disabled", "degraded"},
		{"llm error", "ok", time.Minute, "", "error", "disabled", "degraded"},
		{"llm waiting", "ok", time.Minute, "", "waiting", "disabled", "ok"},
		{"nvme warning", "ok", time.Minute, "", "ok", "warning", "degraded"},
		{"nvme disabled", "ok", time.Minute, "", "ok", "disabled", "ok"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := healthStatus(tc.turbostat, tc.uptime, tc.metricsErr, tc.llmHealth, tc.nvmeHealth); got != tc.want {
				t.Errorf("healthStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHandleHealth_WrongMethod verifies /healthz rejects non-GET.
func TestHandleHealth_WrongMethod(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	w := httptest.NewRecorder()
	server.HandleHealth(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

// TestHandleStatus_IncludesLifetimeEnergy verifies the lifetime counter is
// exposed in the power status block.
func TestHandleStatus_IncludesLifetimeEnergy(t *testing.T) {
	pm := power.NewPowerManager()
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	server.HandleStatus(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	powerData, ok := resp["power"].(map[string]any)
	if !ok {
		t.Fatal("power block missing from status response")
	}
	if _, ok := powerData["lifetime_energy_kwh"]; !ok {
		t.Error("lifetime_energy_kwh missing from power status")
	}
}

// TestRateLimit_PerHostNotPerConnection verifies the limiter keys on the
// client IP, not the ephemeral source port.
func TestRateLimit_PerHostNotPerConnection(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})

	called := 0
	handler := server.RateLimit(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = fmt.Sprintf("1.2.3.4:%d", 1000+i)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: got %d, want 200", i, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:2000"
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("6th request from a new port: got %d, want 429", w.Code)
	}
	if called != 5 {
		t.Errorf("handler called %d times, want 5", called)
	}
}

// TestHandleStatus_IncludesMemory verifies the dynamic memory block is exposed.
func TestHandleStatus_IncludesMemory(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	server.HandleStatus(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	memory, ok := resp["memory"].(map[string]any)
	if !ok {
		t.Fatal("memory block missing from status response")
	}
	total, _ := memory["total_bytes"].(float64)
	used, _ := memory["used_bytes"].(float64)
	if total <= 0 {
		t.Errorf("memory.total_bytes = %v, want > 0", total)
	}
	if used < 0 || used > total {
		t.Errorf("memory.used_bytes = %v, want between 0 and %v", used, total)
	}
}

// TestHandleHealth_IncludesMemory verifies the health payload carries the
// memory pressure value.
func TestHandleHealth_IncludesMemory(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	server.HandleHealth(w, req)

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.MemoryUsedPercent < 0 || resp.MemoryUsedPercent > 100 {
		t.Errorf("MemoryUsedPercent = %v, want 0..100", resp.MemoryUsedPercent)
	}
}

func TestHandleNVMeStats_NotEnabled(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})
	req := httptest.NewRequest(http.MethodGet, "/nvme/stats", nil)
	w := httptest.NewRecorder()
	server.HandleNVMeStats(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandleNVMeStats_WithMonitor(t *testing.T) {
	nvmeMon := nvme.NewMonitor(config.NVMeConfig{Enabled: true, PollInterval: "15m"})
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nvmeMon, nil, nil, sysinfo.SystemInfo{})

	req := httptest.NewRequest(http.MethodGet, "/nvme/stats", nil)
	w := httptest.NewRecorder()
	server.HandleNVMeStats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var resp nvme.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !resp.Enabled {
		t.Error("expected enabled NVMe stats")
	}
}

func TestHandleNVMeStats_WrongMethod(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})
	req := httptest.NewRequest(http.MethodPost, "/nvme/stats", nil)
	w := httptest.NewRecorder()
	server.HandleNVMeStats(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

func TestHandleHealth_NVMeDisabled(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	server.HandleHealth(w, req)

	var resp HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.Monitors.NVMe != "disabled" {
		t.Errorf("nvme = %q, want disabled", resp.Monitors.NVMe)
	}
}

func TestHandleStatus_IncludesNVMe(t *testing.T) {
	nvmeMon := nvme.NewMonitor(config.NVMeConfig{Enabled: true, PollInterval: "15m"})
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nvmeMon, nil, nil, sysinfo.SystemInfo{})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	server.HandleStatus(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, ok := resp["nvme"]; !ok {
		t.Fatal("nvme block missing from status response")
	}
}

// TestHandleStatus_ModeTunableAndHealth verifies the reported APU profile,
// the internal tunable gate and the embedded health block.
func TestHandleStatus_ModeTunableAndHealth(t *testing.T) {
	pm := power.NewPowerManager()
	pm.SetState(false, false, false, 0, false)
	powerMon := power.NewPowerMonitor()
	server := NewServer(pm, powerMon, "", nil, nil, nil, nil, sysinfo.SystemInfo{})

	withRunner(t, &fakeRunner{available: map[string]bool{"powerprofilesctl": true}})
	if err := pm.SetPerformance("test"); err != nil {
		t.Fatalf("SetPerformance: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	server.HandleStatus(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["mode"] != "balanced" {
		t.Errorf("mode = %v, want balanced", resp["mode"])
	}
	if resp["tunable_mode"] != "performance" {
		t.Errorf("tunable_mode = %v, want performance", resp["tunable_mode"])
	}
	health, ok := resp["health"].(map[string]any)
	if !ok {
		t.Fatal("health block missing from status response")
	}
	if _, ok := health["status"]; !ok {
		t.Error("health.status missing")
	}
	if _, ok := health["monitors"]; !ok {
		t.Error("health.monitors missing")
	}
}

func TestHandleLLMStats_NotEnabled(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})
	req := httptest.NewRequest(http.MethodGet, "/llm/stats", nil)
	w := httptest.NewRecorder()
	server.HandleLLMStats(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandleLLMStats_WithMonitor(t *testing.T) {
	llmMon := llm.NewMonitor(config.LLMConfig{Enabled: true, AccessLog: "/tmp/x.log"}, config.PricingConfig{})
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, llmMon, nil, sysinfo.SystemInfo{})

	req := httptest.NewRequest(http.MethodGet, "/llm/stats", nil)
	w := httptest.NewRecorder()
	server.HandleLLMStats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var resp llm.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !resp.Enabled {
		t.Error("expected enabled LLM stats")
	}
}

func TestHandleLLMStats_WrongMethod(t *testing.T) {
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, sysinfo.SystemInfo{})
	req := httptest.NewRequest(http.MethodPost, "/llm/stats", nil)
	w := httptest.NewRecorder()
	server.HandleLLMStats(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

func TestHandleStatus_IncludesLLM(t *testing.T) {
	llmMon := llm.NewMonitor(config.LLMConfig{Enabled: true, AccessLog: "/tmp/x.log"}, config.PricingConfig{})
	server := NewServer(power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, llmMon, nil, sysinfo.SystemInfo{})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	server.HandleStatus(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, ok := resp["llm"]; !ok {
		t.Fatal("llm block missing from status response")
	}
}
