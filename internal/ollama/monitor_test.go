package ollama

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/power"
)

// TestRecordRequest_RecordsByIP verifies A3: a single Ollama request is recorded
// and the monitor no longer drives the power mode directly (that responsibility
// moved to the central updatePowerState in main.go). We assert the request is
// recorded; the structural A3 guarantee (no SetPerformance/SetPowersave calls
// from the Ollama monitor) is enforced by code review + the removed code path.
func TestRecordRequest_RecordsByIP(t *testing.T) {
	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{ServiceUnit: "ollama.service"}, config.PricingConfig{})

	info := &RequestInfo{
		Timestamp: time.Now(),
		IP:        "127.0.0.1",
		Method:    "POST",
		Endpoint:  "/api/chat",
		Status:    200,
		Duration:  2 * time.Second,
	}

	mon.recordRequest(info)

	got := mon.stats.ByIP["127.0.0.1"].Count
	if got != 1 {
		t.Errorf("expected 1 request recorded for 127.0.0.1, got %d", got)
	}
}

// TestGetStats_HungEndpointBounded verifies A4: GetStats must return within a
// bounded time even when the Ollama /api/ps endpoint hangs. The production
// client has a 2s timeout; the test injects a 500ms-timeout client against a
// server that hangs for 2s and asserts GetStats returns in under ~1s.
func TestGetStats_HungEndpointBounded(t *testing.T) {
	hang := make(chan struct{})
	defer close(hang)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-time.After(10 * time.Second):
		}
	}))
	defer srv.Close()

	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{ServiceUnit: "ollama.service"}, config.PricingConfig{})
	ollamaAPIEndpoint = srv.URL + "/api/ps"
	mon.httpClient = realHTTPClient{c: &http.Client{Timeout: 500 * time.Millisecond}}

	done := make(chan struct{})
	start := time.Now()
	go func() {
		_ = mon.GetStats()
		close(done)
	}()

	select {
	case <-done:
		elapsed := time.Since(start)
		if elapsed > time.Second {
			t.Errorf("GetStats took %v, want < ~1s (500ms timeout + slack)", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("GetStats hung > 3s; HTTP poll was not bounded (A4)")
	}
}

// TestGetStats_HappyPath verifies the HTTP poll returns the loaded models.
func TestGetStats_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(psResponse{
			Models: []struct {
				Name     string `json:"name"`
				SizeVRAM int64  `json:"size_vram"`
			}{
				{Name: "llama3:8b", SizeVRAM: 4_000_000_000},
				{Name: "qwen2:7b", SizeVRAM: 5_000_000_000},
			},
		})
	}))
	defer srv.Close()

	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{ServiceUnit: "ollama.service"}, config.PricingConfig{})
	ollamaAPIEndpoint = srv.URL + "/api/ps"
	mon.httpClient = realHTTPClient{c: &http.Client{Timeout: 2 * time.Second}}

	stats := mon.GetStats()
	if len(stats.Models) != 2 {
		t.Fatalf("expected 2 models, got %d (%v)", len(stats.Models), stats.Models)
	}
	if stats.LoadedVRAMBytes != 9_000_000_000 {
		t.Errorf("loaded_vram_bytes = %d, want 9000000000", stats.LoadedVRAMBytes)
	}
}