// Package config provides configuration loading for framework-powerd
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultMetricsStateFile is the default location for the persisted energy
// aggregates. The systemd unit creates /var/lib/framework-powerd via
// StateDirectory=.
const DefaultMetricsStateFile = "/var/lib/framework-powerd/metrics.json"

// DefaultAlertsStateFile is the default location for persisted SMART/NVMe
// alerts.
const DefaultAlertsStateFile = "/var/lib/framework-powerd/alerts.json"

// DefaultLLMStateFile is the default location for persisted LLM request
// accounting state.
const DefaultLLMStateFile = "/var/lib/framework-powerd/llm.json"

// Config represents the daemon configuration
type Config struct {
	Server      ServerConfig      `json:"server"`
	Smartd      SmartdConfig      `json:"smartd"`
	NVMe        NVMeConfig        `json:"nvme"`
	GPU         GPUConfig         `json:"gpu"`
	NPU         NPUConfig         `json:"npu"`
	Fan         FanConfig         `json:"fan"`
	Peripherals PeripheralsConfig `json:"peripherals"`
	Metrics     MetricsConfig     `json:"metrics"`
	LLM         LLMConfig         `json:"llm"`
	Pricing     PricingConfig     `json:"pricing"`
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
	StateFile      string `json:"state_file"`
}

// GetAlertRetention parses the alert TTL.
func (s *SmartdConfig) GetAlertRetention() time.Duration {
	d, err := time.ParseDuration(s.AlertRetention)
	if err != nil || d <= 0 {
		return 7 * 24 * time.Hour
	}
	return d
}

// Validate checks SMART alert settings.
func (s *SmartdConfig) Validate() error {
	if s.AlertRetention != "" {
		if d, err := time.ParseDuration(s.AlertRetention); err != nil {
			return fmt.Errorf("smartd.alert_retention %q is not a valid duration: %w", s.AlertRetention, err)
		} else if d <= 0 {
			return fmt.Errorf("smartd.alert_retention must be greater than zero, got %q", s.AlertRetention)
		}
	}
	return nil
}

// NVMeConfig contains direct NVMe health polling settings.
type NVMeConfig struct {
	Enabled            bool   `json:"enabled"`
	PollInterval       string `json:"poll_interval"`
	WarnPercentageUsed int    `json:"warn_percentage_used"`
	CritPercentageUsed int    `json:"crit_percentage_used"`
	WarnAvailableSpare int    `json:"warn_available_spare"`
	WarnTemperatureC   int    `json:"warn_temperature_c"`
	CritTemperatureC   int    `json:"crit_temperature_c"`
	WarnErrLogEntries  int64  `json:"warn_err_log_entries"`
	CritErrLogEntries  int64  `json:"crit_err_log_entries"`
	MaxMediaErrors     int64  `json:"max_media_errors"`
}

// GetPollInterval parses the NVMe poll interval.
func (n *NVMeConfig) GetPollInterval() time.Duration {
	d, err := time.ParseDuration(n.PollInterval)
	if err != nil || d <= 0 {
		return 15 * time.Minute
	}
	return d
}

// Validate checks NVMe polling thresholds.
func (n *NVMeConfig) Validate() error {
	if !n.Enabled {
		return nil
	}
	if n.PollInterval != "" {
		if d, err := time.ParseDuration(n.PollInterval); err != nil {
			return fmt.Errorf("nvme.poll_interval %q is not a valid duration: %w", n.PollInterval, err)
		} else if d <= 0 {
			return fmt.Errorf("nvme.poll_interval must be greater than zero, got %q", n.PollInterval)
		}
	}
	if n.WarnPercentageUsed < 0 || n.WarnPercentageUsed > 100 {
		return fmt.Errorf("nvme.warn_percentage_used must be 0-100, got %d", n.WarnPercentageUsed)
	}
	if n.CritPercentageUsed < 0 || n.CritPercentageUsed > 100 {
		return fmt.Errorf("nvme.crit_percentage_used must be 0-100, got %d", n.CritPercentageUsed)
	}
	if n.WarnPercentageUsed >= n.CritPercentageUsed {
		return fmt.Errorf("nvme.warn_percentage_used (%d) must be lower than crit_percentage_used (%d)", n.WarnPercentageUsed, n.CritPercentageUsed)
	}
	if n.WarnAvailableSpare < 0 || n.WarnAvailableSpare > 100 {
		return fmt.Errorf("nvme.warn_available_spare must be 0-100, got %d", n.WarnAvailableSpare)
	}
	if n.WarnTemperatureC <= 0 || n.CritTemperatureC <= 0 {
		return fmt.Errorf("nvme temperature thresholds must be greater than zero")
	}
	if n.WarnTemperatureC >= n.CritTemperatureC {
		return fmt.Errorf("nvme.warn_temperature_c (%d) must be lower than crit_temperature_c (%d)", n.WarnTemperatureC, n.CritTemperatureC)
	}
	if n.WarnErrLogEntries < 0 || n.CritErrLogEntries < 0 || n.MaxMediaErrors < 0 {
		return fmt.Errorf("nvme error thresholds must not be negative")
	}
	if n.WarnErrLogEntries >= n.CritErrLogEntries {
		return fmt.Errorf("nvme.warn_err_log_entries (%d) must be lower than crit_err_log_entries (%d)", n.WarnErrLogEntries, n.CritErrLogEntries)
	}
	return nil
}

// GPUConfig contains GPU monitoring settings
type GPUConfig struct {
	Enabled      bool   `json:"enabled"`
	PollInterval string `json:"poll_interval"`
}

// NPUConfig contains AMD XDNA NPU monitoring settings.
type NPUConfig struct {
	Enabled      bool   `json:"enabled"`
	PollInterval string `json:"poll_interval"`
	// Device is the accel device node. Empty means auto-detect.
	Device string `json:"device"`
}

// GetPollInterval parses the NPU poll interval.
func (n *NPUConfig) GetPollInterval() time.Duration {
	d, err := time.ParseDuration(n.PollInterval)
	if err != nil || d < time.Second {
		return 15 * time.Second
	}
	return d
}

// Validate checks the NPU monitoring settings.
func (n *NPUConfig) Validate() error {
	if !n.Enabled {
		return nil
	}
	if n.PollInterval != "" {
		d, err := time.ParseDuration(n.PollInterval)
		if err != nil {
			return fmt.Errorf("npu.poll_interval %q is not a valid duration: %w", n.PollInterval, err)
		}
		if d < time.Second {
			return fmt.Errorf("npu.poll_interval must be at least 1s, got %q", n.PollInterval)
		}
	}
	if n.Device != "" && !strings.HasPrefix(n.Device, "/dev/accel/") {
		return fmt.Errorf("npu.device %q must point into /dev/accel", n.Device)
	}
	return nil
}

// FanConfig controls the EC fan curve.
type FanConfig struct {
	Enabled bool `json:"enabled"`
	// Mode is "auto" (EC curve) or "curve" (the daemon's own curve).
	Mode string `json:"mode"`

	MinDutyPercent     int `json:"min_duty_percent"`
	StartTempC         int `json:"start_temp_c"`
	FullTempC          int `json:"full_temp_c"`
	CriticalTempC      int `json:"critical_temp_c"`
	BoardCriticalTempC int `json:"board_critical_temp_c"`
	HysteresisC        int `json:"hysteresis_c"`
	FallStepPercent    int `json:"fall_step_percent"`

	PollInterval string `json:"poll_interval"`
}

// GetMode returns the configured fan mode, defaulting to the curve.
func (f *FanConfig) GetMode() string {
	if f.Mode == "" {
		return "curve"
	}
	return f.Mode
}

// GetPollInterval parses the fan poll interval.
func (f *FanConfig) GetPollInterval() time.Duration {
	d, err := time.ParseDuration(f.PollInterval)
	if err != nil || d < time.Second {
		return 5 * time.Second
	}
	return d
}

// GetMinDutyPercent returns the duty floor, defaulting to 50.
func (f *FanConfig) GetMinDutyPercent() int {
	if f.MinDutyPercent <= 0 || f.MinDutyPercent > 100 {
		return 50
	}
	return f.MinDutyPercent
}

// GetStartTempC returns the temperature where the ramp starts.
func (f *FanConfig) GetStartTempC() int {
	if f.StartTempC <= 0 {
		return 75
	}
	return f.StartTempC
}

// GetFullTempC returns the temperature that reaches 100 percent duty.
func (f *FanConfig) GetFullTempC() int {
	if f.FullTempC <= 0 {
		return 90
	}
	return f.FullTempC
}

// GetCriticalTempC returns the temperature that forces 100 percent duty.
func (f *FanConfig) GetCriticalTempC() int {
	if f.CriticalTempC <= 0 {
		return 90
	}
	return f.CriticalTempC
}

// GetBoardCriticalTempC returns the mainboard temperature that forces 100 percent.
func (f *FanConfig) GetBoardCriticalTempC() int {
	if f.BoardCriticalTempC <= 0 {
		return 85
	}
	return f.BoardCriticalTempC
}

// GetHysteresisC returns the temperature hysteresis.
func (f *FanConfig) GetHysteresisC() int {
	if f.HysteresisC <= 0 {
		return 3
	}
	return f.HysteresisC
}

// GetFallStepPercent returns the per-update decrease.
func (f *FanConfig) GetFallStepPercent() int {
	if f.FallStepPercent <= 0 || f.FallStepPercent > 100 {
		return 3
	}
	return f.FallStepPercent
}

// Validate checks the fan settings.
func (f *FanConfig) Validate() error {
	if !f.Enabled {
		return nil
	}
	switch f.Mode {
	case "", "auto", "curve":
	default:
		return fmt.Errorf("fan.mode must be auto or curve, got %q", f.Mode)
	}
	if f.MinDutyPercent < 0 || f.MinDutyPercent > 100 {
		return fmt.Errorf("fan.min_duty_percent must be within 0..100, got %d", f.MinDutyPercent)
	}
	start := f.GetStartTempC()
	full := f.GetFullTempC()
	if full <= start {
		return fmt.Errorf("fan.full_temp_c (%d) must be greater than fan.start_temp_c (%d)", full, start)
	}
	if f.CriticalTempC != 0 && f.CriticalTempC < full {
		return fmt.Errorf("fan.critical_temp_c (%d) must not be below fan.full_temp_c (%d)", f.CriticalTempC, full)
	}
	if f.BoardCriticalTempC < 0 || f.BoardCriticalTempC > 125 {
		return fmt.Errorf("fan.board_critical_temp_c must be within 0..125, got %d", f.BoardCriticalTempC)
	}
	if f.HysteresisC < 0 || f.HysteresisC > 30 {
		return fmt.Errorf("fan.hysteresis_c must be within 0..30, got %d", f.HysteresisC)
	}
	if f.FallStepPercent < 0 || f.FallStepPercent > 100 {
		return fmt.Errorf("fan.fall_step_percent must be within 0..100, got %d", f.FallStepPercent)
	}
	if f.PollInterval != "" {
		d, err := time.ParseDuration(f.PollInterval)
		if err != nil {
			return fmt.Errorf("fan.poll_interval %q is not a valid duration: %w", f.PollInterval, err)
		}
		if d < time.Second {
			return fmt.Errorf("fan.poll_interval must be at least 1s, got %q", f.PollInterval)
		}
	}
	return nil
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

// IPGroup defines a named group of IP ranges
type IPGroup struct {
	Name  string   `json:"name"`
	CIDRs []string `json:"cidrs"`
}

// LLMConfig contains settings for LLM request accounting. The data source is
// a reverse proxy access log (Caddy JSON format) in front of llama-server.
type LLMConfig struct {
	Enabled       bool      `json:"enabled"`
	AccessLog     string    `json:"access_log"`
	Groups        []IPGroup `json:"groups"`
	ExcludeIPs    []string  `json:"exclude_ips"`
	CountedPaths  []string  `json:"counted_paths"`
	HistoryDays   int       `json:"history_days"`
	StateFile     string    `json:"state_file"`
	SaveInterval  string    `json:"save_interval"`
	IgnoreLocalIP bool      `json:"ignore_local_ip"`
}

// GetSaveInterval parses the persistence interval.
func (l *LLMConfig) GetSaveInterval() time.Duration {
	d, err := time.ParseDuration(l.SaveInterval)
	if err != nil || d <= 0 {
		return time.Minute
	}
	return d
}

// GetHistoryDays returns the number of daily buckets to keep.
func (l *LLMConfig) GetHistoryDays() int {
	if l.HistoryDays <= 0 {
		return 30
	}
	return l.HistoryDays
}

// Validate checks the LLM accounting settings.
func (l *LLMConfig) Validate() error {
	if !l.Enabled {
		return nil
	}
	if l.AccessLog == "" {
		return fmt.Errorf("llm.access_log must be set when llm.enabled is true")
	}
	if l.HistoryDays < 0 {
		return fmt.Errorf("llm.history_days must not be negative, got %d", l.HistoryDays)
	}
	if len(l.CountedPaths) == 0 {
		return fmt.Errorf("llm.counted_paths must list at least one path when llm.enabled is true")
	}
	if l.SaveInterval != "" {
		if d, err := time.ParseDuration(l.SaveInterval); err != nil {
			return fmt.Errorf("llm.save_interval %q is not a valid duration: %w", l.SaveInterval, err)
		} else if d <= 0 {
			return fmt.Errorf("llm.save_interval must be greater than zero, got %q", l.SaveInterval)
		}
	}
	names := map[string]bool{}
	for _, group := range l.Groups {
		if group.Name == "" {
			return fmt.Errorf("llm.groups: every group needs a name")
		}
		// Group names become Home Assistant entity names, so compare them
		// case-insensitively to catch "home" and "Home".
		key := strings.ToLower(group.Name)
		if names[key] {
			return fmt.Errorf("llm.groups: duplicate group name %q (names are compared case-insensitively)", group.Name)
		}
		names[key] = true
		for _, cidr := range group.CIDRs {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				if net.ParseIP(cidr) == nil {
					return fmt.Errorf("llm.groups: %q is neither a CIDR nor an IP address", cidr)
				}
			}
		}
	}
	for _, ip := range l.ExcludeIPs {
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("llm.exclude_ips: %q is not a valid IP address", ip)
		}
	}
	return nil
}

// PricingConfig contains energy pricing settings
type PricingConfig struct {
	EnergyPricePerKWh float64 `json:"energy_price_per_kwh"`
	Currency          string  `json:"currency"`
}

// Validate checks the pricing settings used for cost estimates.
func (p *PricingConfig) Validate() error {
	if p.EnergyPricePerKWh < 0 {
		return fmt.Errorf("pricing.energy_price_per_kwh must not be negative, got %v", p.EnergyPricePerKWh)
	}
	currency := strings.TrimSpace(p.Currency)
	if len(currency) != 3 {
		return fmt.Errorf("pricing.currency must be a three-letter ISO 4217 code, got %q", p.Currency)
	}
	for _, r := range strings.ToUpper(currency) {
		if r < 'A' || r > 'Z' {
			return fmt.Errorf("pricing.currency must be a three-letter ISO 4217 code, got %q", p.Currency)
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
			AlertRetention: "168h",
			StateFile:      DefaultAlertsStateFile,
		},
		NVMe: NVMeConfig{
			Enabled:            true,
			PollInterval:       "15m",
			WarnPercentageUsed: 80,
			CritPercentageUsed: 90,
			WarnAvailableSpare: 10,
			WarnTemperatureC:   70,
			CritTemperatureC:   80,
			WarnErrLogEntries:  1,
			CritErrLogEntries:  10,
			MaxMediaErrors:     0,
		},
		GPU: GPUConfig{
			Enabled:      false,
			PollInterval: "5s",
		},
		NPU: NPUConfig{
			Enabled:      false,
			PollInterval: "15s",
		},
		Fan: FanConfig{
			Enabled:            false,
			Mode:               "curve",
			MinDutyPercent:     50,
			StartTempC:         75,
			FullTempC:          90,
			CriticalTempC:      90,
			BoardCriticalTempC: 85,
			HysteresisC:        3,
			FallStepPercent:    3,
			PollInterval:       "5s",
		},
		Metrics: MetricsConfig{
			Persist:      true,
			StateFile:    DefaultMetricsStateFile,
			SaveInterval: "5m",
		},
		LLM: LLMConfig{
			Enabled:       false,
			AccessLog:     "/var/log/caddy/llm-access.log",
			HistoryDays:   30,
			StateFile:     DefaultLLMStateFile,
			SaveInterval:  "1m",
			IgnoreLocalIP: true,
			CountedPaths: []string{
				"/v1/chat/completions",
				"/chat/completions",
				"/v1/completions",
				"/completions",
				"/v1/responses",
				"/v1/messages",
				"/v1/embeddings",
				"/embedding",
			},
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
