package nvme

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

func testConfig() config.NVMeConfig {
	return config.NVMeConfig{
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
	}
}

func TestParseNVMeLog(t *testing.T) {
	data := []byte(`{
		"critical_warning": 0,
		"temperature": 42,
		"avail_spare": 100,
		"spare_thresh": 10,
		"percent_used": 4,
		"data_units_written": 1000,
		"power_on_hours": 123,
		"unsafe_shutdowns": 2,
		"media_errors": 0,
		"num_err_log_entries": 0
	}`)

	got, err := parseNVMeLog(data)
	if err != nil {
		t.Fatalf("parseNVMeLog: %v", err)
	}
	if got.temperatureC != 42 {
		t.Errorf("temperature = %d, want 42", got.temperatureC)
	}
	if got.availableSpare != 100 || got.availableSpareThreshold != 10 {
		t.Errorf("spare = %d/%d, want 100/10", got.availableSpare, got.availableSpareThreshold)
	}
	if got.percentageUsed != 4 {
		t.Errorf("percentage used = %d, want 4", got.percentageUsed)
	}
	if got.dataUnitsWritten != 1000 {
		t.Errorf("data units written = %d, want 1000", got.dataUnitsWritten)
	}
}

func TestParseNVMeLog_StringValues(t *testing.T) {
	data := []byte(`{
		"critical_warning": "0",
		"temperature": "55",
		"available_spare": "98",
		"available_spare_threshold": "10",
		"percentage_used": "12",
		"num_err_log_entries": "3"
	}`)

	got, err := parseNVMeLog(data)
	if err != nil {
		t.Fatalf("parseNVMeLog: %v", err)
	}
	if got.temperatureC != 55 || got.availableSpare != 98 || got.percentageUsed != 12 {
		t.Errorf("unexpected values: %+v", got)
	}
	if got.errorLogEntries != 3 {
		t.Errorf("error log entries = %d, want 3", got.errorLogEntries)
	}
}

func TestParseSmartctlLog(t *testing.T) {
	data := []byte(`{
		"model_name": "WD_BLACK SN850X 2000GB",
		"serial_number": "23202F801634",
		"firmware_version": "620361WD",
		"nvme_smart_health_information_log": {
			"critical_warning": 0,
			"temperature": 49,
			"available_spare": 100,
			"available_spare_threshold": 10,
			"percentage_used": 7,
			"data_units_written": 12345,
			"power_on_hours": 500,
			"unsafe_shutdowns": 4,
			"media_errors": 0,
			"num_err_log_entries": 2
		}
	}`)

	got, err := parseSmartctlLog(data)
	if err != nil {
		t.Fatalf("parseSmartctlLog: %v", err)
	}
	if got.temperatureC != 49 || got.percentageUsed != 7 || got.errorLogEntries != 2 {
		t.Errorf("unexpected values: %+v", got)
	}
}

func TestEvaluate(t *testing.T) {
	cfg := testConfig()
	tests := []struct {
		name         string
		device       DeviceStats
		wantHealth   string
		wantIssuesAt int
	}{
		{"healthy", DeviceStats{PercentageUsed: 4, AvailableSpare: 100, AvailableSpareThreshold: 10, TemperatureC: 42}, HealthOK, 0},
		{"wear warning", DeviceStats{PercentageUsed: 85, AvailableSpare: 100, AvailableSpareThreshold: 10, TemperatureC: 42}, HealthWarning, 1},
		{"wear critical", DeviceStats{PercentageUsed: 95, AvailableSpare: 100, AvailableSpareThreshold: 10, TemperatureC: 42}, HealthCritical, 1},
		{"spare at threshold", DeviceStats{PercentageUsed: 4, AvailableSpare: 10, AvailableSpareThreshold: 10, TemperatureC: 42}, HealthCritical, 1},
		{"spare low", DeviceStats{PercentageUsed: 4, AvailableSpare: 5, AvailableSpareThreshold: 10, TemperatureC: 42}, HealthCritical, 1},
		{"media errors", DeviceStats{PercentageUsed: 4, AvailableSpare: 100, AvailableSpareThreshold: 10, MediaErrors: 1, TemperatureC: 42}, HealthCritical, 1},
		{"error log warning", DeviceStats{PercentageUsed: 4, AvailableSpare: 100, AvailableSpareThreshold: 10, ErrorLogEntries: 2, TemperatureC: 42}, HealthWarning, 1},
		{"error log critical", DeviceStats{PercentageUsed: 4, AvailableSpare: 100, AvailableSpareThreshold: 10, ErrorLogEntries: 10, TemperatureC: 42}, HealthCritical, 1},
		{"temperature warning", DeviceStats{PercentageUsed: 4, AvailableSpare: 100, AvailableSpareThreshold: 10, TemperatureC: 71}, HealthWarning, 1},
		{"temperature critical", DeviceStats{PercentageUsed: 4, AvailableSpare: 100, AvailableSpareThreshold: 10, TemperatureC: 80}, HealthCritical, 1},
		{"critical warning", DeviceStats{CriticalWarning: 4, PercentageUsed: 4, AvailableSpare: 100, AvailableSpareThreshold: 10, TemperatureC: 42}, HealthCritical, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.device
			evaluate(&d, cfg)
			if d.Health != tc.wantHealth {
				t.Errorf("health = %q, want %q (issues: %v)", d.Health, tc.wantHealth, d.Issues)
			}
			if len(d.Issues) != tc.wantIssuesAt {
				t.Errorf("issues = %v, want %d issue(s)", d.Issues, tc.wantIssuesAt)
			}
		})
	}
}

func TestDiscoverDevices(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nvme0")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "model"), []byte("WD_BLACK SN850X 2000GB\n"), 0644)
	os.WriteFile(filepath.Join(dir, "serial"), []byte("23202F801634\n"), 0644)
	os.WriteFile(filepath.Join(dir, "firmware_rev"), []byte("620361WD\n"), 0644)

	devices, err := discoverDevices(base)
	if err != nil {
		t.Fatalf("discoverDevices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(devices))
	}
	if devices[0].Path != "/dev/nvme0" {
		t.Errorf("path = %q, want /dev/nvme0", devices[0].Path)
	}
	if devices[0].Model != "WD_BLACK SN850X 2000GB" {
		t.Errorf("model = %q", devices[0].Model)
	}
	if devices[0].Serial != "23202F801634" {
		t.Errorf("serial = %q", devices[0].Serial)
	}
}

type fakeRunner struct {
	outputs map[string][]byte
	errs    map[string]error
	tools   map[string]bool
}

func (f *fakeRunner) Output(name string, args ...string) ([]byte, error) {
	key := name
	if err, ok := f.errs[key]; ok {
		return nil, err
	}
	if out, ok := f.outputs[key]; ok {
		return out, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeRunner) LookPath(name string) bool { return f.tools[name] }

func TestPollDevices_NVMeCLI(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nvme0")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "model"), []byte("Test NVMe\n"), 0644)

	runner := &fakeRunner{
		tools: map[string]bool{"nvme": true},
		outputs: map[string][]byte{
			"nvme": []byte(`{"critical_warning":0,"temperature":42,"avail_spare":100,"spare_thresh":10,"percent_used":4,"media_errors":0,"num_err_log_entries":0}`),
		},
	}

	m := NewMonitor(testConfig())
	m.runner = runner
	m.now = func() time.Time { return time.Unix(1700000000, 0) }

	devices, err := m.pollDevices(base)
	if err != nil {
		t.Fatalf("pollDevices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(devices))
	}
	if devices[0].Source != "nvme" {
		t.Errorf("source = %q, want nvme", devices[0].Source)
	}
	if devices[0].Health != HealthOK {
		t.Errorf("health = %q, want ok", devices[0].Health)
	}
	if devices[0].DataWrittenBytes != 0 {
		t.Errorf("data written = %d, want 0", devices[0].DataWrittenBytes)
	}
}

func TestPollDevices_SmartctlFallback(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nvme0")
	os.MkdirAll(dir, 0755)

	runner := &fakeRunner{
		tools: map[string]bool{"smartctl": true},
		outputs: map[string][]byte{
			"smartctl": []byte(`{"nvme_smart_health_information_log":{"critical_warning":0,"temperature":80,"available_spare":100,"available_spare_threshold":10,"percentage_used":4,"media_errors":0,"num_err_log_entries":0}}`),
		},
	}

	m := NewMonitor(testConfig())
	m.runner = runner
	devices, err := m.pollDevices(base)
	if err != nil {
		t.Fatalf("pollDevices: %v", err)
	}
	if devices[0].Source != "smartctl" {
		t.Errorf("source = %q, want smartctl", devices[0].Source)
	}
	if devices[0].Health != HealthCritical {
		t.Errorf("health = %q, want critical (temperature 80)", devices[0].Health)
	}
}

func TestHealthSummary(t *testing.T) {
	stats := Stats{Devices: []DeviceStats{
		{Health: HealthOK},
		{Health: HealthWarning},
	}}
	if got := stats.HealthSummary(); got != HealthWarning {
		t.Errorf("summary = %q, want warning", got)
	}
	stats.Devices = append(stats.Devices, DeviceStats{Health: HealthCritical})
	if got := stats.HealthSummary(); got != HealthCritical {
		t.Errorf("summary = %q, want critical", got)
	}
}

func TestParseNVMeLog_NoHealthFields(t *testing.T) {
	if _, err := parseNVMeLog([]byte(`{"error":"permission denied"}`)); err == nil {
		t.Error("expected an error for a payload without health fields")
	}
	if _, err := parseSmartctlLog([]byte(`{"messages":[]}`)); err == nil {
		t.Error("expected an error for a payload without NVMe health fields")
	}
}

func TestHealthSummary_DisabledOnError(t *testing.T) {
	stats := Stats{Enabled: true, LastError: "neither nvme nor smartctl is installed"}
	if got := stats.HealthSummary(); got != "disabled" {
		t.Errorf("summary = %q, want disabled", got)
	}
}
