package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/netaudit"
	"github.com/zaolin/framework-powerd/internal/power"
	"github.com/zaolin/framework-powerd/internal/sysinfo"
)

func TestHandleNetworkStats(t *testing.T) {
	monitor := netaudit.NewMonitor(config.NetworkAuditConfig{Enabled: true})
	server := NewServer(
		power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, nil, nil,
		sysinfo.SystemInfo{}, monitor, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/network/stats", nil)
	w := httptest.NewRecorder()
	server.HandleNetworkStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var stats netaudit.Stats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if !stats.Enabled {
		t.Error("network audit must be enabled")
	}
	if stats.Firewall.Backend != "waiting" {
		t.Errorf("firewall backend = %q, want waiting", stats.Firewall.Backend)
	}
}

func TestHandleNetworkStatsDisabled(t *testing.T) {
	server := NewServer(
		power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, nil, nil,
		sysinfo.SystemInfo{}, nil, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/network/stats", nil)
	w := httptest.NewRecorder()
	server.HandleNetworkStats(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleStatusIncludesNetwork(t *testing.T) {
	monitor := netaudit.NewMonitor(config.NetworkAuditConfig{Enabled: true})
	server := NewServer(
		power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, nil, nil,
		sysinfo.SystemInfo{}, monitor, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	server.HandleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	network, ok := response["network"].(map[string]any)
	if !ok {
		t.Fatal("status response must contain the network block")
	}
	if network["enabled"] != true {
		t.Errorf("network.enabled = %v, want true", network["enabled"])
	}
}

func TestHealthIncludesNetworkMonitor(t *testing.T) {
	monitor := netaudit.NewMonitor(config.NetworkAuditConfig{Enabled: true})
	server := NewServer(
		power.NewPowerManager(), power.NewPowerMonitor(), "", nil, nil, nil, nil, nil, nil,
		sysinfo.SystemInfo{}, monitor, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	server.HandleHealth(w, req)

	var response struct {
		Monitors struct {
			Network string `json:"network"`
		} `json:"monitors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Monitors.Network != "waiting" {
		t.Errorf("monitors.network = %q, want waiting", response.Monitors.Network)
	}
}

func TestHealthStatusNetworkError(t *testing.T) {
	if got := healthStatus("ok", 0, "", "ok", "ok", "ok", "ok", "error", "disabled"); got != "degraded" {
		t.Errorf("healthStatus() = %q, want degraded", got)
	}
}
