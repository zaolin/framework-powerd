package smartd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

func newTestMonitor(now time.Time) *Monitor {
	return &Monitor{
		alertRetention: 7 * 24 * time.Hour,
		alerts:         make(map[string]Alert),
		now:            func() time.Time { return now },
	}
}

func TestNewMonitor(t *testing.T) {
	cfg := config.SmartdConfig{
		Enabled:        true,
		ServiceUnit:    "smartd.service",
		NotifyService:  "notify.mobile_phone",
		AlertRetention: "2h",
		StateFile:      "/tmp/alerts.json",
	}

	m := NewMonitor(cfg)

	if m.serviceUnit != cfg.ServiceUnit {
		t.Errorf("serviceUnit = %s, want %s", m.serviceUnit, cfg.ServiceUnit)
	}
	if m.notifyService != cfg.NotifyService {
		t.Errorf("notifyService = %s, want %s", m.notifyService, cfg.NotifyService)
	}
	if m.alertRetention != 2*time.Hour {
		t.Errorf("alertRetention = %v, want 2h", m.alertRetention)
	}
	if m.statePath != cfg.StateFile {
		t.Errorf("statePath = %s, want %s", m.statePath, cfg.StateFile)
	}
}

func TestNewMonitor_DefaultRetention(t *testing.T) {
	m := NewMonitor(config.SmartdConfig{ServiceUnit: "smartd.service"})
	if m.alertRetention != 7*24*time.Hour {
		t.Errorf("alertRetention = %v, want 168h", m.alertRetention)
	}
}

func TestNewMonitor_InvalidRetention(t *testing.T) {
	m := NewMonitor(config.SmartdConfig{ServiceUnit: "smartd.service", AlertRetention: "invalid"})
	if m.alertRetention != 7*24*time.Hour {
		t.Errorf("alertRetention = %v, want 168h fallback", m.alertRetention)
	}
}

func TestParseAlert_SelfTestFailure(t *testing.T) {
	m := newTestMonitor(time.Now())
	m.parseAlert("Device: /dev/sda, FAILED SMART self-test")

	alert, ok := m.alerts["/dev/sda|selftest"]
	if !ok {
		t.Fatalf("expected alert /dev/sda|selftest, got %v", m.alerts)
	}
	if alert.Severity != "critical" {
		t.Errorf("severity = %q, want critical", alert.Severity)
	}
	if alert.Count != 1 {
		t.Errorf("count = %d, want 1", alert.Count)
	}
	if alert.FirstSeen.IsZero() || alert.LastSeen.IsZero() {
		t.Error("timestamps must be set")
	}
}

func TestParseAlert_HealthFailure(t *testing.T) {
	m := newTestMonitor(time.Now())
	m.parseAlert("Device: /dev/nvme0n1, Failed SMART health check")

	alert, ok := m.alerts["/dev/nvme0n1|health"]
	if !ok {
		t.Fatalf("expected health alert, got %v", m.alerts)
	}
	if alert.FailType != "health" || alert.Severity != "critical" {
		t.Errorf("unexpected alert: %+v", alert)
	}
}

func TestParseAlert_AttributeFailure(t *testing.T) {
	m := newTestMonitor(time.Now())
	m.parseAlert("Device: /dev/sda, Failed SMART Attribute: 5 Reallocated_Sector_Ct")

	alert, ok := m.alerts["/dev/sda|attribute"]
	if !ok {
		t.Fatalf("expected attribute alert, got %v", m.alerts)
	}
	if alert.Severity != "critical" {
		t.Errorf("severity = %q, want critical", alert.Severity)
	}
}

func TestParseAlert_NoMatch(t *testing.T) {
	m := newTestMonitor(time.Now())
	for _, msg := range []string{
		"Starting smartd version 7.5",
		"Monitoring 1 device",
		"Device: /dev/nvme0, opened",
		"Device: /dev/nvme0, is SMART capable. Adding to \"monitor\" list.",
		"Device: /dev/nvme0, WD_BLACK SN850X 2000GB, S/N:23202F801634, FW:620361WD, 2.00 TB",
		"Device: /dev/nvme0, SMART Attribute: 194 Temperature_Celsius changed from 94 to 93",
		"Device: /dev/nvme0, No Errors Logged",
		"Device: /dev/nvme0, Critical Warning: 0x00",
		"Device: /dev/nvme0, Media and Data Integrity Errors: 0",
		"Device: /dev/nvme0, Error Information Log Entries: 0",
		"Device: /dev/nvme0, Available Spare: 100%",
		"Device: /dev/nvme0, Percentage Used: 42%",
	} {
		m.parseAlert(msg)
	}
	if len(m.alerts) != 0 {
		t.Errorf("expected 0 alerts, got %v", m.alerts)
	}
}

func TestParseAlert_AdditionalPatterns(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		failType string
		severity string
	}{
		{"critical warning", "Device: /dev/nvme0, Critical Warning (0x04): Reliability", "critical_warning", "critical"},
		{"media errors warning", "Device: /dev/nvme0, Media and Data Integrity Errors: 1", "media_errors", "warning"},
		{"media errors critical", "Device: /dev/nvme0, Media and Data Integrity Errors: 12", "media_errors", "critical"},
		{"spare critical", "Device: /dev/nvme0, Available Spare: 5%", "available_spare", "critical"},
		{"spare warning", "Device: /dev/nvme0, Available Spare: 15%", "available_spare", "warning"},
		{"wear warning", "Device: /dev/nvme0, Percentage Used: 85%", "percentage_used", "warning"},
		{"wear critical", "Device: /dev/nvme0, Percentage Used: 95%", "percentage_used", "critical"},
		{"error log warning", "Device: /dev/nvme0, Error Information Log Entries: 2", "error_log", "warning"},
		{"error log critical", "Device: /dev/nvme0, Error Information Log Entries: 12", "error_log", "critical"},
		{"pending sectors", "Device: /dev/sda, 1 Currently unreadable (pending) sectors", "sector_errors", "critical"},
		{"offline uncorrectable", "Device: /dev/sda, 1 Offline uncorrectable sectors", "sector_errors", "critical"},
		{"reallocated", "Device: /dev/sda, Reallocated_Sector_Ct 5", "reallocated_sectors", "warning"},
		{"ata error count", "Device: /dev/sda, ATA Error Count increased from 1 to 2", "io_errors", "warning"},
		{"generic failure", "Device: /dev/sda, something failed unexpectedly", "event", "warning"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestMonitor(time.Now())
			m.parseAlert(tc.message)
			key := deviceFromMessage(tc.message) + "|" + tc.failType
			alert, ok := m.alerts[key]
			if !ok {
				t.Fatalf("expected key %q, got %v", key, m.alerts)
			}
			if alert.Severity != tc.severity {
				t.Errorf("severity = %q, want %q", alert.Severity, tc.severity)
			}
		})
	}
}

func deviceFromMessage(msg string) string {
	matches := deviceMessageRe.FindStringSubmatch(msg)
	if matches == nil {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func TestParseAlert_MultipleDevicesAndTypes(t *testing.T) {
	m := newTestMonitor(time.Now())
	m.parseAlert("Device: /dev/sda, FAILED SMART self-test")
	m.parseAlert("Device: /dev/sdb, Failed SMART health check")
	m.parseAlert("Device: /dev/sda, Failed SMART health check")

	if len(m.alerts) != 3 {
		t.Fatalf("expected 3 alerts, got %d: %v", len(m.alerts), m.alerts)
	}
}

func TestParseAlert_RepeatIncrements(t *testing.T) {
	now := time.Unix(1700000000, 0)
	m := newTestMonitor(now)
	m.parseAlert("Device: /dev/sda, FAILED SMART self-test")
	first := m.alerts["/dev/sda|selftest"]

	now = now.Add(time.Minute)
	m.now = func() time.Time { return now }
	m.parseAlert("Device: /dev/sda, FAILED SMART self-test")
	second := m.alerts["/dev/sda|selftest"]

	if second.Count != 2 {
		t.Errorf("count = %d, want 2", second.Count)
	}
	if !second.FirstSeen.Equal(first.FirstSeen) {
		t.Error("FirstSeen must not change")
	}
	if !second.LastSeen.After(first.LastSeen) {
		t.Error("LastSeen must advance")
	}
}

func TestCleanupExpiredAlerts(t *testing.T) {
	now := time.Unix(1700000000, 0)
	m := newTestMonitor(now)
	m.alerts["/dev/sda|selftest"] = Alert{
		Device:   "/dev/sda",
		FailType: "selftest",
		LastSeen: now.Add(-8 * 24 * time.Hour),
	}
	m.alerts["/dev/sdb|health"] = Alert{
		Device:   "/dev/sdb",
		FailType: "health",
		LastSeen: now.Add(-time.Hour),
	}

	m.cleanupExpiredAlerts()

	if _, ok := m.alerts["/dev/sda|selftest"]; ok {
		t.Error("expired alert should be removed")
	}
	if _, ok := m.alerts["/dev/sdb|health"]; !ok {
		t.Error("fresh alert should remain")
	}
}

func TestSaveLoadState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.json")
	now := time.Unix(1700000000, 0)

	m := newTestMonitor(now)
	m.statePath = path
	m.parseAlert("Device: /dev/nvme0, Percentage Used: 95%")
	if err := m.SaveState(); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	restored := newTestMonitor(now)
	restored.statePath = path
	ok, err := restored.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !ok {
		t.Fatal("expected alerts to be restored")
	}
	alert, ok := restored.alerts["/dev/nvme0|percentage_used"]
	if !ok {
		t.Fatalf("expected restored alert, got %v", restored.alerts)
	}
	if alert.Severity != "critical" {
		t.Errorf("severity = %q, want critical", alert.Severity)
	}
}

func TestLoadState_ExpiredIsSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.json")
	now := time.Unix(1700000000, 0)

	m := newTestMonitor(now)
	m.statePath = path
	m.alerts["/dev/sda|selftest"] = Alert{
		ID:       "/dev/sda|selftest",
		Device:   "/dev/sda",
		FailType: "selftest",
		Severity: "critical",
		LastSeen: now.Add(-8 * 24 * time.Hour),
	}
	if err := m.SaveState(); err != nil {
		t.Fatal(err)
	}

	restored := newTestMonitor(now)
	restored.statePath = path
	ok, err := restored.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expired alert must not be restored")
	}
}

func TestLoadState_MissingFile(t *testing.T) {
	m := newTestMonitor(time.Now())
	m.statePath = filepath.Join(t.TempDir(), "missing.json")
	ok, err := m.LoadState()
	if err != nil {
		t.Errorf("missing file should not be an error: %v", err)
	}
	if ok {
		t.Error("missing file must not report a restore")
	}
}

func TestGetStats_Sorted(t *testing.T) {
	now := time.Unix(1700000000, 0)
	m := newTestMonitor(now)
	m.lastCheck = now
	m.alerts = map[string]Alert{
		"/dev/sda|event":  {Device: "/dev/sda", FailType: "event", Severity: "warning", LastSeen: now},
		"/dev/sdb|health": {Device: "/dev/sdb", FailType: "health", Severity: "critical", LastSeen: now},
	}

	stats := m.GetStats()
	if len(stats.Alerts) != 2 {
		t.Fatalf("alerts = %d, want 2", len(stats.Alerts))
	}
	if stats.Alerts[0].Severity != "critical" {
		t.Errorf("first alert severity = %q, want critical", stats.Alerts[0].Severity)
	}
	if stats.LastCheck == "" {
		t.Error("LastCheck should be set")
	}
}

func TestGetStats_NoAlertsAndNotifyService(t *testing.T) {
	m := newTestMonitor(time.Now())
	m.notifyService = "notify.mobile_phone"
	stats := m.GetStats()
	if len(stats.Alerts) != 0 {
		t.Errorf("alerts = %d, want 0", len(stats.Alerts))
	}
	if stats.NotifyService != "notify.mobile_phone" {
		t.Errorf("NotifyService = %q", stats.NotifyService)
	}
	if m.GetNotifyService() != "notify.mobile_phone" {
		t.Errorf("GetNotifyService = %q", m.GetNotifyService())
	}
}

func TestConcurrent_ParseAndGetStats(t *testing.T) {
	m := newTestMonitor(time.Now())
	var wg sync.WaitGroup
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					m.parseAlert("Device: /dev/sda, FAILED SMART self-test")
				}
			}
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = m.GetStats()
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(done)
	wg.Wait()
}

func TestStart_ContextCancellation(t *testing.T) {
	m := NewMonitor(config.SmartdConfig{ServiceUnit: "smartd.service"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := m.Start(ctx); err != context.Canceled {
		t.Errorf("Start = %v, want context.Canceled", err)
	}
}

func TestLoadState_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.json")
	if err := os.WriteFile(path, []byte("{broken"), 0644); err != nil {
		t.Fatal(err)
	}
	m := newTestMonitor(time.Now())
	m.statePath = path
	if _, err := m.LoadState(); err == nil {
		t.Error("expected an error for a corrupt state file")
	}
}
