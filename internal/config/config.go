// Package config provides configuration loading for framework-powerd
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Config represents the daemon configuration
type Config struct {
	Server  ServerConfig  `json:"server"`
	Ollama  OllamaConfig  `json:"ollama"`
	Pricing PricingConfig `json:"pricing"`
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

// OllamaConfig contains Ollama monitoring settings
type OllamaConfig struct {
	Enabled     bool      `json:"enabled"`
	ServiceUnit string    `json:"service_unit"`
	Groups      []IPGroup `json:"groups"`
}

// IPGroup defines a named group of IP ranges
type IPGroup struct {
	Name  string   `json:"name"`
	CIDRs []string `json:"cidrs"`
}

// PricingConfig contains energy pricing settings
type PricingConfig struct {
	EnergyPricePerKWh float64 `json:"energy_price_per_kwh"`
	Currency          string  `json:"currency"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address:     "localhost",
			Port:        8080,
			IdleTimeout: "5m",
		},
		Ollama: OllamaConfig{
			Enabled:     false,
			ServiceUnit: "ollama.service",
		},
		Pricing: PricingConfig{
			EnergyPricePerKWh: 0.30,
			Currency:          "EUR",
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
