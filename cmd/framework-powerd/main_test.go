package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/zaolin/framework-powerd/internal/config"
)

// TestDecidePowerMode_Table verifies T4: the 4-branch priority table for
// decidePowerMode across all meaningful state combinations.
func TestDecidePowerMode_Table(t *testing.T) {
	tests := []struct {
		name       string
		st         stateSnapshot
		isPaused   bool
		wantIdle   bool
		wantPause  bool
		wantResume bool
		wantPerf   bool
		wantDef    bool
	}{
		{
			name:     "idle, no game → powersave",
			st:       stateSnapshot{isIdle: true},
			wantIdle: true,
		},
		{
			name:      "idle + game running → powersave + pause",
			st:        stateSnapshot{isIdle: true, isGameRunning: true, gamePID: 1234},
			isPaused:  false,
			wantIdle:  true,
			wantPause: true,
		},
		{
			name:     "idle + game running + already paused → powersave, no pause",
			st:       stateSnapshot{isIdle: true, isGameRunning: true, gamePID: 1234},
			isPaused: true,
			wantIdle: true,
		},
		{
			name:     "active + game running → performance",
			st:       stateSnapshot{isIdle: false, isGameRunning: true, gamePID: 1234},
			wantPerf: true,
		},
		{
			name:     "active + remote play → performance",
			st:       stateSnapshot{isIdle: false, isRemotePlay: true},
			wantPerf: true,
		},
		{
			name:    "active + desktop → default active",
			st:      stateSnapshot{isIdle: false},
			wantDef: true,
		},
		{
			name:       "active + game paused → resume + performance",
			st:         stateSnapshot{isIdle: false, isGameRunning: true, gamePID: 1234},
			isPaused:   true,
			wantResume: true,
			wantPerf:   true,
		},
		{
			// Priority override: remote play + game + idle → NOT idle
			name:     "remote play + game + idle → performance (override)",
			st:       stateSnapshot{isIdle: true, isRemotePlay: true, isGameRunning: true, gamePID: 1234},
			wantPerf: true,
		},
		{
			// Remote play + game + idle + paused → resume + performance
			name:       "remote play + game + idle + paused → resume + performance",
			st:         stateSnapshot{isIdle: true, isRemotePlay: true, isGameRunning: true, gamePID: 1234},
			isPaused:   true,
			wantResume: true,
			wantPerf:   true,
		},
		{
			// Remote play without game + idle → still idle (no override)
			name:     "remote play + idle + no game → powersave",
			st:       stateSnapshot{isIdle: true, isRemotePlay: true},
			wantIdle: true,
		},
		{
			// Game running but gamePID=0 → don't pause (edge case)
			name:      "idle + game running but PID=0 → powersave, no pause",
			st:        stateSnapshot{isIdle: true, isGameRunning: true, gamePID: 0},
			wantIdle:  true,
			wantPause: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := decidePowerMode(tc.st, tc.isPaused)
			if d.shouldIdle != tc.wantIdle {
				t.Errorf("shouldIdle = %v, want %v", d.shouldIdle, tc.wantIdle)
			}
			if d.shouldPause != tc.wantPause {
				t.Errorf("shouldPause = %v, want %v", d.shouldPause, tc.wantPause)
			}
			if d.shouldResume != tc.wantResume {
				t.Errorf("shouldResume = %v, want %v", d.shouldResume, tc.wantResume)
			}
			if d.shouldPerf != tc.wantPerf {
				t.Errorf("shouldPerf = %v, want %v", d.shouldPerf, tc.wantPerf)
			}
			if d.shouldDefault != tc.wantDef {
				t.Errorf("shouldDefault = %v, want %v", d.shouldDefault, tc.wantDef)
			}
		})
	}
}

// TestDaemonState_Accessors verifies T4: daemonState accessors are thread-safe
// and return correct values.
func TestDaemonState_Accessors(t *testing.T) {
	s := &daemonState{}

	// Initial state
	snap := s.snapshot()
	if snap.isIdle || snap.isRemotePlay || snap.isGameRunning || snap.gamePID != 0 {
		t.Fatalf("initial state should be all-zero, got %+v", snap)
	}

	// Set idle
	s.setIdle(true)
	if !s.snapshot().isIdle {
		t.Error("setIdle(true) failed")
	}
	s.setIdle(false)
	if s.snapshot().isIdle {
		t.Error("setIdle(false) failed")
	}

	// Set remote play
	s.setRemotePlay(true)
	if !s.snapshot().isRemotePlay {
		t.Error("setRemotePlay(true) failed")
	}

	// Set game
	s.setGame(1234)
	snap = s.snapshot()
	if !snap.isGameRunning || snap.gamePID != 1234 {
		t.Errorf("setGame(1234) failed: %+v", snap)
	}

	// Set game to 0 → isGameRunning should be false
	s.setGame(0)
	snap = s.snapshot()
	if snap.isGameRunning || snap.gamePID != 0 {
		t.Errorf("setGame(0) should clear game: %+v", snap)
	}
}

// TestGenerateToken verifies the generateToken function produces a valid JWT
// with the "authorized" claim and a 1-year expiry. Since generateToken reads
// from CLI.Token.Secret and prints to stdout, we capture both.
func TestGenerateToken(t *testing.T) {
	// Set the CLI struct's secret directly.
	CLI.Token.Secret = "test-secret-123"

	// Capture stdout.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	generateToken()

	w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	tokenString := string(buf[:n])
	// Trim trailing newline.
	if len(tokenString) > 0 && tokenString[len(tokenString)-1] == '\n' {
		tokenString = tokenString[:len(tokenString)-1]
	}

	if tokenString == "" {
		t.Fatal("generateToken produced empty output")
	}

	// Parse the token and verify claims.
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte("test-secret-123"), nil
	})

	if err != nil || !token.Valid {
		t.Fatalf("generated token is invalid: %v", err)
	}
	if auth, ok := claims["authorized"].(bool); !ok || !auth {
		t.Error("token missing 'authorized' claim or it is false")
	}
	if _, ok := claims["exp"]; !ok {
		t.Error("token missing exp claim")
	}
}

// TestApplyCLIOverrides verifies that config values survive when a flag is
// absent, and that explicitly provided flags (including ones equal to the old
// defaults) override the config file.
func TestApplyCLIOverrides(t *testing.T) {
	orig := CLI.Serve
	defer func() { CLI.Serve = orig }()

	// No flags provided: the config file wins.
	CLI.Serve.Address = nil
	CLI.Serve.Port = nil
	CLI.Serve.IdleTimeout = nil
	CLI.Serve.JWTSecret = ""

	cfg := config.DefaultConfig()
	cfg.Server.Address = "config-host"
	cfg.Server.Port = 9999
	cfg.Server.IdleTimeout = "10m"
	cfg.Server.JWTSecret = "config-secret"

	applyCLIOverrides(cfg)

	if cfg.Server.Address != "config-host" {
		t.Errorf("address = %q, want config-host", cfg.Server.Address)
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("port = %d, want 9999", cfg.Server.Port)
	}
	if cfg.Server.GetIdleTimeout() != 10*time.Minute {
		t.Errorf("idle timeout = %v, want 10m", cfg.Server.GetIdleTimeout())
	}
	if cfg.Server.JWTSecret != "config-secret" {
		t.Errorf("jwt secret = %q, want config-secret", cfg.Server.JWTSecret)
	}

	// Explicit flags override, even when they equal the built-in defaults.
	address := "localhost"
	port := 8080
	idle := 5 * time.Minute
	CLI.Serve.Address = &address
	CLI.Serve.Port = &port
	CLI.Serve.IdleTimeout = &idle
	CLI.Serve.JWTSecret = "cli-secret"

	applyCLIOverrides(cfg)

	if cfg.Server.Address != "localhost" {
		t.Errorf("address = %q, want localhost", cfg.Server.Address)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.GetIdleTimeout() != 5*time.Minute {
		t.Errorf("idle timeout = %v, want 5m", cfg.Server.GetIdleTimeout())
	}
	if cfg.Server.JWTSecret != "cli-secret" {
		t.Errorf("jwt secret = %q, want cli-secret", cfg.Server.JWTSecret)
	}
}

var wantInitialProfileBackoff = []time.Duration{
	time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	10 * time.Second,
	10 * time.Second,
	10 * time.Second,
	10 * time.Second,
}

func TestInitialProfileBackoffSchedule(t *testing.T) {
	if !slices.Equal(initialProfileBackoff, wantInitialProfileBackoff) {
		t.Fatalf("initialProfileBackoff = %v, want %v", initialProfileBackoff, wantInitialProfileBackoff)
	}
	var total time.Duration
	for _, d := range initialProfileBackoff {
		total += d
	}
	if total != 58*time.Second {
		t.Errorf("total backoff = %s, want 58s", total)
	}
}

func TestApplyInitialState_SucceedsAfterRetries(t *testing.T) {
	attempts := 0
	var waits []time.Duration

	applied, err := applyInitialState(
		func() error {
			attempts++
			if attempts < 3 {
				return fmt.Errorf("power-profiles-daemon not ready")
			}
			return nil
		},
		func() bool { return false },
		func() bool { return false },
		func(d time.Duration) bool {
			waits = append(waits, d)
			return true
		},
	)

	if err != nil {
		t.Fatalf("applyInitialState() error = %v", err)
	}
	if !applied {
		t.Fatal("applyInitialState() applied = false, want true")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if !slices.Equal(waits, wantInitialProfileBackoff[:2]) {
		t.Errorf("waits = %v, want %v", waits, wantInitialProfileBackoff[:2])
	}
}

func TestApplyInitialState_StopsWhenAlreadyApplied(t *testing.T) {
	attempts := 0
	applied, err := applyInitialState(
		func() error {
			attempts++
			return nil
		},
		func() bool { return true },
		func() bool { return false },
		func(time.Duration) bool {
			t.Fatal("wait must not be called when the state is already applied")
			return false
		},
	)

	if err != nil || !applied {
		t.Fatalf("applyInitialState() = (%v, %v), want (true, nil)", applied, err)
	}
	if attempts != 0 {
		t.Errorf("attempts = %d, want 0", attempts)
	}
}

func TestApplyInitialState_StopsWhenAppliedDuringRetry(t *testing.T) {
	attempts := 0
	checks := 0
	calls := 0
	applied, err := applyInitialState(
		func() error {
			attempts++
			return fmt.Errorf("power-profiles-daemon not ready")
		},
		func() bool {
			checks++
			return checks > 1
		},
		func() bool { return false },
		func(time.Duration) bool {
			calls++
			return true
		},
	)

	if err != nil || !applied {
		t.Fatalf("applyInitialState() = (%v, %v), want (true, nil)", applied, err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (a committed mode must stop retries)", attempts)
	}
	if checks != 2 {
		t.Errorf("alreadyApplied calls = %d, want 2", checks)
	}
	if calls != 1 {
		t.Errorf("wait calls = %d, want 1", calls)
	}
}

func TestApplyInitialState_StopsBeforeFirstAttemptOnShutdown(t *testing.T) {
	attempts := 0
	applied, err := applyInitialState(
		func() error {
			attempts++
			return nil
		},
		func() bool { return false },
		func() bool { return true },
		func(time.Duration) bool {
			t.Fatal("wait must not be called after shutdown")
			return false
		},
	)

	if err != nil || applied {
		t.Fatalf("applyInitialState() = (%v, %v), want (false, nil)", applied, err)
	}
	if attempts != 0 {
		t.Errorf("attempts = %d, want 0", attempts)
	}
}

func TestApplyInitialState_ExhaustsRetries(t *testing.T) {
	attempts := 0
	var waits []time.Duration
	applied, err := applyInitialState(
		func() error {
			attempts++
			return fmt.Errorf("attempt %d", attempts)
		},
		func() bool { return false },
		func() bool { return false },
		func(d time.Duration) bool {
			waits = append(waits, d)
			return true
		},
	)

	if err == nil {
		t.Fatal("applyInitialState() error = nil, want the last failure")
	}
	if err.Error() != "attempt 9" {
		t.Errorf("error = %q, want the last attempt", err)
	}
	if applied {
		t.Fatal("applyInitialState() applied = true, want false")
	}
	if attempts != 9 {
		t.Errorf("attempts = %d, want 9", attempts)
	}
	if !slices.Equal(waits, wantInitialProfileBackoff) {
		t.Errorf("waits = %v, want %v", waits, wantInitialProfileBackoff)
	}
}

func TestApplyInitialState_StopsWhenWaitIsInterrupted(t *testing.T) {
	attempts := 0
	calls := 0
	applied, err := applyInitialState(
		func() error {
			attempts++
			return fmt.Errorf("power-profiles-daemon not ready")
		},
		func() bool { return false },
		func() bool { return false },
		func(time.Duration) bool {
			calls++
			return calls < 3
		},
	)

	if err != nil {
		t.Errorf("applyInitialState() error = %v, want nil after shutdown", err)
	}
	if applied {
		t.Error("applyInitialState() applied = true, want false")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if calls != 3 {
		t.Errorf("wait calls = %d, want 3", calls)
	}
}

func systemdDirectives(data []byte) map[string][]string {
	// Join continuation lines before splitting, so a wrapped directive cannot
	// hide a dependency from the test.
	text := strings.ReplaceAll(string(data), "\\\n", " ")
	directives := map[string][]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		directives[key] = append(directives[key], strings.Fields(value)...)
	}
	return directives
}

func TestSystemdUnitDoesNotOrderAfterPowerProfilesDaemon(t *testing.T) {
	data, err := os.ReadFile("../../configs/systemd/framework-powerd.service")
	if err != nil {
		t.Fatal(err)
	}
	directives := systemdDirectives(data)

	for _, unit := range directives["After"] {
		if unit == "power-profiles-daemon.service" || unit == "power-profiles-daemon" {
			t.Fatalf("unit must not order after %s", unit)
		}
	}
	if !contains(directives["After"], "network.target") {
		t.Error("unit must keep After=network.target")
	}
	if !contains(directives["Wants"], "power-profiles-daemon.service") {
		t.Error("unit must still want power-profiles-daemon.service")
	}
}

func TestSystemdDirectiveParser(t *testing.T) {
	tests := []struct {
		name string
		unit string
		key  string
		want []string
	}{
		{
			name: "continued line",
			unit: "[Unit]\nAfter=network.target \\\n power-profiles-daemon.service\n",
			key:  "After",
			want: []string{"network.target", "power-profiles-daemon.service"},
		},
		{
			name: "whitespace around equals",
			unit: "[Unit]\nAfter = power-profiles-daemon.service\n",
			key:  "After",
			want: []string{"power-profiles-daemon.service"},
		},
		{
			name: "comment is ignored",
			unit: "[Unit]\n#After=power-profiles-daemon.service\nWants=power-profiles-daemon.service\n",
			key:  "After",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directives := systemdDirectives([]byte(tt.unit))
			if !slices.Equal(directives[tt.key], tt.want) {
				t.Errorf("%s = %v, want %v", tt.key, directives[tt.key], tt.want)
			}
		})
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
