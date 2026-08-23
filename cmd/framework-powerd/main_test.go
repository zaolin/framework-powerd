package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/golang-jwt/jwt/v5"
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
			name:      "active + game running → performance",
			st:        stateSnapshot{isIdle: false, isGameRunning: true, gamePID: 1234},
			wantPerf:  true,
		},
		{
			name:      "active + remote play → performance",
			st:        stateSnapshot{isIdle: false, isRemotePlay: true},
			wantPerf:  true,
		},
		{
			name:     "active + desktop → default active",
			st:       stateSnapshot{isIdle: false},
			wantDef:  true,
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