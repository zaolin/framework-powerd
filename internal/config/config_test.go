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
		{"1h", 1 * time.Hour},
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
		"server": {"address": "0.0.0.0", "port": 9090},
		"ollama": {"enabled": false},
		"smartd": {
			"enabled": true,
			"service_unit": "smartd.service",
			"notify_service": "notify.mobile_phone",
			"alert_retention": "1m"
		},
		"pricing": {"energy_price_per_kwh": 0.40, "currency": "USD"}
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
	if !cfg.Smartd.Enabled {
		t.Error("Expected Smartd.Enabled true")
	}
	if cfg.Smartd.AlertRetention != "1m" {
		t.Errorf("Expected AlertRetention '1m', got %s", cfg.Smartd.AlertRetention)
	}
	if cfg.Pricing.EnergyPricePerKWh != 0.40 {
		t.Errorf("Expected EnergyPricePerKWh 0.40, got %f", cfg.Pricing.EnergyPricePerKWh)
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
	if cfg.Ollama.Enabled != false {
		t.Errorf("Expected default Ollama.Enabled false, got %v", cfg.Ollama.Enabled)
	}
	if cfg.Pricing.EnergyPricePerKWh != 0.30 {
		t.Errorf("Expected default EnergyPricePerKWh 0.30, got %f", cfg.Pricing.EnergyPricePerKWh)
	}
	if cfg.Pricing.Currency != "EUR" {
		t.Errorf("Expected default Currency 'EUR', got %s", cfg.Pricing.Currency)
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

func TestConfig_OllamaGroups(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	configContent := `{
		"server": {"address": "localhost", "port": 8080},
		"ollama": {
			"enabled": true,
			"service_unit": "ollama.service",
			"groups": [
				{"name": "lan", "cidrs": ["192.168.0.0/16", "10.0.0.0/8"]},
				{"name": "tailscale", "cidrs": ["100.64.0.0/10"]}
			]
		}
	}`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg.Ollama.Enabled {
		t.Error("Expected Ollama.Enabled true")
	}
	if len(cfg.Ollama.Groups) != 2 {
		t.Errorf("Expected 2 groups, got %d", len(cfg.Ollama.Groups))
	}
	if cfg.Ollama.Groups[0].Name != "lan" {
		t.Errorf("Expected first group name 'lan', got %s", cfg.Ollama.Groups[0].Name)
	}
	if len(cfg.Ollama.Groups[0].CIDRs) != 2 {
		t.Errorf("Expected 2 CIDRs for lan, got %d", len(cfg.Ollama.Groups[0].CIDRs))
	}
}
