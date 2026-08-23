package ollama

import (
	"testing"
	"time"
)

// TestParseGINLog_ValidLine verifies T2: parseGINLog correctly extracts fields
// from a real GIN log line.
func TestParseGINLog_ValidLine(t *testing.T) {
	m := &Monitor{}
	line := `[GIN] 2026/01/24 - 16:57:23 | 200 | 2m36s | 100.76.21.125 | POST "/api/chat"`

	info := m.parseGINLog(line)
	if info == nil {
		t.Fatal("expected non-nil RequestInfo for valid GIN log line")
	}
	if info.Status != 200 {
		t.Errorf("Status = %d, want 200", info.Status)
	}
	if info.IP != "100.76.21.125" {
		t.Errorf("IP = %q, want 100.76.21.125", info.IP)
	}
	if info.Method != "POST" {
		t.Errorf("Method = %q, want POST", info.Method)
	}
	if info.Endpoint != "/api/chat" {
		t.Errorf("Endpoint = %q, want /api/chat", info.Endpoint)
	}
	expectedDur := 2*time.Minute + 36*time.Second
	if info.Duration != expectedDur {
		t.Errorf("Duration = %v, want %v", info.Duration, expectedDur)
	}
}

// TestParseGINLog_VariousStatusCodes verifies the regex handles different HTTP
// status codes (404, 500, 201, etc.).
func TestParseGINLog_VariousStatusCodes(t *testing.T) {
	m := &Monitor{}
	cases := []struct {
		line   string
		status int
	}{
		{`[GIN] 2026/01/24 - 10:00:00 | 404 | 1.2ms | 192.168.1.1 | GET "/api/ps"`, 404},
		{`[GIN] 2026/01/24 - 10:00:00 | 500 | 5s | 10.0.0.1 | POST "/api/generate"`, 500},
		{`[GIN] 2026/01/24 - 10:00:00 | 201 | 500ms | 172.16.0.1 | POST "/api/pull"`, 201},
	}
	for _, c := range cases {
		info := m.parseGINLog(c.line)
		if info == nil {
			t.Errorf("expected non-nil for line: %s", c.line)
			continue
		}
		if info.Status != c.status {
			t.Errorf("Status = %d, want %d for line: %s", info.Status, c.status, c.line)
		}
	}
}

// TestParseGINLog_InvalidLine verifies non-matching lines return nil.
func TestParseGINLog_InvalidLine(t *testing.T) {
	m := &Monitor{}
	cases := []string{
		"",
		"some random log line",
		`[GIN] 2026/01/24 - incomplete`,
	}
	for _, line := range cases {
		info := m.parseGINLog(line)
		if info != nil {
			t.Errorf("expected nil for invalid line %q, got %+v", line, info)
		}
	}
}

// TestParseDuration verifies T2: parseDuration handles various duration formats
// from GIN logs.
func TestParseDuration(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"2m36s", 2*time.Minute + 36*time.Second},
		{"10.105733868s", 10105733868 * time.Nanosecond},
		{"1.2ms", 1200 * time.Microsecond},
		{"5s", 5 * time.Second},
		{"0s", 0},
		{"garbage", 0},
		{"", 0},
	}
	for _, c := range cases {
		got := parseDuration(c.input)
		if got != c.want {
			t.Errorf("parseDuration(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}