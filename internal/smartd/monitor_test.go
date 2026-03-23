package smartd

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

func TestNewMonitor(t *testing.T) {
	cfg := config.SmartdConfig{
		Enabled:        true,
		ServiceUnit:    "smartd.service",
		NotifyService:  "notify.mobile_phone",
		AlertRetention: "30s",
	}

	m := NewMonitor(cfg)

	if m.serviceUnit != cfg.ServiceUnit {
		t.Errorf("Expected serviceUnit %s, got %s", cfg.ServiceUnit, m.serviceUnit)
	}
	if m.notifyService != cfg.NotifyService {
		t.Errorf("Expected notifyService %s, got %s", cfg.NotifyService, m.notifyService)
	}
	if m.alertRetention != 30*time.Second {
		t.Errorf("Expected alertRetention 30s, got %v", m.alertRetention)
	}
}

func TestNewMonitor_DefaultRetention(t *testing.T) {
	cfg := config.SmartdConfig{
		Enabled:     true,
		ServiceUnit: "smartd.service",
	}

	m := NewMonitor(cfg)

	if m.alertRetention != 30*time.Second {
		t.Errorf("Expected default alertRetention 30s, got %v", m.alertRetention)
	}
}

func TestNewMonitor_InvalidRetention(t *testing.T) {
	cfg := config.SmartdConfig{
		Enabled:        true,
		ServiceUnit:    "smartd.service",
		AlertRetention: "invalid",
	}

	m := NewMonitor(cfg)

	if m.alertRetention != 30*time.Second {
		t.Errorf("Expected default alertRetention 30s for invalid input, got %v", m.alertRetention)
	}
}

func TestParseAlert_SelfTestFailure(t *testing.T) {
	m := &Monitor{
		alerts: make(map[string]Alert),
	}

	msg := "Device: /dev/sda, FAILED SMART self-test"

	m.parseAlert(msg)

	if len(m.alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(m.alerts))
	}

	alert, ok := m.alerts["/dev/sda"]
	if !ok {
		t.Fatal("Expected alert for /dev/sda")
	}

	if alert.FailType != "SelfTest" {
		t.Errorf("Expected FailType 'SelfTest', got '%s'", alert.FailType)
	}
	if alert.Message != "FAILED SMART self-test" {
		t.Errorf("Expected Message 'FAILED SMART self-test', got '%s'", alert.Message)
	}
	if alert.Device != "/dev/sda" {
		t.Errorf("Expected Device '/dev/sda', got '%s'", alert.Device)
	}
}

func TestParseAlert_HealthFailure(t *testing.T) {
	m := &Monitor{
		alerts: make(map[string]Alert),
	}

	msg := "Device: /dev/nvme0n1, Failed SMART health check"

	m.parseAlert(msg)

	if len(m.alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(m.alerts))
	}

	alert, ok := m.alerts["/dev/nvme0n1"]
	if !ok {
		t.Fatal("Expected alert for /dev/nvme0n1")
	}

	if alert.FailType != "Health" {
		t.Errorf("Expected FailType 'Health', got '%s'", alert.FailType)
	}
	if alert.Message != "Failed SMART health check" {
		t.Errorf("Expected Message 'Failed SMART health check', got '%s'", alert.Message)
	}
}

func TestParseAlert_AttributeFailure(t *testing.T) {
	m := &Monitor{
		alerts: make(map[string]Alert),
	}

	msg := "Device: /dev/sda, Failed SMART Attribute: 5 Reallocated_Sector_Ct"

	m.parseAlert(msg)

	if len(m.alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(m.alerts))
	}

	alert, ok := m.alerts["/dev/sda"]
	if !ok {
		t.Fatal("Expected alert for /dev/sda")
	}

	if alert.FailType != "Attribute" {
		t.Errorf("Expected FailType 'Attribute', got '%s'", alert.FailType)
	}
	if alert.Message != "Failed SMART Attribute: 5 Reallocated_Sector_Ct" {
		t.Errorf("Expected Message with attribute, got '%s'", alert.Message)
	}
}

func TestParseAlert_NoMatch(t *testing.T) {
	m := &Monitor{
		alerts: make(map[string]Alert),
	}

	testCases := []string{
		"Device: /dev/sda, SMART Attribute: 194 Temperature_Celsius changed from 94 to 93",
		"Starting smartd version 7.3",
		"Monitoring 1 device",
		"some random log message",
	}

	for _, msg := range testCases {
		m.parseAlert(msg)
	}

	if len(m.alerts) != 0 {
		t.Errorf("Expected 0 alerts for non-matching messages, got %d", len(m.alerts))
	}
}

func TestParseAlert_MultipleDevices(t *testing.T) {
	m := &Monitor{
		alerts: make(map[string]Alert),
	}

	m.parseAlert("Device: /dev/sda, FAILED SMART self-test")
	m.parseAlert("Device: /dev/sdb, Failed SMART health check")

	if len(m.alerts) != 2 {
		t.Fatalf("Expected 2 alerts, got %d", len(m.alerts))
	}

	if _, ok := m.alerts["/dev/sda"]; !ok {
		t.Error("Expected alert for /dev/sda")
	}
	if _, ok := m.alerts["/dev/sdb"]; !ok {
		t.Error("Expected alert for /dev/sdb")
	}
}

func TestParseAlert_SameDeviceOverwrites(t *testing.T) {
	m := &Monitor{
		alerts: make(map[string]Alert),
	}

	m.parseAlert("Device: /dev/sda, FAILED SMART self-test")
	firstAlert := m.alerts["/dev/sda"]

	time.Sleep(10 * time.Millisecond)

	m.parseAlert("Device: /dev/sda, Failed SMART health check")
	secondAlert := m.alerts["/dev/sda"]

	if len(m.alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(m.alerts))
	}

	if secondAlert.FailType == firstAlert.FailType {
		t.Error("Expected FailType to be overwritten")
	}
	if secondAlert.Timestamp == firstAlert.Timestamp {
		t.Error("Expected Timestamp to be updated")
	}
}

func TestCleanupResolvedAlerts_NoExpired(t *testing.T) {
	m := &Monitor{
		alertRetention: 30 * time.Second,
		alerts: map[string]Alert{
			"/dev/sda": {
				Device:    "/dev/sda",
				FailType:  "SelfTest",
				Timestamp: time.Now(),
			},
		},
	}

	m.cleanupResolvedAlerts()

	if len(m.alerts) != 1 {
		t.Errorf("Expected 1 alert (not expired), got %d", len(m.alerts))
	}
}

func TestCleanupResolvedAlerts_Expired(t *testing.T) {
	m := &Monitor{
		alertRetention: 10 * time.Millisecond,
		alerts: map[string]Alert{
			"/dev/sda": {
				Device:    "/dev/sda",
				FailType:  "SelfTest",
				Timestamp: time.Now().Add(-100 * time.Millisecond),
			},
		},
	}

	time.Sleep(15 * time.Millisecond)

	m.cleanupResolvedAlerts()

	if len(m.alerts) != 0 {
		t.Errorf("Expected 0 alerts (all expired), got %d", len(m.alerts))
	}
}

func TestCleanupResolvedAlerts_PartiallyExpired(t *testing.T) {
	m := &Monitor{
		alertRetention: 100 * time.Millisecond,
		alerts: map[string]Alert{
			"/dev/sda": {
				Device:    "/dev/sda",
				FailType:  "SelfTest",
				Timestamp: time.Now().Add(-200 * time.Millisecond),
			},
			"/dev/sdb": {
				Device:    "/dev/sdb",
				FailType:  "Health",
				Timestamp: time.Now(),
			},
		},
	}

	time.Sleep(15 * time.Millisecond)

	m.cleanupResolvedAlerts()

	if len(m.alerts) != 1 {
		t.Errorf("Expected 1 alert (one expired), got %d", len(m.alerts))
	}
	if _, ok := m.alerts["/dev/sda"]; ok {
		t.Error("Expected /dev/sda to be removed")
	}
	if _, ok := m.alerts["/dev/sdb"]; !ok {
		t.Error("Expected /dev/sdb to remain")
	}
}

func TestGetStats(t *testing.T) {
	m := &Monitor{
		notifyService: "notify.mobile_phone",
		alerts: map[string]Alert{
			"/dev/sda": {
				Device:    "/dev/sda",
				FailType:  "SelfTest",
				Message:   "FAILED SMART self-test",
				Timestamp: time.Now(),
			},
		},
	}

	stats := m.GetStats()

	if len(stats.Alerts) != 1 {
		t.Errorf("Expected 1 alert, got %d", len(stats.Alerts))
	}
	if stats.NotifyService != "notify.mobile_phone" {
		t.Errorf("Expected NotifyService 'notify.mobile_phone', got '%s'", stats.NotifyService)
	}
	if stats.LastCheck == "" {
		t.Error("Expected LastCheck to be set")
	}
}

func TestGetStats_NoAlerts(t *testing.T) {
	m := &Monitor{
		notifyService: "notify.mobile_phone",
		alerts:        make(map[string]Alert),
	}

	stats := m.GetStats()

	if len(stats.Alerts) != 0 {
		t.Errorf("Expected 0 alerts, got %d", len(stats.Alerts))
	}
	if stats.NotifyService != "notify.mobile_phone" {
		t.Errorf("Expected NotifyService 'notify.mobile_phone', got '%s'", stats.NotifyService)
	}
}

func TestGetStats_EmptyNotifyService(t *testing.T) {
	m := &Monitor{
		notifyService: "",
		alerts:        make(map[string]Alert),
	}

	stats := m.GetStats()

	if stats.NotifyService != "" {
		t.Errorf("Expected NotifyService '', got '%s'", stats.NotifyService)
	}
}

func TestGetNotifyService(t *testing.T) {
	m := &Monitor{
		notifyService: "notify.mobile_phone",
	}

	if m.GetNotifyService() != "notify.mobile_phone" {
		t.Errorf("Expected 'notify.mobile_phone', got '%s'", m.GetNotifyService())
	}
}

func TestConcurrent_ParseAndGetStats(t *testing.T) {
	m := &Monitor{
		notifyService: "notify.mobile_phone",
		alerts:        make(map[string]Alert),
	}

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
	cfg := config.SmartdConfig{
		Enabled:        true,
		ServiceUnit:    "smartd.service",
		NotifyService:  "notify.mobile_phone",
		AlertRetention: "30s",
	}

	m := NewMonitor(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Start(ctx)

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}
