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
	if cfg.Smartd.AlertRetention != "168h" {
		t.Errorf("Expected Smartd.AlertRetention '168h', got %s", cfg.Smartd.AlertRetention)
	}
	if cfg.Smartd.StateFile != DefaultAlertsStateFile {
		t.Errorf("Expected Smartd.StateFile %q, got %q", DefaultAlertsStateFile, cfg.Smartd.StateFile)
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

func TestNVMeConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig().NVMe
	if !cfg.Enabled {
		t.Error("Expected NVMe polling to be enabled by default")
	}
	if cfg.GetPollInterval() != 15*time.Minute {
		t.Errorf("poll interval = %v, want 15m", cfg.GetPollInterval())
	}
	if cfg.WarnPercentageUsed != 80 || cfg.CritPercentageUsed != 90 {
		t.Errorf("unexpected wear thresholds: %+v", cfg)
	}
	if cfg.WarnTemperatureC != 70 || cfg.CritTemperatureC != 80 {
		t.Errorf("unexpected temperature thresholds: %+v", cfg)
	}
}

func TestNVMeConfig_Validate(t *testing.T) {
	valid := DefaultConfig().NVMe
	if err := valid.Validate(); err != nil {
		t.Errorf("valid NVMe config rejected: %v", err)
	}

	disabled := NVMeConfig{Enabled: false}
	if err := disabled.Validate(); err != nil {
		t.Errorf("disabled NVMe config rejected: %v", err)
	}

	badInterval := valid
	badInterval.PollInterval = "soon"
	if err := badInterval.Validate(); err == nil {
		t.Error("expected invalid poll interval to be rejected")
	}

	badWear := valid
	badWear.WarnPercentageUsed = 95
	if err := badWear.Validate(); err == nil {
		t.Error("expected warn >= crit wear thresholds to be rejected")
	}

	badTemp := valid
	badTemp.WarnTemperatureC = 90
	if err := badTemp.Validate(); err == nil {
		t.Error("expected warn >= crit temperature thresholds to be rejected")
	}

	badErr := valid
	badErr.WarnErrLogEntries = 20
	if err := badErr.Validate(); err == nil {
		t.Error("expected warn >= crit error thresholds to be rejected")
	}
}

func TestSmartdConfig_Validate(t *testing.T) {
	valid := DefaultConfig().Smartd
	if err := valid.Validate(); err != nil {
		t.Errorf("valid smartd config rejected: %v", err)
	}

	bad := valid
	bad.AlertRetention = "soon"
	if err := bad.Validate(); err == nil {
		t.Error("expected invalid alert retention to be rejected")
	}

	if got := valid.GetAlertRetention(); got != 7*24*time.Hour {
		t.Errorf("GetAlertRetention = %v, want 168h", got)
	}
}

func TestLLMConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig().LLM
	if cfg.Enabled {
		t.Error("Expected LLM accounting to be disabled by default")
	}
	if cfg.AccessLog != "/var/log/caddy/llm-access.log" {
		t.Errorf("Expected default access log path, got %q", cfg.AccessLog)
	}
	if cfg.GetHistoryDays() != 30 {
		t.Errorf("Expected 30 history days, got %d", cfg.GetHistoryDays())
	}
	if cfg.GetSaveInterval() != time.Minute {
		t.Errorf("Expected 1m save interval, got %v", cfg.GetSaveInterval())
	}
	if !cfg.IgnoreLocalIP {
		t.Error("Expected local IP traffic to be ignored by default")
	}
	if len(cfg.CountedPaths) == 0 {
		t.Error("Expected default counted paths")
	}
}

func TestLLMConfig_Validate(t *testing.T) {
	valid := DefaultConfig().LLM
	valid.Enabled = true
	if err := valid.Validate(); err != nil {
		t.Errorf("valid LLM config rejected: %v", err)
	}

	disabled := LLMConfig{}
	if err := disabled.Validate(); err != nil {
		t.Errorf("disabled LLM config rejected: %v", err)
	}

	missingLog := valid
	missingLog.AccessLog = ""
	if err := missingLog.Validate(); err == nil {
		t.Error("expected missing access_log to be rejected")
	}

	badInterval := valid
	badInterval.SaveInterval = "soon"
	if err := badInterval.Validate(); err == nil {
		t.Error("expected invalid save_interval to be rejected")
	}

	duplicateGroup := valid
	duplicateGroup.Groups = []IPGroup{
		{Name: "home", CIDRs: []string{"192.168.0.0/16"}},
		{Name: "home", CIDRs: []string{"10.0.0.0/8"}},
	}
	if err := duplicateGroup.Validate(); err == nil {
		t.Error("expected duplicate group names to be rejected")
	}

	badCIDR := valid
	badCIDR.Groups = []IPGroup{{Name: "home", CIDRs: []string{"not-a-cidr"}}}
	if err := badCIDR.Validate(); err == nil {
		t.Error("expected an invalid CIDR to be rejected")
	}

	badExclude := valid
	badExclude.ExcludeIPs = []string{"nope"}
	if err := badExclude.Validate(); err == nil {
		t.Error("expected an invalid exclude IP to be rejected")
	}

	negativeHistory := valid
	negativeHistory.HistoryDays = -1
	if err := negativeHistory.Validate(); err == nil {
		t.Error("expected a negative history_days to be rejected")
	}

	emptyPaths := valid
	emptyPaths.CountedPaths = nil
	if err := emptyPaths.Validate(); err == nil {
		t.Error("expected empty counted_paths to be rejected when enabled")
	}

	caseDuplicate := valid
	caseDuplicate.Groups = []IPGroup{
		{Name: "home", CIDRs: []string{"192.168.0.0/16"}},
		{Name: "Home", CIDRs: []string{"10.0.0.0/8"}},
	}
	if err := caseDuplicate.Validate(); err == nil {
		t.Error("expected case-insensitive duplicate group names to be rejected")
	}
}

func TestPricingConfig_Validate(t *testing.T) {
	valid := PricingConfig{EnergyPricePerKWh: 0.30, Currency: "EUR"}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid pricing rejected: %v", err)
	}

	negative := PricingConfig{EnergyPricePerKWh: -1, Currency: "EUR"}
	if err := negative.Validate(); err == nil {
		t.Error("expected a negative price to be rejected")
	}

	noCurrency := PricingConfig{EnergyPricePerKWh: 0.30}
	if err := noCurrency.Validate(); err == nil {
		t.Error("expected an empty currency to be rejected")
	}

	for _, bad := range []string{"EURO", "€", "E1R"} {
		invalid := PricingConfig{EnergyPricePerKWh: 0.30, Currency: bad}
		if err := invalid.Validate(); err == nil {
			t.Errorf("expected currency %q to be rejected", bad)
		}
	}

	lowercase := PricingConfig{EnergyPricePerKWh: 0.30, Currency: "eur"}
	if err := lowercase.Validate(); err != nil {
		t.Errorf("lowercase ISO code should be accepted: %v", err)
	}
}

func TestPricingConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig().Pricing
	if cfg.EnergyPricePerKWh != 0.30 {
		t.Errorf("Expected default price 0.30, got %v", cfg.EnergyPricePerKWh)
	}
	if cfg.Currency != "EUR" {
		t.Errorf("Expected default currency EUR, got %q", cfg.Currency)
	}
}
