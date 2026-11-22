package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/led"
	"github.com/zaolin/framework-powerd/internal/power"
	"github.com/zaolin/framework-powerd/internal/sysinfo"
)

type stubLEDRunner struct {
	available bool
}

func (s stubLEDRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}

func (s stubLEDRunner) LookPath(string) bool { return s.available }

func TestHandleLEDStats(t *testing.T) {
	controller := led.NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(stubLEDRunner{available: false})
	server := NewServer(
		power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, nil, nil,
		sysinfo.SystemInfo{}, nil, controller,
	)

	req := httptest.NewRequest(http.MethodGet, "/led/stats", nil)
	w := httptest.NewRecorder()
	server.HandleLEDStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var stats led.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Mode != "off" {
		t.Errorf("mode = %q, want off", stats.Mode)
	}
	if stats.Supported {
		t.Error("framework_tool must not be marked supported in the test environment")
	}
}

func TestHandleLEDModeOff(t *testing.T) {
	controller := led.NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(stubLEDRunner{available: false})
	server := NewServer(
		power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, nil, nil,
		sysinfo.SystemInfo{}, nil, controller,
	)

	body := bytes.NewBufferString(`{"mode":"off"}`)
	req := httptest.NewRequest(http.MethodPost, "/led/mode", body)
	w := httptest.NewRecorder()
	server.HandleLEDMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if controller.GetStats().Mode != "off" {
		t.Errorf("mode = %q, want off", controller.GetStats().Mode)
	}
}

func TestHandleLEDModeWithoutTool(t *testing.T) {
	controller := led.NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(stubLEDRunner{available: false})
	server := NewServer(
		power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, nil, nil,
		sysinfo.SystemInfo{}, nil, controller,
	)

	body := bytes.NewBufferString(`{"mode":"white"}`)
	req := httptest.NewRequest(http.MethodPost, "/led/mode", body)
	w := httptest.NewRecorder()
	server.HandleLEDMode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleLEDModeInvalid(t *testing.T) {
	controller := led.NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(stubLEDRunner{available: false})
	server := NewServer(
		power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, nil, nil,
		sysinfo.SystemInfo{}, nil, controller,
	)

	body := bytes.NewBufferString(`{"mode":"blink"}`)
	req := httptest.NewRequest(http.MethodPost, "/led/mode", body)
	w := httptest.NewRecorder()
	server.HandleLEDMode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleLEDStatsDisabled(t *testing.T) {
	server := NewServer(
		power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, nil, nil,
		sysinfo.SystemInfo{}, nil, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/led/stats", nil)
	w := httptest.NewRecorder()
	server.HandleLEDStats(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleStatusIncludesLED(t *testing.T) {
	controller := led.NewController(config.LEDConfig{Mode: "off"})
	controller.SetRunner(stubLEDRunner{available: false})
	server := NewServer(
		power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, nil, nil,
		sysinfo.SystemInfo{}, nil, controller,
	)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	server.HandleStatus(w, req)

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	ledBlock, ok := response["led"].(map[string]any)
	if !ok {
		t.Fatal("status response must contain the led block")
	}
	if ledBlock["mode"] != "off" {
		t.Errorf("led.mode = %v, want off", ledBlock["mode"])
	}
}

func TestHealthStatusLEDError(t *testing.T) {
	if got := healthStatus("ok", 0, "", "ok", "ok", "ok", "ok", "disabled", "error"); got != "degraded" {
		t.Errorf("healthStatus() = %q, want degraded", got)
	}
}
