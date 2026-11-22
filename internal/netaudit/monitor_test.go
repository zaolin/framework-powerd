package netaudit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

func TestMonitorDisabled(t *testing.T) {
	monitor := NewMonitor(config.NetworkAuditConfig{Enabled: false, PollInterval: "30s"})
	monitor.Start(context.Background())
	if monitor.Health() != "disabled" {
		t.Errorf("health = %q, want disabled", monitor.Health())
	}
}

func TestMonitorPoll(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(procNetTCPFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "4242")
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[12345]", filepath.Join(dir, "fd", "10")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte("sshd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup"), []byte("0::/system.slice/ssh.service\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	monitor := NewMonitor(config.NetworkAuditConfig{Enabled: true, PollInterval: "1s"})
	monitor.procRoot = root
	monitor.runtimeStat = func(string) error { return os.ErrNotExist }
	monitor.runner = &fakeFirewallRunner{available: map[string]bool{}}
	monitor.now = func() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) }
	monitor.poll(context.Background())

	stats := monitor.GetStats()
	if len(stats.Listening) != 1 {
		t.Fatalf("listening = %v, want one socket", stats.Listening)
	}
	got := stats.Listening[0]
	if got.Port != 22 || got.Process != "sshd" || got.Unit != "ssh.service" || got.PID != 4242 {
		t.Errorf("unexpected socket: %+v", got)
	}
	if stats.Firewall.Backend != "unsupported" {
		t.Errorf("firewall backend = %q, want unsupported", stats.Firewall.Backend)
	}
	if monitor.Health() != "ok" {
		t.Errorf("health = %q, want ok", monitor.Health())
	}
	if stats.LastUpdate.IsZero() {
		t.Error("last_update must be set")
	}
}

func TestMonitorReportsProcError(t *testing.T) {
	monitor := NewMonitor(config.NetworkAuditConfig{Enabled: true})
	monitor.procRoot = filepath.Join(t.TempDir(), "missing")
	monitor.runtimeStat = func(string) error { return os.ErrNotExist }
	monitor.runner = &fakeFirewallRunner{available: map[string]bool{}}
	monitor.poll(context.Background())

	if monitor.Health() != "error" {
		t.Errorf("health = %q, want error", monitor.Health())
	}
	if monitor.GetStats().LastError == "" {
		t.Error("last_error must describe the failed procfs read")
	}
}
