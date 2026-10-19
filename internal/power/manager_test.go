package power

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestValidateTools_AllPresent verifies ValidateTools returns nil when all
// required tools are available.
func TestValidateTools_AllPresent(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })

	SetCommandRunner(&fakeRunner{
		available: map[string]bool{"powerprofilesctl": true, "scxctl": true, "powertop": true, "iw": true},
	})

	pm := NewPowerManager()
	if err := pm.ValidateTools(); err != nil {
		t.Errorf("ValidateTools() error = %v, want nil", err)
	}
}

// TestValidateTools_MissingRequired verifies ValidateTools returns an error
// when a required tool is missing.
func TestValidateTools_MissingRequired(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })

	SetCommandRunner(&fakeRunner{
		available: map[string]bool{"powerprofilesctl": false},
	})

	pm := NewPowerManager()
	err := pm.ValidateTools()
	if err == nil {
		t.Fatal("ValidateTools() expected error for missing powerprofilesctl, got nil")
	}
}

// TestSetStateAndGetStatus verifies SetState populates fields and GetStatus
// returns them correctly, including secondsUntilIdle from the idle monitor.
func TestSetStateAndGetStatus(t *testing.T) {
	pm := NewPowerManager()
	pm.SetState(true, false, true, 1234, true)

	status := pm.GetStatus()
	if !status.IsIdle {
		t.Error("IsIdle = false, want true")
	}
	if status.IsRemotePlay {
		t.Error("IsRemotePlay = true, want false")
	}
	if !status.IsGameRunning {
		t.Error("IsGameRunning = false, want true")
	}
	if status.GamePID != 1234 {
		t.Errorf("GamePID = %d, want 1234", status.GamePID)
	}
	if !status.IsGamePaused {
		t.Error("IsGamePaused = false, want true")
	}
}

// TestGetStatus_SecondsUntilIdle verifies GetStatus returns secondsUntilIdle
// from the injected idle monitor.
func TestGetStatus_SecondsUntilIdle(t *testing.T) {
	pm := NewPowerManager()
	pm.SetIdleMonitor(&mockIdleMonitor{remaining: 42 * time.Second})

	status := pm.GetStatus()
	if status.SecondsUntilIdle != 42 {
		t.Errorf("SecondsUntilIdle = %v, want 42", status.SecondsUntilIdle)
	}
}

// TestGetStatus_NoIdleMonitor verifies GetStatus returns 0 secondsUntilIdle
// when no idle monitor is set.
func TestGetStatus_NoIdleMonitor(t *testing.T) {
	pm := NewPowerManager()
	status := pm.GetStatus()
	if status.SecondsUntilIdle != 0 {
		t.Errorf("SecondsUntilIdle = %v, want 0 (no idle monitor)", status.SecondsUntilIdle)
	}
}

// TestGetCurrentMode verifies GetCurrentMode returns the committed mode.
func TestGetCurrentMode(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })
	SetCommandRunner(&fakeRunner{available: map[string]bool{"powerprofilesctl": true}})

	pm := NewPowerManager()
	if got := pm.GetCurrentMode(); got != "" {
		t.Errorf("initial mode = %q, want empty", got)
	}
	if err := pm.SetPerformance("test"); err != nil {
		t.Fatalf("SetPerformance error: %v", err)
	}
	// The APU profile trio is pinned to balanced, so the externally reported
	// mode is "balanced" regardless of the internal tunable gate.
	if got := pm.GetCurrentMode(); got != "balanced" {
		t.Errorf("mode after SetPerformance = %q, want balanced", got)
	}
}

// TestCurrentTunableState verifies CurrentTunableState exposes the internal
// performance/powersave gate while GetCurrentMode reports the pinned balanced
// profile. Peripheral estimation depends on this distinction.
func TestCurrentTunableState(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })
	SetCommandRunner(&fakeRunner{available: map[string]bool{"powerprofilesctl": true}})

	pm := NewPowerManager()
	if got := pm.CurrentTunableState(); got != "" {
		t.Errorf("initial tunable state = %q, want empty", got)
	}
	if err := pm.SetPerformance("test"); err != nil {
		t.Fatalf("SetPerformance error: %v", err)
	}
	if got := pm.CurrentTunableState(); got != "performance" {
		t.Errorf("tunable state after SetPerformance = %q, want performance", got)
	}
	if got := pm.GetCurrentMode(); got != "balanced" {
		t.Errorf("reported mode = %q, want balanced", got)
	}
	if err := pm.SetPowersave("test"); err != nil {
		t.Fatalf("SetPowersave error: %v", err)
	}
	if got := pm.CurrentTunableState(); got != "powersave" {
		t.Errorf("tunable state after SetPowersave = %q, want powersave", got)
	}
	if got := pm.GetCurrentMode(); got != "balanced" {
		t.Errorf("reported mode = %q, want balanced", got)
	}
}

// TestSetPerformance_HappyPath verifies SetPerformance commits the mode after
// the primary command succeeds and pins the balanced profile via
// powerprofilesctl.
func TestSetPerformance_HappyPath(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })
	runner := &fakeRunner{available: map[string]bool{"powerprofilesctl": true, "scxctl": true}}
	SetCommandRunner(runner)

	pm := NewPowerManager()
	if err := pm.SetPerformance("test"); err != nil {
		t.Fatalf("SetPerformance error: %v", err)
	}
	if pm.GetCurrentMode() != "balanced" {
		t.Errorf("mode = %q, want balanced", pm.GetCurrentMode())
	}
	if !contains(runner.calls, "powerprofilesctl set balanced") {
		t.Errorf("expected powerprofilesctl set balanced, calls: %v", runner.calls)
	}
}

// TestSetPowersave_HappyPath verifies SetPowersave commits the mode after
// the primary command succeeds and pins the balanced profile via
// powerprofilesctl.
func TestSetPowersave_HappyPath(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })
	runner := &fakeRunner{available: map[string]bool{"powerprofilesctl": true, "powertop": true, "iw": true}}
	SetCommandRunner(runner)

	pm := NewPowerManager()
	if err := pm.SetPowersave("test"); err != nil {
		t.Fatalf("SetPowersave error: %v", err)
	}
	if pm.GetCurrentMode() != "balanced" {
		t.Errorf("mode = %q, want balanced", pm.GetCurrentMode())
	}
	if !contains(runner.calls, "powerprofilesctl set balanced") {
		t.Errorf("expected powerprofilesctl set balanced, calls: %v", runner.calls)
	}
}

// TestSetPowersave_NoSchedulerSwitch verifies idle transitions no longer flip
// the scx scheduler — gaming mode stays pinned from the last active transition.
func TestSetPowersave_NoSchedulerSwitch(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })
	runner := &fakeRunner{available: map[string]bool{"powerprofilesctl": true, "scxctl": true}}
	SetCommandRunner(runner)

	pm := NewPowerManager()
	if err := pm.SetPowersave("test"); err != nil {
		t.Fatalf("SetPowersave error: %v", err)
	}
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "scxctl") {
			t.Errorf("SetPowersave must not touch scxctl, saw: %s", call)
		}
	}
}

// TestSetPerformance_PinsSchedulerGaming verifies SetPerformance pins scx_lavd
// to gaming mode (once, on the active transition).
func TestSetPerformance_PinsSchedulerGaming(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })
	runner := &fakeRunner{available: map[string]bool{"powerprofilesctl": true, "scxctl": true}}
	SetCommandRunner(runner)

	pm := NewPowerManager()
	if err := pm.SetPerformance("test"); err != nil {
		t.Fatalf("SetPerformance error: %v", err)
	}
	if !contains(runner.calls, "scxctl switch --sched scx_lavd --mode gaming") {
		t.Errorf("expected scxctl gaming pin, calls: %v", runner.calls)
	}
}

// TestSetDefaultActive_PinsScheduler verifies SetDefaultActive (startup path)
// pins the scheduler to gaming mode.
func TestSetDefaultActive_PinsScheduler(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })
	runner := &fakeRunner{available: map[string]bool{"powerprofilesctl": true, "scxctl": true}}
	SetCommandRunner(runner)

	pm := NewPowerManager()
	if err := pm.SetDefaultActive(); err != nil {
		t.Fatalf("SetDefaultActive error: %v", err)
	}
	if !contains(runner.calls, "scxctl switch --sched scx_lavd --mode gaming") {
		t.Errorf("expected scxctl gaming pin on startup, calls: %v", runner.calls)
	}
}

// TestSetPerformance_AlreadyInMode verifies SetPerformance is a no-op when
// already in performance mode.
func TestSetPerformance_AlreadyInMode(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })

	callCount := 0
	SetCommandRunner(&countingRunner{available: map[string]bool{"powerprofilesctl": true}, calls: &callCount})

	pm := NewPowerManager()
	_ = pm.SetPerformance("first")
	callCount = 0 // reset after setup
	_ = pm.SetPerformance("second")
	if callCount > 0 {
		t.Errorf("SetPerformance called runCommand %d times when already in mode, want 0", callCount)
	}
}

// TestTriggerActivity verifies TriggerActivity calls ResetActivity on the
// idle monitor.
func TestTriggerActivity(t *testing.T) {
	pm := NewPowerManager()
	mock := &mockIdleMonitor{}
	pm.SetIdleMonitor(mock)

	pm.TriggerActivity()

	if mock.resetCount != 1 {
		t.Errorf("ResetActivity called %d times, want 1", mock.resetCount)
	}
}

// TestTriggerActivity_NoIdleMonitor verifies TriggerActivity does not panic
// when no idle monitor is set.
func TestTriggerActivity_NoIdleMonitor(t *testing.T) {
	pm := NewPowerManager()
	pm.TriggerActivity() // should not panic
}

// --- Helpers -----

// fakeRunner is an injectable execRunner for tests. It never shells out.
type fakeRunner struct {
	available map[string]bool
	runErr    error
	calls     []string // recorded "name args..." invocations
}

func (f *fakeRunner) Run(name string, args ...string) error {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.runErr != nil {
		return f.runErr
	}
	return nil
}
func (f *fakeRunner) LookPath(name string) bool { return f.available[name] }

type mockIdleMonitor struct {
	remaining  time.Duration
	resetCount int
}

func (m *mockIdleMonitor) GetTimeUntilIdle() time.Duration { return m.remaining }
func (m *mockIdleMonitor) ResetActivity()                  { m.resetCount++ }

type countingRunner struct {
	available map[string]bool
	calls     *int
}

func (c *countingRunner) Run(name string, args ...string) error {
	*c.calls++
	return nil
}
func (c *countingRunner) LookPath(name string) bool { return c.available[name] }

// contains reports whether any recorded call equals want.
func contains(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

// TestGetNetworkDeviceStatusFromDir verifies T2: the function reads interface
// power-control status from a temp-dir mock of /sys/class/net.
func TestGetNetworkDeviceStatusFromDir(t *testing.T) {
	netDir := t.TempDir()

	// Create a real interface with device/power/control = "on"
	wlanPath := filepath.Join(netDir, "wlan0", "device", "power")
	os.MkdirAll(wlanPath, 0755)
	os.WriteFile(filepath.Join(wlanPath, "control"), []byte("on"), 0644)

	// Create a virtual interface (no "device" symlink) — should be skipped
	os.MkdirAll(filepath.Join(netDir, "virbr0"), 0755)

	// Create loopback — should be skipped
	os.MkdirAll(filepath.Join(netDir, "lo"), 0755)

	statuses := getNetworkDeviceStatusFromDir(netDir)
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status (wlan0 only), got %d: %+v", len(statuses), statuses)
	}
	if statuses[0].Interface != "wlan0" {
		t.Errorf("interface = %q, want wlan0", statuses[0].Interface)
	}
	if statuses[0].PowerControl != "on" {
		t.Errorf("power_control = %q, want on", statuses[0].PowerControl)
	}
}

// TestGetNetworkDeviceStatusFromDir_Empty verifies an empty directory returns
// no statuses.
func TestGetNetworkDeviceStatusFromDir_Empty(t *testing.T) {
	netDir := t.TempDir()
	statuses := getNetworkDeviceStatusFromDir(netDir)
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses for empty dir, got %d", len(statuses))
	}
}

// TestGetStatus_ModeReporting verifies that GetStatus reports the pinned APU
// profile ("balanced") and exposes the internal tunable gate separately.
func TestGetStatus_ModeReporting(t *testing.T) {
	orig := CommandRunner()
	t.Cleanup(func() { SetCommandRunner(orig) })
	SetCommandRunner(&fakeRunner{available: map[string]bool{"powerprofilesctl": true}})

	pm := NewPowerManager()
	status := pm.GetStatus()
	if status.Mode != "" || status.TunableMode != "" {
		t.Errorf("initial mode = %q/%q, want empty/empty", status.Mode, status.TunableMode)
	}

	if err := pm.SetPerformance("test"); err != nil {
		t.Fatalf("SetPerformance: %v", err)
	}
	status = pm.GetStatus()
	if status.Mode != "balanced" {
		t.Errorf("mode = %q, want balanced", status.Mode)
	}
	if status.TunableMode != "performance" {
		t.Errorf("tunable_mode = %q, want performance", status.TunableMode)
	}

	if err := pm.SetPowersave("test"); err != nil {
		t.Fatalf("SetPowersave: %v", err)
	}
	status = pm.GetStatus()
	if status.Mode != "balanced" {
		t.Errorf("mode = %q, want balanced", status.Mode)
	}
	if status.TunableMode != "powersave" {
		t.Errorf("tunable_mode = %q, want powersave", status.TunableMode)
	}
}
