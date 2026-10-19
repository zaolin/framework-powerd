// Package nvme polls NVMe SMART/health data directly from the drives.
package nvme

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

// Monitor polls NVMe devices and caches their health data.
type Monitor struct {
	mu           sync.RWMutex
	cfg          config.NVMeConfig
	pollInterval time.Duration
	stats        Stats
	runner       commandRunner
	now          func() time.Time
}

// commandRunner is the seam over external commands used by tests.
type commandRunner interface {
	Output(name string, args ...string) ([]byte, error)
	LookPath(name string) bool
}

type realRunner struct{}

func (realRunner) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func (realRunner) LookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// NewMonitor creates an NVMe health monitor.
func NewMonitor(cfg config.NVMeConfig) *Monitor {
	return &Monitor{
		cfg:          cfg,
		pollInterval: cfg.GetPollInterval(),
		runner:       realRunner{},
		now:          time.Now,
		stats:        Stats{Enabled: cfg.Enabled},
	}
}

// Start polls immediately and then on the configured interval until ctx ends.
func (m *Monitor) Start(ctx context.Context) {
	if !m.cfg.Enabled {
		return
	}

	if !m.runner.LookPath("nvme") && !m.runner.LookPath("smartctl") {
		msg := "neither nvme nor smartctl is installed; NVMe health polling disabled"
		log.Printf("[NVMe] %s", msg)
		m.setError(msg)
		return
	}

	log.Printf("[NVMe] Polling health every %s", m.pollInterval)
	m.poll()

	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll()
		}
	}
}

func (m *Monitor) poll() {
	devices, err := m.pollDevices("/sys/class/nvme")
	if err != nil {
		log.Printf("[NVMe] Poll failed: %v", err)
		m.setError(err.Error())
		return
	}

	m.mu.Lock()
	m.stats.Devices = devices
	m.stats.LastUpdate = m.now()
	m.stats.LastError = ""
	m.mu.Unlock()

	for _, d := range devices {
		if d.Health == HealthOK {
			continue
		}
		log.Printf("[NVMe] %s health=%s: %s", d.Device, d.Health, strings.Join(d.Issues, "; "))
	}
}

func (m *Monitor) setError(msg string) {
	m.mu.Lock()
	m.stats.LastError = msg
	m.mu.Unlock()
}

// GetStats returns a copy of the cached health data.
func (m *Monitor) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := m.stats
	stats.Devices = make([]DeviceStats, len(m.stats.Devices))
	for i, d := range m.stats.Devices {
		d.Issues = append([]string(nil), d.Issues...)
		stats.Devices[i] = d
	}
	return stats
}

type deviceInfo struct {
	Path     string
	Model    string
	Serial   string
	Firmware string
}

// discoverDevices scans a sysfs controller directory such as /sys/class/nvme.
func discoverDevices(baseDir string) ([]deviceInfo, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}

	var devices []deviceInfo
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "nvme") || !entry.IsDir() {
			continue
		}
		dir := filepath.Join(baseDir, entry.Name())
		devices = append(devices, deviceInfo{
			Path:     filepath.Join("/dev", entry.Name()),
			Model:    readTrimmed(filepath.Join(dir, "model")),
			Serial:   readTrimmed(filepath.Join(dir, "serial")),
			Firmware: readTrimmed(filepath.Join(dir, "firmware_rev")),
		})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Path < devices[j].Path })
	return devices, nil
}

func readTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (m *Monitor) pollDevices(baseDir string) ([]DeviceStats, error) {
	devices, err := discoverDevices(baseDir)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return []DeviceStats{}, nil
	}

	var (
		result []DeviceStats
		errs   []string
	)
	for _, info := range devices {
		values, source, err := m.readDevice(info)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", info.Path, err))
			continue
		}
		stats := DeviceStats{
			Device:                  info.Path,
			Model:                   info.Model,
			Serial:                  info.Serial,
			Firmware:                info.Firmware,
			Source:                  source,
			CriticalWarning:         values.criticalWarning,
			PercentageUsed:          values.percentageUsed,
			AvailableSpare:          values.availableSpare,
			AvailableSpareThreshold: values.availableSpareThreshold,
			MediaErrors:             values.mediaErrors,
			ErrorLogEntries:         values.errorLogEntries,
			TemperatureC:            values.temperatureC,
			DataWrittenBytes:        values.dataUnitsWritten * 512000,
			PowerOnHours:            values.powerOnHours,
			UnsafeShutdowns:         values.unsafeShutdowns,
			LastUpdate:              m.now(),
		}
		evaluate(&stats, m.cfg)
		result = append(result, stats)
	}

	if len(result) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	if len(errs) > 0 {
		log.Printf("[NVMe] %d device(s) could not be read: %s", len(errs), strings.Join(errs, "; "))
	}
	return result, nil
}

type deviceValues struct {
	criticalWarning         int
	percentageUsed          int
	availableSpare          int
	availableSpareThreshold int
	mediaErrors             int64
	errorLogEntries         int64
	temperatureC            int
	dataUnitsWritten        int64
	powerOnHours            int64
	unsafeShutdowns         int64
}

// readDevice prefers nvme-cli and falls back to smartctl.
func (m *Monitor) readDevice(info deviceInfo) (deviceValues, string, error) {
	if m.runner.LookPath("nvme") {
		out, err := m.runner.Output("nvme", "smart-log", "--output-format=json", info.Path)
		if err == nil {
			if values, err := parseNVMeLog(out); err == nil {
				return values, "nvme", nil
			}
		}
	}

	if m.runner.LookPath("smartctl") {
		out, err := m.runner.Output("smartctl", "--json=o", "-a", info.Path)
		if err != nil {
			return deviceValues{}, "", err
		}
		if values, err := parseSmartctlLog(out); err == nil {
			return values, "smartctl", nil
		} else {
			return deviceValues{}, "", err
		}
	}

	return deviceValues{}, "", fmt.Errorf("no usable NVMe tool")
}

// parseNVMeLog parses the `nvme smart-log --output-format=json` payload.
func parseNVMeLog(data []byte) (deviceValues, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return deviceValues{}, err
	}
	if !hasHealthFields(doc) {
		return deviceValues{}, fmt.Errorf("no NVMe health fields in nvme output")
	}
	return valuesFromMap(doc), nil
}

type smartctlDoc struct {
	ModelName                     string         `json:"model_name"`
	SerialNumber                  string         `json:"serial_number"`
	FirmwareVersion               string         `json:"firmware_version"`
	NVMeSmartHealthInformationLog map[string]any `json:"nvme_smart_health_information_log"`
}

// parseSmartctlLog parses a `smartctl --json=o -a` payload.
func parseSmartctlLog(data []byte) (deviceValues, error) {
	var doc smartctlDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return deviceValues{}, err
	}
	health := doc.NVMeSmartHealthInformationLog
	if health == nil {
		// Some smartctl versions put the fields at the top level.
		var top map[string]any
		if err := json.Unmarshal(data, &top); err != nil {
			return deviceValues{}, err
		}
		health = top
	}
	if !hasHealthFields(health) {
		return deviceValues{}, fmt.Errorf("no NVMe health fields in smartctl output")
	}
	return valuesFromMap(health), nil
}

func hasHealthFields(doc map[string]any) bool {
	for _, key := range []string{
		"critical_warning", "temperature", "temperature_celsius",
		"percentage_used", "percent_used",
		"available_spare", "avail_spare",
		"media_errors", "num_err_log_entries", "error_log_entries",
	} {
		if _, ok := doc[key]; ok {
			return true
		}
	}
	return false
}

func valuesFromMap(doc map[string]any) deviceValues {
	return deviceValues{
		criticalWarning:         int(getInt64(doc, "critical_warning")),
		percentageUsed:          int(getInt64(doc, "percentage_used", "percent_used")),
		availableSpare:          int(getInt64(doc, "available_spare", "avail_spare")),
		availableSpareThreshold: int(getInt64(doc, "available_spare_threshold", "spare_thresh")),
		mediaErrors:             getInt64(doc, "media_errors"),
		errorLogEntries:         getInt64(doc, "num_err_log_entries", "error_log_entries"),
		temperatureC:            int(getInt64(doc, "temperature", "temperature_celsius")),
		dataUnitsWritten:        getInt64(doc, "data_units_written"),
		powerOnHours:            getInt64(doc, "power_on_hours"),
		unsafeShutdowns:         getInt64(doc, "unsafe_shutdowns"),
	}
}

func getInt64(doc map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := doc[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case int:
			return int64(v)
		case string:
			s := strings.TrimSpace(v)
			if strings.HasPrefix(strings.ToLower(s), "0x") {
				if n, err := strconv.ParseInt(s[2:], 16, 64); err == nil {
					return n
				}
			}
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return n
			}
			if n, err := strconv.ParseFloat(s, 64); err == nil {
				return int64(n)
			}
		}
	}
	return 0
}

// evaluate applies the configured thresholds and fills Health/Issues.
func evaluate(d *DeviceStats, cfg config.NVMeConfig) {
	d.Issues = nil
	critical := false
	warning := false

	add := func(sev, msg string) {
		d.Issues = append(d.Issues, msg)
		if sev == HealthCritical {
			critical = true
		} else if sev == HealthWarning {
			warning = true
		}
	}

	if d.CriticalWarning != 0 {
		add(HealthCritical, fmt.Sprintf("critical warning bitmask 0x%x", d.CriticalWarning))
	}
	if d.PercentageUsed >= cfg.CritPercentageUsed {
		add(HealthCritical, fmt.Sprintf("percentage used %d%% (critical threshold %d%%)", d.PercentageUsed, cfg.CritPercentageUsed))
	} else if d.PercentageUsed >= cfg.WarnPercentageUsed {
		add(HealthWarning, fmt.Sprintf("percentage used %d%% (warning threshold %d%%)", d.PercentageUsed, cfg.WarnPercentageUsed))
	}
	if d.AvailableSpare > 0 || d.AvailableSpareThreshold > 0 {
		if d.AvailableSpare <= d.AvailableSpareThreshold {
			add(HealthCritical, fmt.Sprintf("available spare %d%% at or below threshold %d%%", d.AvailableSpare, d.AvailableSpareThreshold))
		} else if d.AvailableSpare <= cfg.WarnAvailableSpare {
			add(HealthWarning, fmt.Sprintf("available spare %d%% (warning threshold %d%%)", d.AvailableSpare, cfg.WarnAvailableSpare))
		}
	}
	if d.MediaErrors > cfg.MaxMediaErrors {
		add(HealthCritical, fmt.Sprintf("media/data-integrity errors: %d", d.MediaErrors))
	}
	if cfg.CritErrLogEntries > 0 && d.ErrorLogEntries >= cfg.CritErrLogEntries {
		add(HealthCritical, fmt.Sprintf("error log entries: %d", d.ErrorLogEntries))
	} else if cfg.WarnErrLogEntries > 0 && d.ErrorLogEntries >= cfg.WarnErrLogEntries {
		add(HealthWarning, fmt.Sprintf("error log entries: %d", d.ErrorLogEntries))
	}
	if d.TemperatureC > 0 {
		if d.TemperatureC >= cfg.CritTemperatureC {
			add(HealthCritical, fmt.Sprintf("temperature %d°C (critical threshold %d°C)", d.TemperatureC, cfg.CritTemperatureC))
		} else if d.TemperatureC >= cfg.WarnTemperatureC {
			add(HealthWarning, fmt.Sprintf("temperature %d°C (warning threshold %d°C)", d.TemperatureC, cfg.WarnTemperatureC))
		}
	}

	switch {
	case critical:
		d.Health = HealthCritical
	case warning:
		d.Health = HealthWarning
	default:
		d.Health = HealthOK
	}
}
