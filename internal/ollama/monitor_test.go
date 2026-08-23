package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/power"
)

// mockJournal is an injectable journalReader for testing Start (T3).
type mockJournal struct {
	messages  []string
	msgIndex  int
	cancelled bool
}

func (m *mockJournal) Wait(timeout time.Duration) int { return 1 }
func (m *mockJournal) Next() (uint64, error) {
	if m.msgIndex >= len(m.messages) {
		return 0, nil
	}
	m.msgIndex++
	return 1, nil
}
func (m *mockJournal) GetDataValue(field string) (string, error) {
	if m.msgIndex == 0 {
		return "", nil
	}
	return m.messages[m.msgIndex-1], nil
}
func (m *mockJournal) Close() error                 { return nil }
func (m *mockJournal) AddMatch(match string) error   { return nil }
func (m *mockJournal) SeekTail() error              { return nil }
func (m *mockJournal) Previous() (uint64, error)     { return 0, nil }

// TestStartWithJournal_ProcessesMessages verifies T3: startWithJournal reads
// GIN log messages from a mock journal and records them as requests.
func TestStartWithJournal_ProcessesMessages(t *testing.T) {
	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{ServiceUnit: "ollama.service"}, config.PricingConfig{})
	ollamaAPIEndpoint = "http://localhost:0/api/ps" // won't be called

	mock := &mockJournal{
		messages: []string{
			`[GIN] 2026/01/24 - 16:57:23 | 200 | 2m36s | 127.0.0.1 | POST "/api/chat"`,
			`[GIN] 2026/01/24 - 16:58:00 | 200 | 5s | 127.0.0.1 | POST "/api/generate"`,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after the mock journal is exhausted.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_ = mon.startWithJournal(ctx, mock)

	// Verify both requests were recorded.
	mon.mu.RLock()
	defer mon.mu.RUnlock()
	if mon.stats.ByIP["127.0.0.1"].Count != 2 {
		t.Errorf("expected 2 requests for 127.0.0.1, got %d", mon.stats.ByIP["127.0.0.1"].Count)
	}
}

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

// TestGetStats_HappyPath verifies the HTTP poll returns the loaded models with
// full per-model detail from the official /api/ps response shape.
func TestGetStats_HappyPath(t *testing.T) {
	// This JSON matches the official API example from https://docs.ollama.com/api/ps
	apiResponse := `{
		"models": [
			{
				"name": "gemma4",
				"model": "gemma4",
				"size": 6591830464,
				"digest": "c6eb396dbd5992bbe3f5cdb947e8bbc0ee413d7c17e2beaae69f5d569cf982eb",
				"details": {
					"parent_model": "",
					"format": "gguf",
					"family": "gemma4",
					"families": ["gemma4"],
					"parameter_size": "8.0B",
					"quantization_level": "Q4_K_M"
				},
				"expires_at": "2025-10-17T16:47:07.93355-07:00",
				"size_vram": 5333539264,
				"context_length": 4096
			},
			{
				"name": "llama3:8b",
				"model": "llama3:8b",
				"size": 4661219072,
				"digest": "a6990ed6b5e1f3d4...",
				"details": {
					"parent_model": "",
					"format": "gguf",
					"family": "llama",
					"families": ["llama"],
					"parameter_size": "8.0B",
					"quantization_level": "Q4_K_M"
				},
				"expires_at": "",
				"size_vram": 4000000000,
				"context_length": 8192
			}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(apiResponse))
	}))
	defer srv.Close()

	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{ServiceUnit: "ollama.service"}, config.PricingConfig{})
	ollamaAPIEndpoint = srv.URL + "/api/ps"
	mon.httpClient = realHTTPClient{c: &http.Client{Timeout: 2 * time.Second}}

	stats := mon.GetStats()
	if len(stats.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(stats.Models))
	}

	// First model — full detail check
	m0 := stats.Models[0]
	if m0.Name != "gemma4" {
		t.Errorf("model[0].Name = %q, want gemma4", m0.Name)
	}
	if m0.Model != "gemma4" {
		t.Errorf("model[0].Model = %q, want gemma4", m0.Model)
	}
	if m0.Size != 6591830464 {
		t.Errorf("model[0].Size = %d, want 6591830464", m0.Size)
	}
	if m0.SizeVRAM != 5333539264 {
		t.Errorf("model[0].SizeVRAM = %d, want 5333539264", m0.SizeVRAM)
	}
	if m0.Digest == "" {
		t.Error("model[0].Digest is empty, expected non-empty")
	}
	if m0.Details.Family != "gemma4" {
		t.Errorf("model[0].Details.Family = %q, want gemma4", m0.Details.Family)
	}
	if m0.Details.ParameterSize != "8.0B" {
		t.Errorf("model[0].Details.ParameterSize = %q, want 8.0B", m0.Details.ParameterSize)
	}
	if m0.Details.QuantizationLevel != "Q4_K_M" {
		t.Errorf("model[0].Details.QuantizationLevel = %q, want Q4_K_M", m0.Details.QuantizationLevel)
	}
	if m0.Details.Format != "gguf" {
		t.Errorf("model[0].Details.Format = %q, want gguf", m0.Details.Format)
	}
	if m0.ContextLength != 4096 {
		t.Errorf("model[0].ContextLength = %d, want 4096", m0.ContextLength)
	}
	if m0.ExpiresAt == "" {
		t.Error("model[0].ExpiresAt is empty, expected non-empty")
	}

	// Second model
	m1 := stats.Models[1]
	if m1.Name != "llama3:8b" {
		t.Errorf("model[1].Name = %q, want llama3:8b", m1.Name)
	}
	if m1.Details.Family != "llama" {
		t.Errorf("model[1].Details.Family = %q, want llama", m1.Details.Family)
	}
	if m1.ContextLength != 8192 {
		t.Errorf("model[1].ContextLength = %d, want 8192", m1.ContextLength)
	}

	// Total VRAM
	expectedVRAM := int64(5333539264 + 4000000000)
	if stats.LoadedVRAMBytes != expectedVRAM {
		t.Errorf("loaded_vram_bytes = %d, want %d", stats.LoadedVRAMBytes, expectedVRAM)
	}
}

// TestGetStats_EmptyModels verifies that an empty models list is handled
// correctly (no models loaded, 0 VRAM).
func TestGetStats_EmptyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models": []}`))
	}))
	defer srv.Close()

	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{ServiceUnit: "ollama.service"}, config.PricingConfig{})
	ollamaAPIEndpoint = srv.URL + "/api/ps"
	mon.httpClient = realHTTPClient{c: &http.Client{Timeout: 2 * time.Second}}

	stats := mon.GetStats()
	if len(stats.Models) != 0 {
		t.Errorf("expected 0 models, got %d", len(stats.Models))
	}
	if stats.LoadedVRAMBytes != 0 {
		t.Errorf("loaded_vram_bytes = %d, want 0", stats.LoadedVRAMBytes)
	}
}

// TestGetStats_MalformedResponse verifies that a malformed /api/ps response
// doesn't panic and returns zero values.
func TestGetStats_MalformedResponse(t *testing.T) {
	cases := []string{
		`{}`,
		`{"models": null}`,
		`{"models": "not-an-array"}`,
		`not json at all`,
		`{"foo": "bar"}`,
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(body))
			}))
			defer srv.Close()

			pm := power.NewPowerManager()
			mon := NewMonitor(pm, nil, config.OllamaConfig{ServiceUnit: "ollama.service"}, config.PricingConfig{})
			ollamaAPIEndpoint = srv.URL + "/api/ps"
			mon.httpClient = realHTTPClient{c: &http.Client{Timeout: 2 * time.Second}}

			stats := mon.GetStats()
			// Should not panic, should return empty models.
			if len(stats.Models) != 0 {
				t.Errorf("expected 0 models for malformed response %q, got %d", body, len(stats.Models))
			}
		})
	}
}

// TestPollLoadedModels_PerModelVRAM verifies per-model VRAM is correctly
// captured (not just the sum).
func TestPollLoadedModels_PerModelVRAM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"models": [
				{"name": "model-a", "model": "model-a", "size": 100, "size_vram": 4000000000, "digest": "aaa", "details": {"family": "test"}, "context_length": 4096},
				{"name": "model-b", "model": "model-b", "size": 200, "size_vram": 6000000000, "digest": "bbb", "details": {"family": "test"}, "context_length": 8192}
			]
		}`))
	}))
	defer srv.Close()

	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{ServiceUnit: "ollama.service"}, config.PricingConfig{})
	ollamaAPIEndpoint = srv.URL + "/api/ps"
	mon.httpClient = realHTTPClient{c: &http.Client{Timeout: 2 * time.Second}}

	models, totalVRAM := mon.pollLoadedModels()
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].SizeVRAM != 4000000000 {
		t.Errorf("model[0].SizeVRAM = %d, want 4000000000", models[0].SizeVRAM)
	}
	if models[1].SizeVRAM != 6000000000 {
		t.Errorf("model[1].SizeVRAM = %d, want 6000000000", models[1].SizeVRAM)
	}
	if totalVRAM != 10000000000 {
		t.Errorf("totalVRAM = %d, want 10000000000", totalVRAM)
	}
}

// TestRecordRequest_GroupBuckets verifies that recordRequest correctly buckets
// requests into by_group when the IP matches a configured CIDR group.
func TestRecordRequest_GroupBuckets(t *testing.T) {
	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{
		ServiceUnit: "ollama.service",
		Groups: []config.IPGroup{
			{Name: "lan", CIDRs: []string{"192.168.0.0/16"}},
		},
	}, config.PricingConfig{})

	info := &RequestInfo{
		Timestamp: time.Now(),
		IP:        "192.168.1.50",
		Method:    "POST",
		Endpoint:  "/api/chat",
		Status:    200,
		Duration:  2 * time.Second,
	}

	mon.recordRequest(info)

	mon.mu.RLock()
	defer mon.mu.RUnlock()

	// Should be in by_group["lan"]
	lanStats, ok := mon.stats.ByGroup["lan"]
	if !ok {
		t.Fatal("expected by_group[\"lan\"] to exist")
	}
	if lanStats.Count != 1 {
		t.Errorf("by_group[\"lan\"].Count = %d, want 1", lanStats.Count)
	}

	// Should also be in by_ip["192.168.1.50"]
	ipStats, ok := mon.stats.ByIP["192.168.1.50"]
	if !ok {
		t.Fatal("expected by_ip[\"192.168.1.50\"] to exist")
	}
	if ipStats.Count != 1 {
		t.Errorf("by_ip count = %d, want 1", ipStats.Count)
	}

	// Ungrouped should have 0 count
	if mon.stats.Ungrouped.Count != 0 {
		t.Errorf("ungrouped.Count = %d, want 0", mon.stats.Ungrouped.Count)
	}
}

// TestRecordRequest_UngroupedBucket verifies that requests from IPs not matching
// any configured group go into ungrouped.
func TestRecordRequest_UngroupedBucket(t *testing.T) {
	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{
		ServiceUnit: "ollama.service",
		Groups: []config.IPGroup{
			{Name: "lan", CIDRs: []string{"192.168.0.0/16"}},
		},
	}, config.PricingConfig{})

	info := &RequestInfo{
		Timestamp: time.Now(),
		IP:        "10.0.0.1",
		Method:    "POST",
		Endpoint:  "/api/generate",
		Status:    200,
		Duration:  1 * time.Second,
	}

	mon.recordRequest(info)

	mon.mu.RLock()
	defer mon.mu.RUnlock()

	if mon.stats.Ungrouped.Count != 1 {
		t.Errorf("ungrouped.Count = %d, want 1", mon.stats.Ungrouped.Count)
	}
	if _, ok := mon.stats.ByGroup["lan"]; ok {
		t.Error("by_group[\"lan\"] should not exist for ungrouped IP")
	}
}

// TestRecordRequest_SkipsMonitoringEndpoints verifies that the daemon's own
// /api/ps, /api/version, and /api/tags GET requests are not counted as user
// requests. Without this filter, the daemon's own monitoring polls and the
// HA coordinator's direct Ollama fallback create a feedback loop that
// prevents idle and inflates the request count.
func TestRecordRequest_SkipsMonitoringEndpoints(t *testing.T) {
	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{ServiceUnit: "ollama.service"}, config.PricingConfig{})

	// Simulate the daemon's own monitoring polls
	mon.recordRequest(&RequestInfo{
		Timestamp: time.Now(),
		IP:        "::1",
		Method:    "GET",
		Endpoint:  "/api/ps",
		Status:    200,
		Duration:  0,
	})
	mon.recordRequest(&RequestInfo{
		Timestamp: time.Now(),
		IP:        "::1",
		Method:    "GET",
		Endpoint:  "/api/version",
		Status:    200,
		Duration:  0,
	})
	mon.recordRequest(&RequestInfo{
		Timestamp: time.Now(),
		IP:        "192.168.178.100",
		Method:    "GET",
		Endpoint:  "/api/tags",
		Status:    200,
		Duration:  0,
	})

	mon.mu.RLock()
	defer mon.mu.RUnlock()

	// No requests should be recorded
	if len(mon.stats.ByIP) != 0 {
		t.Errorf("ByIP should be empty (monitoring endpoints skipped), got %d entries", len(mon.stats.ByIP))
	}
	if mon.stats.Ungrouped.Count != 0 {
		t.Errorf("ungrouped.Count = %d, want 0 (monitoring endpoints skipped)", mon.stats.Ungrouped.Count)
	}

	// But a real user request (POST /api/chat) should still be recorded
	mon.mu.RUnlock()
	mon.recordRequest(&RequestInfo{
		Timestamp: time.Now(),
		IP:        "192.168.1.1",
		Method:    "POST",
		Endpoint:  "/api/chat",
		Status:    200,
		Duration:  2 * time.Second,
	})
	mon.mu.RLock()

	if mon.stats.ByIP["192.168.1.1"].Count != 1 {
		t.Errorf("real request should be recorded: ByIP count = %d, want 1", mon.stats.ByIP["192.168.1.1"].Count)
	}
}

// TestPollOllamaVersion_HappyPath verifies pollOllamaVersion returns the version
// from a mock /api/version endpoint.
func TestPollOllamaVersion_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version": "0.23.0"}`))
	}))
	defer srv.Close()

	mon := &Monitor{
		httpClient: realHTTPClient{c: &http.Client{Timeout: 2 * time.Second}},
	}
	ollamaVersionEndpoint = srv.URL + "/api/version"

	got := mon.pollOllamaVersion()
	if got != "0.23.0" {
		t.Errorf("pollOllamaVersion = %q, want %q", got, "0.23.0")
	}
}

// TestPollOllamaVersion_Unreachable verifies it returns "" when the endpoint
// is unreachable.
func TestPollOllamaVersion_Unreachable(t *testing.T) {
	mon := &Monitor{
		httpClient: realHTTPClient{c: &http.Client{Timeout: 500 * time.Millisecond}},
	}
	ollamaVersionEndpoint = "http://127.0.0.1:1/api/version" // unreachable port

	got := mon.pollOllamaVersion()
	if got != "" {
		t.Errorf("pollOllamaVersion = %q, want empty for unreachable endpoint", got)
	}
}

// TestPollOllamaVersion_Malformed verifies it returns "" for malformed responses.
func TestPollOllamaVersion_Malformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	mon := &Monitor{
		httpClient: realHTTPClient{c: &http.Client{Timeout: 2 * time.Second}},
	}
	ollamaVersionEndpoint = srv.URL + "/api/version"

	got := mon.pollOllamaVersion()
	if got != "" {
		t.Errorf("pollOllamaVersion = %q, want empty for malformed response", got)
	}
}

// TestGetStats_IncludesOllamaVersion verifies GetStats populates OllamaVersion.
func TestGetStats_IncludesOllamaVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "version") {
			w.Write([]byte(`{"version": "0.23.0"}`))
			return
		}
		// /api/ps
		w.Write([]byte(`{"models": []}`))
	}))
	defer srv.Close()

	pm := power.NewPowerManager()
	mon := NewMonitor(pm, nil, config.OllamaConfig{ServiceUnit: "ollama.service"}, config.PricingConfig{})
	ollamaAPIEndpoint = srv.URL + "/api/ps"
	ollamaVersionEndpoint = srv.URL + "/api/version"
	mon.httpClient = realHTTPClient{c: &http.Client{Timeout: 2 * time.Second}}

	stats := mon.GetStats()
	if stats.OllamaVersion != "0.23.0" {
		t.Errorf("OllamaVersion = %q, want %q", stats.OllamaVersion, "0.23.0")
	}
}