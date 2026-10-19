// Package config provides configuration loading for framework-powerd
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultMetricsStateFile is the default location for the persisted energy
// aggregates. The systemd unit creates /var/lib/framework-powerd via
// StateDirectory=.
const DefaultMetricsStateFile = "/var/lib/framework-powerd/metrics.json"

// Config represents the daemon configuration
type Config struct {
	Server      ServerConfig      `json:"server"`
	Smartd      SmartdConfig      `json:"smartd"`
	GPU         GPUConfig         `json:"gpu"`
	Peripherals PeripheralsConfig `json:"peripherals"`
	Metrics     MetricsConfig     `json:"metrics"`
}

// ServerConfig contains server settings
type ServerConfig struct {
	Address     string `json:"address"`
	Port        int    `json:"port"`
	JWTSecret   string `json:"jwt_secret"`
	IdleTimeout string `json:"idle_timeout"` // Duration string like "5m"
}

// GetIdleTimeout parses the idle timeout duration
func (s *ServerConfig) GetIdleTimeout() time.Duration {
	d, err := time.ParseDuration(s.IdleTimeout)
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

// Validate checks the server settings for obviously invalid values.
func (s *ServerConfig) Validate() error {
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("server.port %d is out of range (1-65535)", s.Port)
	}
	if s.IdleTimeout != "" {
		if d, err := time.ParseDuration(s.IdleTimeout); err != nil {
			return fmt.Errorf("server.idle_timeout %q is not a valid duration: %w", s.IdleTimeout, err)
		} else if d <= 0 {
			return fmt.Errorf("server.idle_timeout must be greater than zero, got %q", s.IdleTimeout)
		}
	}
	return nil
}

// SmartdConfig contains smartd monitoring settings
type SmartdConfig struct {
	Enabled        bool   `json:"enabled"`
	ServiceUnit    string `json:"service_unit"`
	NotifyService  string `json:"notify_service"`
	AlertRetention string `json:"alert_retention"`
}

// GPUConfig contains GPU monitoring settings
type GPUConfig struct {
	Enabled      bool   `json:"enabled"`
	PollInterval string `json:"poll_interval"`
}

// PeripheralsConfig holds optional overrides for the peripheral power estimator.
// If fields are zero, the daemon auto-detects from sysfs and uses defaults.
type PeripheralsConfig struct {
	VRMLossPercent  float64 `json:"vrm_loss_percent"`
	FanIdleRPM      int     `json:"fan_idle_rpm"`
	NVMeIdleWatts   float64 `json:"nvme_idle_watts"`
	NVMeActiveWatts float64 `json:"nvme_active_watts"`
	WiFiIdleWatts   float64 `json:"wifi_idle_watts"`
	WiFiActiveWatts float64 `json:"wifi_active_watts"`
}

// MetricsConfig controls persistence of the aggregated energy metrics.
type MetricsConfig struct {
	Persist      bool   `json:"persist"`
	StateFile    string `json:"state_file"`
	SaveInterval string `json:"save_interval"`
}

// GetSaveInterval parses the persistence save interval.
func (m *MetricsConfig) GetSaveInterval() time.Duration {
	d, err := time.ParseDuration(m.SaveInterval)
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

// Validate checks the metrics persistence settings.
func (m *MetricsConfig) Validate() error {
	if !m.Persist {
		return nil
	}
	if m.StateFile == "" {
		return fmt.Errorf("metrics.state_file must be set when metrics.persist is true")
	}
	if m.SaveInterval != "" {
		if d, err := time.ParseDuration(m.SaveInterval); err != nil {
			return fmt.Errorf("metrics.save_interval %q is not a valid duration: %w", m.SaveInterval, err)
		} else if d <= 0 {
			return fmt.Errorf("metrics.save_interval must be greater than zero, got %q", m.SaveInterval)
		}
	}
	return nil
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address:     "localhost",
			Port:        8080,
			IdleTimeout: "5m",
		},
		Smartd: SmartdConfig{
			Enabled:        false,
			ServiceUnit:    "smartd.service",
			AlertRetention: "30s",
		},
		GPU: GPUConfig{
			Enabled:      false,
			PollInterval: "5s",
		},
		Metrics: MetricsConfig{
			Persist:      true,
			StateFile:    DefaultMetricsStateFile,
			SaveInterval: "5m",
		},
	}
}

// Load reads configuration from the specified path, or searches default locations
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	// If explicit path provided, use it
	if path != "" {
		return loadFromFile(path, cfg)
	}

	// Search default locations
	searchPaths := []string{
		"/etc/framework-powerd/config.json",
	}

	// Add user config path
	if home, err := os.UserHomeDir(); err == nil {
		searchPaths = append(searchPaths, filepath.Join(home, ".config", "framework-powerd", "config.json"))
	}

	for _, p := range searchPaths {
		if _, err := os.Stat(p); err == nil {
			return loadFromFile(p, cfg)
		}
	}

	// No config file found, return defaults
	return cfg, nil
}

func loadFromFile(path string, cfg *Config) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
