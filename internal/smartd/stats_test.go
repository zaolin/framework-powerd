package smartd

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAlert_JSONSerialization(t *testing.T) {
	alert := Alert{
		Device:    "/dev/sda",
		FailType:  "SelfTest",
		Message:   "FAILED SMART self-test",
		Timestamp: time.Date(2026, 3, 23, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(alert)
	if err != nil {
		t.Fatalf("Failed to marshal Alert: %v", err)
	}

	var decoded Alert
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal Alert: %v", err)
	}

	if decoded.Device != alert.Device {
		t.Errorf("Expected Device %s, got %s", alert.Device, decoded.Device)
	}
	if decoded.FailType != alert.FailType {
		t.Errorf("Expected FailType %s, got %s", alert.FailType, decoded.FailType)
	}
	if decoded.Message != alert.Message {
		t.Errorf("Expected Message %s, got %s", alert.Message, decoded.Message)
	}
}

func TestStats_JSONSerialization(t *testing.T) {
	stats := Stats{
		Alerts: []Alert{
			{
				Device:    "/dev/sda",
				FailType:  "SelfTest",
				Message:   "FAILED SMART self-test",
				Timestamp: time.Date(2026, 3, 23, 10, 30, 0, 0, time.UTC),
			},
			{
				Device:    "/dev/sdb",
				FailType:  "Health",
				Message:   "Failed SMART health check",
				Timestamp: time.Date(2026, 3, 23, 11, 0, 0, 0, time.UTC),
			},
		},
		LastCheck:     "2026-03-23T12:00:00Z",
		NotifyService: "notify.mobile_phone",
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("Failed to marshal Stats: %v", err)
	}

	var decoded Stats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal Stats: %v", err)
	}

	if len(decoded.Alerts) != 2 {
		t.Errorf("Expected 2 alerts, got %d", len(decoded.Alerts))
	}
	if decoded.LastCheck != stats.LastCheck {
		t.Errorf("Expected LastCheck %s, got %s", stats.LastCheck, decoded.LastCheck)
	}
	if decoded.NotifyService != stats.NotifyService {
		t.Errorf("Expected NotifyService %s, got %s", stats.NotifyService, decoded.NotifyService)
	}
}

func TestStats_EmptyAlerts(t *testing.T) {
	stats := Stats{
		Alerts:        []Alert{},
		LastCheck:     "2026-03-23T12:00:00Z",
		NotifyService: "notify.mobile_phone",
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("Failed to marshal Stats: %v", err)
	}

	var decoded Stats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal Stats: %v", err)
	}

	if len(decoded.Alerts) != 0 {
		t.Errorf("Expected 0 alerts, got %d", len(decoded.Alerts))
	}
}

func TestAlert_FailTypes(t *testing.T) {
	failTypes := []string{"SelfTest", "Health", "Attribute"}

	for _, ft := range failTypes {
		alert := Alert{
			Device:    "/dev/sda",
			FailType:  ft,
			Message:   "Test message",
			Timestamp: time.Now(),
		}

		data, err := json.Marshal(alert)
		if err != nil {
			t.Fatalf("Failed to marshal Alert with FailType %s: %v", ft, err)
		}

		var decoded Alert
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal Alert with FailType %s: %v", ft, err)
		}

		if decoded.FailType != ft {
			t.Errorf("Expected FailType %s, got %s", ft, decoded.FailType)
		}
	}
}
