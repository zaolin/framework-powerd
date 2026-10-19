package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSmartdConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Smartd.Enabled != false {
		t.Errorf("Expected Smartd.Enabled false, got %v", cfg.Smartd.Enabled)
	}
	if cfg.Smartd.ServiceUnit != "smartd.service" {
		t.Errorf("Expected Smartd.ServiceUnit 'smartd.service', got %s", cfg.Smartd.ServiceUnit)
	}
	if cfg.Smartd.AlertRetention != "30s" {
		t.Errorf("Expected Smartd.AlertRetention '30s', got %s", cfg.Smartd.AlertRetention)
	}
	if cfg.Smartd.NotifyService != "" {
		t.Errorf("Expected Smartd.NotifyService '', got %s", cfg.Smartd.NotifyService)
	}
}

func TestSmartdConfig_Parsing(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	configContent := `{
		"server": {"address": "localhost", "port": 8080},
		"smartd": {
			"enabled": true,
			"service_unit": "smartd.service",
			"notify_service": "notify.mobile_phone",
			"alert_retention": "1m"
		}
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg.Smartd.Enabled {
		t.Error("Expected Smartd.Enabled true")
	}
	if cfg.Smartd.ServiceUnit != "smartd.service" {
		t.Errorf("Expected Smartd.ServiceUnit 'smartd.service', got %s", cfg.Smartd.ServiceUnit)
	}
	if cfg.Smartd.NotifyService != "notify.mobile_phone" {
		t.Errorf("Expected Smartd.NotifyService 'notify.mobile_phone', got %s", cfg.Smartd.NotifyService)
	}
	if cfg.Smartd.AlertRetention != "1m" {
		t.Errorf("Expected Smartd.AlertRetention '1m', got %s", cfg.Smartd.AlertRetention)
	}
}

func TestAlertRetention_ValidDurations(t *testing.T) {
	testCases := []struct {
		input    string
		expected time.Duration
	}{
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"90s", 90 * time.Second},
		{"2h30m", 2*time.Hour + 30*time.Minute},
	}

	for _, tc := range testCases {
		d, err := time.ParseDuration(tc.input)
		if err != nil {
			t.Errorf("Failed to parse duration '%s': %v", tc.input, err)
			continue
		}
		if d != tc.expected {
			t.Errorf("For '%s': expected %v, got %v", tc.input, tc.expected, d)
		}
	}
}

func TestAlertRetention_InvalidDurations(t *testing.T) {
	testCases := []string{"invalid", "abc", "1", "1x"}

	for _, tc := range testCases {
		_, err := time.ParseDuration(tc)
		if err == nil {
			t.Errorf("Expected error for '%s', got nil", tc)
		}
	}
}

func TestConfig_LoadEmptyPathReturnsDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load('') should not fail, got error: %v", err)
	}

	defaults := DefaultConfig()
	if cfg.Server.Address != defaults.Server.Address {
		t.Errorf("Expected default Address, got %s", cfg.Server.Address)
	}
}

func TestConfig_LoadExplicitPathNotExist(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Error("Expected error for nonexistent explicit path")
	}
}

func TestConfig_LoadFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	configContent := `{
		"server": {"address": "0.0.0.0", "port": 9090, "idle_timeout": "10m"},
		"smartd": {
			"enabled": true,
			"service_unit": "smartd.service",
			"notify_service": "notify.mobile_phone",
			"alert_retention": "1m"
		},
		"metrics": {
			"persist": true,
			"state_file": "/tmp/metrics.json",
			"save_interval": "2m"
		}
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Server.Address != "0.0.0.0" {
		t.Errorf("Expected Address '0.0.0.0', got %s", cfg.Server.Address)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("Expected Port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.GetIdleTimeout() != 10*time.Minute {
		t.Errorf("Expected idle timeout 10m, got %v", cfg.Server.GetIdleTimeout())
	}
	if !cfg.Smartd.Enabled {
		t.Error("Expected Smartd.Enabled true")
	}
	if cfg.Smartd.AlertRetention != "1m" {
		t.Errorf("Expected AlertRetention '1m', got %s", cfg.Smartd.AlertRetention)
	}
	if !cfg.Metrics.Persist {
		t.Error("Expected Metrics.Persist true")
	}
	if cfg.Metrics.StateFile != "/tmp/metrics.json" {
		t.Errorf("Expected StateFile '/tmp/metrics.json', got %s", cfg.Metrics.StateFile)
	}
	if cfg.Metrics.GetSaveInterval() != 2*time.Minute {
		t.Errorf("Expected save interval 2m, got %v", cfg.Metrics.GetSaveInterval())
	}
}

func TestConfig_LoadDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.Address != "localhost" {
		t.Errorf("Expected default Address 'localhost', got %s", cfg.Server.Address)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected default Port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.IdleTimeout != "5m" {
		t.Errorf("Expected default IdleTimeout '5m', got %s", cfg.Server.IdleTimeout)
	}
	if !cfg.Metrics.Persist {
		t.Error("Expected metrics persistence to be enabled by default")
	}
	if cfg.Metrics.StateFile != DefaultMetricsStateFile {
		t.Errorf("Expected default state file %q, got %q", DefaultMetricsStateFile, cfg.Metrics.StateFile)
	}
	if cfg.Metrics.GetSaveInterval() != 5*time.Minute {
		t.Errorf("Expected default save interval 5m, got %v", cfg.Metrics.GetSaveInterval())
	}
}

func TestConfig_ServerGetIdleTimeout(t *testing.T) {
	cfg := &ServerConfig{IdleTimeout: "10m"}
	if d := cfg.GetIdleTimeout(); d != 10*time.Minute {
		t.Errorf("Expected 10m, got %v", d)
	}

	cfg = &ServerConfig{IdleTimeout: "invalid"}
	if d := cfg.GetIdleTimeout(); d != 5*time.Minute {
		t.Errorf("Expected default 5m, got %v", d)
	}

	cfg = &ServerConfig{IdleTimeout: ""}
	if d := cfg.GetIdleTimeout(); d != 5*time.Minute {
		t.Errorf("Expected default 5m for empty string, got %v", d)
	}
}

func TestServerConfig_Validate(t *testing.T) {
	valid := &ServerConfig{Port: 8080, IdleTimeout: "5m"}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}

	invalidPort := &ServerConfig{Port: 0}
	if err := invalidPort.Validate(); err == nil {
		t.Error("expected port 0 to be rejected")
	}

	invalidTimeout := &ServerConfig{Port: 8080, IdleTimeout: "soon"}
	if err := invalidTimeout.Validate(); err == nil {
		t.Error("expected invalid idle_timeout to be rejected")
	}

	zeroTimeout := &ServerConfig{Port: 8080, IdleTimeout: "0s"}
	if err := zeroTimeout.Validate(); err == nil {
		t.Error("expected zero idle_timeout to be rejected")
	}
}

func TestMetricsConfig_Validate(t *testing.T) {
	disabled := &MetricsConfig{Persist: false}
	if err := disabled.Validate(); err != nil {
		t.Errorf("disabled persistence should validate: %v", err)
	}

	missingPath := &MetricsConfig{Persist: true, StateFile: ""}
	if err := missingPath.Validate(); err == nil {
		t.Error("expected missing state_file to be rejected")
	}

	badInterval := &MetricsConfig{Persist: true, StateFile: "/tmp/x", SaveInterval: "nope"}
	if err := badInterval.Validate(); err == nil {
		t.Error("expected invalid save_interval to be rejected")
	}

	valid := &MetricsConfig{Persist: true, StateFile: "/tmp/x", SaveInterval: "5m"}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid metrics config rejected: %v", err)
	}
}

func TestMetricsConfig_GetSaveIntervalFallback(t *testing.T) {
	for _, input := range []string{"", "invalid", "0s", "-1m"} {
		cfg := &MetricsConfig{SaveInterval: input}
		if d := cfg.GetSaveInterval(); d != 5*time.Minute {
			t.Errorf("GetSaveInterval(%q) = %v, want 5m", input, d)
		}
	}
}
