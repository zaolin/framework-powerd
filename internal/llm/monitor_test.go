package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

// entry builds an access log entry for the given client and path.
func entry(ip, uri string, ts, duration float64) accessEntry {
	var e accessEntry
	e.Request.RemoteIP = ip
	e.Request.URI = uri
	e.TS = ts
	e.Duration = duration
	return e
}

// logLine renders a Caddy JSON access log line.
func logLine(ip, uri string, ts, duration float64) string {
	return fmt.Sprintf(
		`{"level":"info","ts":%f,"logger":"http.log.access.log0","msg":"handled request",`+
			`"request":{"remote_ip":%q,"remote_port":"59046","client_ip":%q,`+
			`"proto":"HTTP/1.1","method":"POST","uri":%q},`+
			`"bytes_read":2,"user_id":"","duration":%f,"size":104,"status":200}`+"\n",
		ts, ip, ip, uri, duration)
}

func TestMatchGroup(t *testing.T) {
	groups := []config.IPGroup{
		{Name: "home", CIDRs: []string{"192.168.0.0/16"}},
		{Name: "vpn", CIDRs: []string{"10.8.0.0/24"}},
		{Name: "single", CIDRs: []string{"10.1.2.3"}},
	}

	tests := []struct {
		ip   string
		want string
	}{
		{"192.168.1.50", "home"},
		{"192.168.255.254", "home"},
		{"10.8.0.42", "vpn"},
		{"10.1.2.3", "single"},
		{"8.8.8.8", ""},
		{"not-an-ip", ""},
	}

	for _, tc := range tests {
		if got := MatchGroup(tc.ip, groups); got != tc.want {
			t.Errorf("MatchGroup(%q) = %q, want %q", tc.ip, got, tc.want)
		}
	}
}

func TestIsExcluded(t *testing.T) {
	if !IsExcluded("127.0.0.1", nil, true) {
		t.Error("loopback should be excluded when ignoreLocal is set")
	}
	if IsExcluded("127.0.0.1", nil, false) {
		t.Error("loopback should be kept when ignoreLocal is unset")
	}
	if !IsExcluded("192.168.1.5", []string{"192.168.1.5"}, false) {
		t.Error("explicitly excluded IP should be excluded")
	}
	if IsExcluded("192.168.1.6", []string{"192.168.1.5"}, false) {
		t.Error("other IPs must not be excluded")
	}
	if !IsExcluded("garbage", nil, false) {
		t.Error("unparsable IPs should be excluded")
	}
}

func TestParseAccessEntry(t *testing.T) {
	line := logLine("192.168.1.50", "/v1/chat/completions", 1792595296.9, 12.5)

	e, ok := parseAccessEntry(line)
	if !ok {
		t.Fatal("expected the line to parse")
	}
	if e.Request.RemoteIP != "192.168.1.50" {
		t.Errorf("remote_ip = %q", e.Request.RemoteIP)
	}
	if e.Request.URI != "/v1/chat/completions" {
		t.Errorf("uri = %q", e.Request.URI)
	}
	if e.Duration != 12.5 {
		t.Errorf("duration = %v", e.Duration)
	}
	if e.TS != 1792595296.9 {
		t.Errorf("ts = %v", e.TS)
	}
}

func TestParseAccessEntry_Invalid(t *testing.T) {
	if _, ok := parseAccessEntry("not json"); ok {
		t.Error("expected non-JSON to be rejected")
	}
	if _, ok := parseAccessEntry(`{"level":"info"}`); ok {
		t.Error("expected entries without remote_ip to be rejected")
	}
}

func TestTailerReadsAndFollows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	os.WriteFile(path, []byte(logLine("1.1.1.1", "/v1/chat/completions", 1, 1)), 0644)

	tl := newTailer(path)

	// Without a persisted position the whole file is read, so a first run
	// bootstraps the history that is already on disk.
	entries, err := tl.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 on first open", len(entries))
	}
	if entries[0].Request.RemoteIP != "1.1.1.1" {
		t.Errorf("remote_ip = %q", entries[0].Request.RemoteIP)
	}

	// Append a line: it must be picked up.
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString(logLine("2.2.2.2", "/v1/chat/completions", 2, 2))
	file.Close()

	entries, err = tl.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Request.RemoteIP != "2.2.2.2" {
		t.Errorf("remote_ip = %q", entries[0].Request.RemoteIP)
	}

	// No new data: nothing to read.
	entries, err = tl.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(entries))
	}
}

func TestTailerResumesAtPersistedOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	first := logLine("1.1.1.1", "/v1/chat/completions", 1, 1)
	os.WriteFile(path, []byte(first), 0644)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	inode := fileInode(info)

	tl := newTailer(path)
	tl.resume(inode, int64(len(first)))

	entries, err := tl.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0 when already consumed", len(entries))
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString(logLine("2.2.2.2", "/v1/completions", 2, 3))
	file.Close()

	entries, err = tl.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Request.RemoteIP != "2.2.2.2" {
		t.Errorf("remote_ip = %q", entries[0].Request.RemoteIP)
	}
}

func TestTailerHandlesPartialLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	line := logLine("7.7.7.7", "/v1/chat/completions", 5, 4)
	cut := len(line) / 2

	// Create the tailer before the file exists, so it starts at offset 0 and
	// sees the fragment as it is written.
	tl := newTailer(path)

	if err := os.WriteFile(path, []byte(line[:cut]), 0644); err != nil {
		t.Fatal(err)
	}

	// The fragment is not counted yet.
	entries, err := tl.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0 for an incomplete line", len(entries))
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString(line[cut:])
	file.Close()

	entries, err = tl.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want the completed line exactly once", len(entries))
	}
	if entries[0].Request.RemoteIP != "7.7.7.7" {
		t.Errorf("remote_ip = %q", entries[0].Request.RemoteIP)
	}
}

func TestTailerHandlesRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	os.WriteFile(path, []byte(logLine("1.1.1.1", "/v1/chat/completions", 1, 1)), 0644)

	tl := newTailer(path)
	if _, err := tl.read(); err != nil {
		t.Fatalf("read: %v", err)
	}

	// Rotate: replace the file with a new inode.
	rotated := filepath.Join(dir, "access.log.1")
	os.Rename(path, rotated)
	os.WriteFile(path, []byte(logLine("3.3.3.3", "/v1/chat/completions", 3, 3)), 0644)

	entries, err := tl.read()
	if err != nil {
		t.Fatalf("read after rotation: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 after rotation", len(entries))
	}
	if entries[0].Request.RemoteIP != "3.3.3.3" {
		t.Errorf("remote_ip = %q, want the rotated file's entry", entries[0].Request.RemoteIP)
	}
}

func TestTailerDrainsRotatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	os.WriteFile(path, []byte(logLine("1.1.1.1", "/v1/chat/completions", 1, 1)), 0644)

	tl := newTailer(path)
	if _, err := tl.read(); err != nil {
		t.Fatalf("read: %v", err)
	}

	// Rotate, then write to the old file before the poller notices: the tail of
	// the rotated file must still be counted.
	rotated := filepath.Join(dir, "access.log.1")
	if err := os.Rename(path, rotated); err != nil {
		t.Fatal(err)
	}
	old, err := os.OpenFile(rotated, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	old.WriteString(logLine("2.2.2.2", "/v1/chat/completions", 2, 2))
	old.Close()

	os.WriteFile(path, []byte(logLine("3.3.3.3", "/v1/chat/completions", 3, 3)), 0644)

	entries, err := tl.read()
	if err != nil {
		t.Fatalf("read after rotation: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (tail of the old file plus the new file)", len(entries))
	}
	if entries[0].Request.RemoteIP != "2.2.2.2" || entries[1].Request.RemoteIP != "3.3.3.3" {
		t.Errorf("remote_ips = %q, %q", entries[0].Request.RemoteIP, entries[1].Request.RemoteIP)
	}
}

func TestTailerHandlesInPlaceTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	os.WriteFile(path, []byte(
		logLine("1.1.1.1", "/v1/chat/completions", 1, 1)+
			logLine("2.2.2.2", "/v1/chat/completions", 2, 2)), 0644)

	tl := newTailer(path)
	entries, err := tl.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	// copytruncate: same inode, size drops to zero.
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(logLine("3.3.3.3", "/v1/chat/completions", 3, 3)), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err = tl.read()
	if err != nil {
		t.Fatalf("read after truncation: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 after an in-place truncation", len(entries))
	}
	if entries[0].Request.RemoteIP != "3.3.3.3" {
		t.Errorf("remote_ip = %q", entries[0].Request.RemoteIP)
	}
}

func TestTailerReportsStatError(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "access.log")
	if err := os.WriteFile(path, []byte(logLine("1.1.1.1", "/v1/chat/completions", 1, 1)), 0644); err != nil {
		t.Fatal(err)
	}

	tl := newTailer(path)
	if _, err := tl.read(); err != nil {
		t.Fatalf("read: %v", err)
	}

	// Replace the parent directory with a regular file. Stat now fails with
	// ENOTDIR, which must be reported instead of silently disabling rotation
	// detection.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sub); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sub, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := tl.read(); err == nil {
		t.Error("a non-ENOENT stat error must be reported")
	}
}

func TestTailerDrainReportsErrors(t *testing.T) {
	tl := newTailer(t.TempDir())

	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	tl.file = dir
	tl.reader = bufio.NewReaderSize(dir, 1024)

	if _, err := tl.drain(); err == nil {
		t.Error("expected a read error while draining")
	}
}

func TestTailerMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.log")

	tl := newTailer(path)

	// A log that does not exist yet is not an error: there is simply nothing to
	// read, and the monitor must stay in the waiting state.
	entries, err := tl.read()
	if err != nil {
		t.Errorf("missing log file must not be an error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0", len(entries))
	}

	// Repeated polls keep returning nothing while the file is absent.
	if entries, err = tl.read(); err != nil || len(entries) != 0 {
		t.Errorf("second read = (%d entries, %v), want (0, nil)", len(entries), err)
	}

	// Once the proxy creates the file, its content is picked up.
	os.WriteFile(path, []byte(logLine("4.4.4.4", "/v1/chat/completions", 6, 1)), 0644)

	entries, err = tl.read()
	if err != nil {
		t.Fatalf("read after the file appeared: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Request.RemoteIP != "4.4.4.4" {
		t.Errorf("remote_ip = %q", entries[0].Request.RemoteIP)
	}
}

func newTestMonitor(t *testing.T) *Monitor {
	t.Helper()
	cfg := config.LLMConfig{
		Enabled:       true,
		AccessLog:     "/tmp/does-not-matter.log",
		HistoryDays:   30,
		StateFile:     filepath.Join(t.TempDir(), "llm.json"),
		SaveInterval:  "1m",
		IgnoreLocalIP: true,
		Groups: []config.IPGroup{
			{Name: "home", CIDRs: []string{"192.168.0.0/16"}},
			{Name: "vpn", CIDRs: []string{"10.8.0.0/24"}},
		},
		CountedPaths: []string{"/v1/chat/completions", "/v1/completions"},
	}
	m := NewMonitor(cfg, config.PricingConfig{EnergyPricePerKWh: 0.30, Currency: "EUR"})
	m.now = func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }
	return m
}

func TestMonitorRecordsByIPAndGroup(t *testing.T) {
	m := newTestMonitor(t)
	m.SetPowerProvider(func() float64 { return 100 })

	m.record(entry("192.168.1.50", "/v1/chat/completions", 0, 10))

	stats := m.GetStats()
	if stats.Total.Count != 1 {
		t.Fatalf("total count = %d, want 1", stats.Total.Count)
	}
	if stats.ByIP["192.168.1.50"].Count != 1 {
		t.Errorf("per-IP count = %d, want 1", stats.ByIP["192.168.1.50"].Count)
	}
	if stats.ByGroup["home"].Count != 1 {
		t.Errorf("per-group count = %d, want 1", stats.ByGroup["home"].Count)
	}
	if stats.Ungrouped.Count != 0 {
		t.Errorf("ungrouped count = %d, want 0", stats.Ungrouped.Count)
	}
	// 100 W for 10 s = 1000 J = 0.000278 kWh
	if stats.Total.TotalEnergy < 0.00027 || stats.Total.TotalEnergy > 0.00028 {
		t.Errorf("energy = %v kWh, want ~0.000278", stats.Total.TotalEnergy)
	}
	expectedCost := 0.0002777 * 0.30
	if stats.Total.TotalCost < expectedCost-0.00001 || stats.Total.TotalCost > expectedCost+0.00001 {
		t.Errorf("cost = %v, want ~%v", stats.Total.TotalCost, expectedCost)
	}
	if stats.Today.Total != 1 {
		t.Errorf("today total = %d, want 1", stats.Today.Total)
	}
	if stats.Today.TotalEnergy < 0.00027 || stats.Today.TotalEnergy > 0.00028 {
		t.Errorf("today energy = %v kWh, want ~0.000278", stats.Today.TotalEnergy)
	}
	if stats.Today.TotalCost < expectedCost-0.00001 || stats.Today.TotalCost > expectedCost+0.00001 {
		t.Errorf("today cost = %v, want ~%v", stats.Today.TotalCost, expectedCost)
	}
	if got := stats.Today.ByGroup["home"]; got != 1 {
		t.Errorf("today group count = %d, want 1", got)
	}
}

func TestMonitorIgnoresUncountedPaths(t *testing.T) {
	m := newTestMonitor(t)
	m.record(entry("192.168.1.50", "/health", 0, 1))

	if got := m.GetStats().Total.Count; got != 0 {
		t.Errorf("total count = %d, want 0", got)
	}
}

func TestMonitorStripsQueryString(t *testing.T) {
	m := newTestMonitor(t)
	m.record(entry("192.168.1.50", "/v1/chat/completions?stream=true", 0, 1))

	stats := m.GetStats()
	if stats.Total.Count != 1 {
		t.Fatalf("total count = %d, want 1", stats.Total.Count)
	}
	if stats.Total.Endpoints["/v1/chat/completions"] != 1 {
		t.Errorf("endpoints = %v, want the query stripped", stats.Total.Endpoints)
	}
}

func TestMonitorExcludesLocalhost(t *testing.T) {
	m := newTestMonitor(t)
	m.record(entry("127.0.0.1", "/v1/chat/completions", 0, 1))

	if got := m.GetStats().Total.Count; got != 0 {
		t.Error("loopback requests must be ignored")
	}
}

func TestMonitorUngrouped(t *testing.T) {
	m := newTestMonitor(t)
	m.record(entry("8.8.8.8", "/v1/chat/completions", 0, 1))

	stats := m.GetStats()
	if stats.Ungrouped.Count != 1 {
		t.Errorf("ungrouped count = %d, want 1", stats.Ungrouped.Count)
	}
	if len(stats.ByGroup) != 0 {
		t.Errorf("groups = %v, want none", stats.ByGroup)
	}
}

func TestMonitorDayBuckets(t *testing.T) {
	m := newTestMonitor(t)
	now := time.Date(2026, 10, 21, 23, 59, 0, 0, time.UTC)
	m.now = func() time.Time { return now }

	m.record(entry("192.168.1.1", "/v1/chat/completions", 0, 1))

	// Cross midnight: a new day bucket must be created.
	now = now.Add(2 * time.Minute)
	m.record(entry("192.168.1.2", "/v1/chat/completions", 0, 1))

	stats := m.GetStats()
	if len(stats.Days) != 2 {
		t.Fatalf("days = %d, want 2", len(stats.Days))
	}
	if stats.Days[0].Date != "2026-10-21" || stats.Days[1].Date != "2026-10-22" {
		t.Errorf("day dates = %v", []string{stats.Days[0].Date, stats.Days[1].Date})
	}
	if stats.Today.Total != 1 {
		t.Errorf("today total = %d, want 1", stats.Today.Total)
	}
}

func TestMonitorBucketsByLogTimestamp(t *testing.T) {
	m := newTestMonitor(t)
	// Processing time is today, but the request happened two days ago.
	m.now = func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }

	old := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	m.record(entry("192.168.1.50", "/v1/chat/completions", float64(old.Unix()), 5))

	stats := m.GetStats()
	if len(stats.Days) != 1 || stats.Days[0].Date != "2026-10-19" {
		t.Fatalf("days = %+v, want a single 2026-10-19 bucket", stats.Days)
	}
	if stats.Today.Total != 0 {
		t.Errorf("today total = %d, want 0", stats.Today.Total)
	}
	if !stats.LastRequest.Equal(old) {
		t.Errorf("last request = %v, want %v", stats.LastRequest, old)
	}
}

func TestMonitorPrunesOldDays(t *testing.T) {
	m := newTestMonitor(t)
	m.historyDays = 3
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return base.AddDate(0, 0, 4) }

	for i := 0; i < 5; i++ {
		at := base.AddDate(0, 0, i)
		m.record(entry("192.168.1.1", "/v1/chat/completions", float64(at.Unix()), 1))
	}

	stats := m.GetStats()
	if len(stats.Days) != 3 {
		t.Fatalf("days = %d, want exactly 3 (history_days counts today)", len(stats.Days))
	}
	if stats.Days[0].Date != "2026-10-03" || stats.Days[2].Date != "2026-10-05" {
		t.Errorf("kept days = %v, want 2026-10-03..2026-10-05",
			[]string{stats.Days[0].Date, stats.Days[2].Date})
	}
}

func TestMonitorPrunesIdleIPs(t *testing.T) {
	m := newTestMonitor(t)
	m.historyDays = 3
	m.now = func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }

	// Last active on the first retained day, before the current time of day:
	// must survive, because the window is counted in whole UTC days.
	firstRetained := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC)
	m.record(entry("192.168.1.10", "/v1/chat/completions", float64(firstRetained.Unix()), 1))

	// Last active before the window: must be dropped.
	outside := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	m.record(entry("192.168.1.11", "/v1/chat/completions", float64(outside.Unix()), 1))

	stats := m.GetStats()
	if _, ok := stats.ByIP["192.168.1.10"]; !ok {
		t.Error("an address active on the first retained day must be kept")
	}
	if _, ok := stats.ByIP["192.168.1.11"]; ok {
		t.Error("an address last seen outside the window must be dropped")
	}
}

func TestMonitorPrunesIdleIPsWithOneDayWindow(t *testing.T) {
	m := newTestMonitor(t)
	m.historyDays = 1
	m.now = func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }

	today := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)
	m.record(entry("192.168.1.10", "/v1/chat/completions", float64(today.Unix()), 1))

	yesterday := time.Date(2026, 10, 4, 23, 0, 0, 0, time.UTC)
	m.record(entry("192.168.1.11", "/v1/chat/completions", float64(yesterday.Unix()), 1))

	stats := m.GetStats()
	if _, ok := stats.ByIP["192.168.1.10"]; !ok {
		t.Error("today's address must be kept with history_days=1")
	}
	if _, ok := stats.ByIP["192.168.1.11"]; ok {
		t.Error("yesterday's address must be dropped with history_days=1")
	}
}

func TestMonitorPollPrunesIdleIPs(t *testing.T) {
	m := newTestMonitor(t)
	m.historyDays = 1
	m.tailer = newTailer(filepath.Join(t.TempDir(), "access.log"))

	day := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return day }
	m.record(entry("192.168.1.10", "/v1/chat/completions", float64(day.Unix()), 1))

	// Two days later there is no new traffic and no snapshot is requested, so
	// the poll itself has to apply retention.
	m.now = func() time.Time { return day.AddDate(0, 0, 2) }
	m.poll()

	m.mu.RLock()
	_, present := m.stats.ByIP["192.168.1.10"]
	m.mu.RUnlock()
	if present {
		t.Error("poll must prune per-IP counters outside the retention window")
	}
}

func TestMonitorNormalizesCurrency(t *testing.T) {
	cfg := config.LLMConfig{Enabled: true, AccessLog: filepath.Join(t.TempDir(), "x.log")}
	normalized := NewMonitor(cfg, config.PricingConfig{EnergyPricePerKWh: 0.3, Currency: "  eur "})
	if got := normalized.GetStats().Currency; got != "EUR" {
		t.Errorf("currency = %q, want EUR", got)
	}
}

func TestMonitorSaveLoadState(t *testing.T) {
	m := newTestMonitor(t)
	m.SetPowerProvider(func() float64 { return 50 })
	at := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	m.record(entry("192.168.1.50", "/v1/chat/completions", float64(at.Unix()), 4))
	m.logInode = 4242
	m.logOffset = 1234

	if err := m.SaveState(); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	restored := newTestMonitor(t)
	restored.statePath = m.statePath
	ok, err := restored.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !ok {
		t.Fatal("expected the state to be restored")
	}

	stats := restored.GetStats()
	if stats.Total.Count != 1 {
		t.Errorf("total count = %d, want 1", stats.Total.Count)
	}
	if stats.ByIP["192.168.1.50"].Count != 1 {
		t.Errorf("per-IP count = %d, want 1", stats.ByIP["192.168.1.50"].Count)
	}
	if stats.Today.Total != 1 {
		t.Errorf("today total = %d, want 1", stats.Today.Total)
	}
	if len(stats.Days) != 1 || stats.Days[0].Date != "2026-10-21" {
		t.Errorf("days = %+v", stats.Days)
	}
	// The last request time must survive, otherwise health reports "waiting"
	// until new traffic arrives.
	if !stats.LastRequest.Equal(at) {
		t.Errorf("last request = %v, want %v", stats.LastRequest, at)
	}
	if got := restored.Health(); got != "ok" {
		t.Errorf("health = %q, want ok", got)
	}
	// The log position must be restored so the replay does not re-count.
	if inode, offset := restored.tailer.resumeInode, restored.tailer.resumeOffset; inode != 4242 || offset != 1234 {
		t.Errorf("resumed at inode %d offset %d, want 4242/1234", inode, offset)
	}
}

func TestMonitorLoadStateMissingFile(t *testing.T) {
	m := newTestMonitor(t)
	ok, err := m.LoadState()
	if err != nil {
		t.Errorf("missing state file should not be an error: %v", err)
	}
	if ok {
		t.Error("missing state file must not report a restore")
	}
}

func TestMonitorLoadStateCorrupt(t *testing.T) {
	m := newTestMonitor(t)
	if err := os.WriteFile(m.statePath, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.LoadState(); err == nil {
		t.Error("a corrupt state file must be reported")
	}
}

func TestMonitorLoadStateVersionMismatch(t *testing.T) {
	m := newTestMonitor(t)
	if err := os.WriteFile(m.statePath, []byte(`{"version": 99}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.LoadState(); err == nil {
		t.Error("an unsupported state version must be reported")
	}
}

func TestMonitorSnapshotIsIsolated(t *testing.T) {
	m := newTestMonitor(t)
	m.record(entry("192.168.1.50", "/v1/chat/completions", 0, 1))

	stats := m.GetStats()
	stats.Total.Endpoints["/injected"] = 99
	stats.ByIP["192.168.1.50"].Endpoints["/injected"] = 99
	stats.Today.ByIP["10.0.0.1"] = 99
	stats.Days[0].ByIP["10.0.0.2"] = 99
	delete(stats.ByGroup, "home")

	fresh := m.GetStats()
	if _, ok := fresh.Total.Endpoints["/injected"]; ok {
		t.Error("mutating the snapshot must not change the monitor's total")
	}
	if _, ok := fresh.ByIP["192.168.1.50"].Endpoints["/injected"]; ok {
		t.Error("mutating the snapshot must not change the per-IP stats")
	}
	if _, ok := fresh.Today.ByIP["10.0.0.1"]; ok {
		t.Error("mutating the snapshot must not change today's buckets")
	}
	if _, ok := fresh.Days[0].ByIP["10.0.0.2"]; ok {
		t.Error("mutating the snapshot must not change the day history")
	}
	if _, ok := fresh.ByGroup["home"]; !ok {
		t.Error("deleting from the snapshot must not change the monitor's groups")
	}
}

func TestMonitorConcurrentRecordAndSnapshot(t *testing.T) {
	m := newTestMonitor(t)
	m.SetPowerProvider(func() float64 { return 10 })

	const workers = 4
	const iterations = 200

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				m.record(entry("192.168.1.50", "/v1/chat/completions", 0, 1))
				snapshot := m.GetStats()
				if _, err := json.Marshal(snapshot); err != nil {
					t.Errorf("marshal snapshot: %v", err)
				}
				if j%100 == 0 {
					if err := m.SaveState(); err != nil {
						t.Errorf("SaveState: %v", err)
					}
				}
			}
		}()
	}
	wg.Wait()

	if got := m.GetStats().Total.Count; got != workers*iterations {
		t.Errorf("total count = %d, want %d", got, workers*iterations)
	}
}

func newTestMonitorAt(t *testing.T, accessLog string) *Monitor {
	t.Helper()
	cfg := config.LLMConfig{
		Enabled:       true,
		AccessLog:     accessLog,
		HistoryDays:   30,
		StateFile:     filepath.Join(t.TempDir(), "llm.json"),
		SaveInterval:  "1m",
		IgnoreLocalIP: true,
		Groups: []config.IPGroup{
			{Name: "home", CIDRs: []string{"192.168.0.0/16"}},
		},
		CountedPaths: []string{"/v1/chat/completions"},
	}
	m := NewMonitor(cfg, config.PricingConfig{EnergyPricePerKWh: 0.30, Currency: "EUR"})
	m.now = func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }
	return m
}

func TestMonitorRestartDoesNotRecount(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "access.log")
	statePath := filepath.Join(dir, "llm.json")

	ts := float64(time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC).Unix())
	if err := os.WriteFile(logPath, []byte(logLine("192.168.1.50", "/v1/chat/completions", ts, 2)), 0644); err != nil {
		t.Fatal(err)
	}

	first := newTestMonitorAt(t, logPath)
	first.statePath = statePath
	first.poll()
	if got := first.GetStats().Total.Count; got != 1 {
		t.Fatalf("count = %d before restart, want 1", got)
	}
	if err := first.SaveState(); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Restart: the restored counters and the persisted log position must stop
	// the daemon from counting the same line again.
	second := newTestMonitorAt(t, logPath)
	second.statePath = statePath
	restored, err := second.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !restored {
		t.Fatal("expected the state to be restored")
	}
	second.poll()
	if got := second.GetStats().Total.Count; got != 1 {
		t.Errorf("count = %d after restart, want 1 (the log must not be replayed)", got)
	}

	// New traffic after the restart is counted exactly once.
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	file.WriteString(logLine("192.168.1.51", "/v1/chat/completions", ts+60, 2))
	file.Close()

	second.poll()
	if got := second.GetStats().Total.Count; got != 2 {
		t.Errorf("count = %d after new traffic, want 2", got)
	}
	second.poll()
	if got := second.GetStats().Total.Count; got != 2 {
		t.Errorf("count = %d after an idle poll, want 2", got)
	}
}

func TestMonitorHealth(t *testing.T) {
	m := newTestMonitor(t)
	if got := m.Health(); got != "waiting" {
		t.Errorf("health = %q, want waiting", got)
	}
	m.record(entry("192.168.1.50", "/v1/chat/completions", 0, 1))
	if got := m.Health(); got != "ok" {
		t.Errorf("health = %q, want ok", got)
	}

	disabled := NewMonitor(config.LLMConfig{}, config.PricingConfig{})
	if got := disabled.Health(); got != "disabled" {
		t.Errorf("health = %q, want disabled", got)
	}
}

func TestMonitorMissingLogStaysWaiting(t *testing.T) {
	m := newTestMonitor(t)
	m.tailer = newTailer(filepath.Join(t.TempDir(), "not-created-yet.log"))

	m.poll()

	if got := m.Health(); got != "waiting" {
		t.Errorf("health = %q, want waiting while the log does not exist", got)
	}
	if got := m.GetStats().LastError; got != "" {
		t.Errorf("last error = %q, want empty", got)
	}
}

func TestMonitorClearsStaleReadError(t *testing.T) {
	m := newTestMonitor(t)
	m.tailer = newTailer(filepath.Join(t.TempDir(), "access.log"))

	m.mu.Lock()
	m.stats.LastError = "permission denied"
	m.mu.Unlock()
	if got := m.GetStats().LastError; got == "" {
		t.Fatal("expected the injected error to be visible")
	}

	// The next successful read clears the error and returns to waiting.
	m.poll()
	if got := m.GetStats().LastError; got != "" {
		t.Errorf("last error = %q, want cleared", got)
	}
	if got := m.Health(); got != "waiting" {
		t.Errorf("health = %q, want waiting", got)
	}

	// A genuine read failure must still be reported.
	m.tailer = newTailer(t.TempDir()) // a directory cannot be read as a log file
	m.poll()
	if got := m.GetStats().LastError; got == "" {
		t.Error("expected a read error to be reported")
	}
	if got := m.Health(); got != "error" {
		t.Errorf("health = %q, want error", got)
	}
}
