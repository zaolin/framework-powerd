package fan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

func testParams() params {
	return params{
		Min: 50, Start: 75, Full: 90,
		Critical: 90, BoardCritical: 85,
		Hysteresis: 3, Fall: 3,
		Dwell: 90 * time.Second,
	}
}

func TestDutyForTemp(t *testing.T) {
	p := testParams()
	cases := map[float64]int{
		40: 50, 75: 50, 82.5: 75, 90: 100, 95: 100,
	}
	for temp, want := range cases {
		if got := dutyForTemp(temp, p); got != want {
			t.Errorf("dutyForTemp(%v) = %d, want %d", temp, got, want)
		}
	}
}

func TestNextDutyRisesImmediately(t *testing.T) {
	p := testParams()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	duty, state := nextDuty(SensorSet{Max: 82.5}, p, curveState{Duty: 50}, now)
	if duty != 75 {
		t.Errorf("duty = %d, want 75", duty)
	}
	if !state.LastIncrease.Equal(now) {
		t.Errorf("last increase = %v, want %v", state.LastIncrease, now)
	}
}

func TestNextDutyHoldsOnSameTarget(t *testing.T) {
	p := testParams()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	last := now.Add(-10 * time.Minute)
	duty, state := nextDuty(SensorSet{Max: 82.5}, p, curveState{Duty: 75, LastIncrease: last}, now)
	if duty != 75 {
		t.Errorf("duty = %d, want 75", duty)
	}
	if !state.LastIncrease.Equal(last) {
		t.Errorf("a steady target must not reset the dwell timer")
	}
}

func TestNextDutyFallsSlowlyAfterDwell(t *testing.T) {
	p := testParams()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	// Target for 70 C is 50, so the duty may fall, but only one step.
	duty, _ := nextDuty(SensorSet{Max: 70}, p, curveState{Duty: 75, LastIncrease: now.Add(-2 * time.Minute)}, now)
	if duty != 72 {
		t.Errorf("first fall = %d, want 72", duty)
	}
}

func TestNextDutyRespectsDwell(t *testing.T) {
	p := testParams()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	duty, _ := nextDuty(SensorSet{Max: 60}, p, curveState{Duty: 75, LastIncrease: now.Add(-30 * time.Second)}, now)
	if duty != 75 {
		t.Errorf("duty = %d, want 75 during the dwell", duty)
	}
}

func TestNextDutyRespectsHysteresis(t *testing.T) {
	p := testParams()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	// 75 % corresponds to 82.5 C; at 80 C the 3 C band is not cleared yet.
	duty, _ := nextDuty(SensorSet{Max: 80}, p, curveState{Duty: 75, LastIncrease: now.Add(-10 * time.Minute)}, now)
	if duty != 75 {
		t.Errorf("duty = %d, want 75 inside the hysteresis band", duty)
	}
	// At 79 C the band is cleared.
	duty, _ = nextDuty(SensorSet{Max: 79}, p, curveState{Duty: 75, LastIncrease: now.Add(-10 * time.Minute)}, now)
	if duty != 72 {
		t.Errorf("duty = %d, want 72 below the hysteresis band", duty)
	}
}

func TestNextDutyCriticalOverride(t *testing.T) {
	p := testParams()
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	duty, _ := nextDuty(SensorSet{Max: 91}, p, curveState{Duty: 50}, now)
	if duty != 100 {
		t.Errorf("critical duty = %d, want 100", duty)
	}

	board := 85.0
	duty, _ = nextDuty(SensorSet{Max: 70, Board: &board}, p, curveState{Duty: 50}, now)
	if duty != 100 {
		t.Errorf("board critical duty = %d, want 100", duty)
	}
}

func TestECSetDutyAndAuto(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	ec := newEC(r)
	ec.root = true
	if err := ec.SetDuty(50); err != nil {
		t.Fatalf("SetDuty: %v", err)
	}
	if err := ec.Auto(); err != nil {
		t.Fatalf("Auto: %v", err)
	}
	want := [][]string{
		{"framework_tool", "--fansetduty", "50"},
		{"framework_tool", "--autofanctrl"},
	}
	calls := r.snapshot()
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if len(calls[i]) != len(want[i]) {
			t.Fatalf("call %d = %v, want %v", i, calls[i], want[i])
		}
		for j := range want[i] {
			if calls[i][j] != want[i][j] {
				t.Errorf("call %d arg %d = %q, want %q", i, j, calls[i][j], want[i][j])
			}
		}
	}
}

func TestECFallsBackToCrosEC(t *testing.T) {
	r := &fakeRunner{
		tools:      map[string]bool{"framework_tool": true},
		failDriver: "portio",
	}
	ec := newEC(r)
	ec.root = true
	if err := ec.SetDuty(40); err != nil {
		t.Fatalf("SetDuty: %v", err)
	}
	if calls := r.snapshot(); len(calls) != 2 || calls[1][1] != "--driver" || calls[1][2] != "cros-ec" {
		t.Errorf("calls = %v, want a cros-ec retry", calls)
	}
	// The next call must go straight to the working driver.
	r.reset()
	if err := ec.SetDuty(45); err != nil {
		t.Fatalf("SetDuty: %v", err)
	}
	calls := r.snapshot()
	if len(calls) != 1 || calls[0][1] != "--driver" || calls[0][2] != "cros-ec" {
		t.Errorf("calls = %v, want a single cros-ec call", calls)
	}
}

func TestReadSensors(t *testing.T) {
	root := t.TempDir()
	ec := mkHwmon(t, root, "cros_ec", map[string]string{
		"temp1_input": "48000", "temp1_label": "mainboard_power@4d",
		"temp2_input": "43000", "temp2_label": "mainboard_memory@4d",
		"temp3_input": "44000", "temp3_label": "mainboard_ambient@4d",
		"temp4_input": "50000", "temp4_label": "cpu_virtual@4c",
		"fan1_input": "1148",
	})
	cpu := mkHwmon(t, root, "k10temp", map[string]string{"temp1_input": "51000"})
	gpu := mkHwmon(t, root, "amdgpu", map[string]string{"temp1_input": "47000"})
	nvme := mkHwmon(t, root, "nvme", map[string]string{"temp1_input": "44000"})

	r := &hwmonReader{ec: ec, cpu: cpu, gpu: gpu, nvme: nvme}
	set, err := r.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if set.Board == nil || *set.Board != 48 {
		t.Errorf("board = %v, want 48", set.Board)
	}
	if set.CPUEC == nil || *set.CPUEC != 50 {
		t.Errorf("cpu_ec = %v, want 50", set.CPUEC)
	}
	if set.CPU == nil || *set.CPU != 51 {
		t.Errorf("cpu = %v, want 51", set.CPU)
	}
	if set.GPU == nil || *set.GPU != 47 {
		t.Errorf("gpu = %v, want 47", set.GPU)
	}
	if set.NVMe == nil || *set.NVMe != 44 {
		t.Errorf("nvme = %v, want 44", set.NVMe)
	}
	if set.FanRPM == nil || *set.FanRPM != 1148 {
		t.Errorf("rpm = %v, want 1148", set.FanRPM)
	}
	if set.Max != 51 {
		t.Errorf("max = %v, want 51", set.Max)
	}
}

func TestReadSensorsNoSource(t *testing.T) {
	r := &hwmonReader{}
	if _, err := r.Read(); err == nil {
		t.Error("expected an error when no sensor is readable")
	}
}

func mkHwmon(t *testing.T, root, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "name"), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	for file, value := range files {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// fakeSensors is an injectable sensor source.
type fakeSensors struct {
	set SensorSet
	err error
}

func (f *fakeSensors) Read() (SensorSet, error) { return f.set, f.err }

func TestMonitorAppliesCurve(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	sensors := &fakeSensors{set: SensorSet{Max: 82.5}}
	cfg := config.FanConfig{Enabled: true, Mode: "curve", MinDutyPercent: 50}
	m := newMonitor(cfg, testEC(r), sensors)
	m.now = func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }

	m.poll()

	stats := m.GetStats()
	if stats.DutyPercent != 75 {
		t.Errorf("duty = %d, want 75", stats.DutyPercent)
	}
	calls := r.snapshot()
	if len(calls) != 1 || calls[0][2] != "75" {
		t.Errorf("calls = %v, want one fansetduty 75", calls)
	}
	if got := m.Health(); got != "curve" {
		t.Errorf("health = %q, want curve", got)
	}
}

func TestMonitorAutoNeverWrites(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	sensors := &fakeSensors{set: SensorSet{Max: 95}}
	cfg := config.FanConfig{Enabled: true, Mode: "auto", MinDutyPercent: 50}
	m := newMonitor(cfg, testEC(r), sensors)

	m.poll()

	if got := r.snapshot(); len(got) != 0 {
		t.Errorf("calls = %v, want none in auto mode", got)
	}
	if got := m.Health(); got != "auto" {
		t.Errorf("health = %q, want auto", got)
	}
}

func TestMonitorFallsBackAfterReadFailures(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	sensors := &fakeSensors{err: errors.New("i2c timeout")}
	cfg := config.FanConfig{Enabled: true, Mode: "curve"}
	m := newMonitor(cfg, testEC(r), sensors)

	m.poll()
	m.poll()

	if got := m.GetStats().Mode; got != "auto" {
		t.Errorf("mode = %q, want auto after repeated failures", got)
	}
	calls := r.snapshot()
	if len(calls) != 1 || calls[0][1] != "--autofanctrl" {
		t.Errorf("calls = %v, want a single autofanctrl", calls)
	}
}

func TestMonitorSetMode(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	sensors := &fakeSensors{set: SensorSet{Max: 80}}
	cfg := config.FanConfig{Enabled: true, Mode: "auto", MinDutyPercent: 50}
	m := newMonitor(cfg, testEC(r), sensors)
	m.now = func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }

	if err := m.SetMode("curve"); err != nil {
		t.Fatalf("SetMode curve: %v", err)
	}
	if got := m.GetStats().Mode; got != "curve" {
		t.Errorf("mode = %q, want curve", got)
	}
	calls := r.snapshot()
	if len(calls) == 0 || calls[0][1] != "--fansetduty" {
		t.Errorf("calls = %v, want a fansetduty", calls)
	}

	if err := m.SetMode("warp"); err == nil {
		t.Error("expected an unknown mode to be rejected")
	}

	if err := m.SetMode("auto"); err != nil {
		t.Fatalf("SetMode auto: %v", err)
	}
	calls = r.snapshot()
	last := calls[len(calls)-1]
	if last[1] != "--autofanctrl" {
		t.Errorf("last call = %v, want autofanctrl", last)
	}
}

func TestMonitorHealthStates(t *testing.T) {
	if got := NewMonitor(config.FanConfig{}).Health(); got != "disabled" {
		t.Errorf("health = %q, want disabled", got)
	}

	r := &fakeRunner{tools: map[string]bool{}}
	m := newMonitor(config.FanConfig{Enabled: true}, testEC(r), &fakeSensors{})
	if got := m.Health(); got != "unsupported" {
		t.Errorf("health = %q, want unsupported", got)
	}

	r.tools["framework_tool"] = true
	m = newMonitor(config.FanConfig{Enabled: true, Mode: "curve"}, testEC(r), &fakeSensors{})
	if got := m.Health(); got != "waiting" {
		t.Errorf("health = %q, want waiting", got)
	}
}

// testEC returns an EC wrapper that believes it is privileged.
func testEC(r runner) *EC {
	ec := newEC(r)
	ec.root = true
	return ec
}

func TestECRequiresRoot(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	ec := newEC(r)
	ec.root = false
	if ec.Available() {
		t.Error("fan control must be unavailable without root")
	}
}

func TestECDetectsFailureText(t *testing.T) {
	// framework_tool exits 0 while printing an error when unprivileged.
	r := &fakeRunner{
		tools:  map[string]bool{"framework_tool": true},
		output: []byte("Must be root to get SMBIOS data.\nFailed to find SMBIOS\n"),
	}
	ec := testEC(r)
	if err := ec.SetDuty(50); err == nil {
		t.Error("expected the tool output to be treated as a failure")
	}
}

// fakeRunner records calls. failDriver makes calls that use that driver fail,
// failAuto makes --autofanctrl fail.
type fakeRunner struct {
	mu         sync.Mutex
	calls      [][]string
	tools      map[string]bool
	failDriver string
	failAuto   bool
	output     []byte
}

func (f *fakeRunner) CombinedOutput(name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string{name}, args...))
	failAuto := f.failAuto
	f.mu.Unlock()

	for _, arg := range args {
		if arg == "--autofanctrl" && failAuto {
			return nil, errors.New("autofanctrl failed")
		}
	}
	if f.failDriver != "" {
		driver := "portio"
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--driver" {
				driver = args[i+1]
			}
		}
		if driver == f.failDriver {
			return nil, errors.New("driver failed")
		}
	}
	return f.output, nil
}

func (f *fakeRunner) LookPath(name string) bool { return f.tools[name] }

func (f *fakeRunner) snapshot() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeRunner) reset() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

func (f *fakeRunner) count(arg string) int {
	n := 0
	for _, call := range f.snapshot() {
		for _, a := range call {
			if a == arg {
				n++
			}
		}
	}
	return n
}

func TestMonitorStartAutoAssertsEC(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	sensors := &fakeSensors{set: SensorSet{Max: 50}}
	m := newMonitor(config.FanConfig{Enabled: true, Mode: "auto"}, testEC(r), sensors)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.Start(ctx)

	if got := m.GetStats().Mode; got != "auto" {
		t.Errorf("mode = %q, want auto", got)
	}
	// One assertion at startup, one on Stop. Fewer means the startup path was
	// skipped; more would mean a retry fired without a failure.
	if got := r.count("--autofanctrl"); got != 2 {
		t.Errorf("autofanctrl calls = %d, want 2 (startup and stop)", got)
	}
	if got := r.count("--fansetduty"); got != 0 {
		t.Errorf("fansetduty calls = %d, want none in auto mode", got)
	}
}

func TestMonitorFallbackRetriesAuto(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}, failAuto: true}
	sensors := &fakeSensors{err: errors.New("i2c timeout")}
	m := newMonitor(config.FanConfig{Enabled: true, Mode: "curve"}, testEC(r), sensors)

	m.poll()
	m.poll()

	// The fallback call failed, so the mode must stay curve and be retried.
	if got := m.GetStats().Mode; got != "curve" {
		t.Errorf("mode = %q, want curve while autofanctrl fails", got)
	}
	if got := m.GetStats().LastError; got == "" {
		t.Error("expected the autofanctrl failure to be reported")
	}

	r.failAuto = false
	m.poll()

	if got := m.GetStats().Mode; got != "auto" {
		t.Errorf("mode = %q, want auto once autofanctrl succeeds", got)
	}
}

func TestMonitorRecoversFromFallback(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	sensors := &fakeSensors{set: SensorSet{Max: 60}}
	// The configured mode is auto; the curve was selected at runtime and then
	// demoted by a transient failure. Recovery must not depend on the config.
	m := newMonitor(config.FanConfig{Enabled: true, Mode: "auto"}, testEC(r), sensors)

	m.mu.Lock()
	m.mode = "auto"
	m.autoReason = "fallback"
	m.stats.Mode = "auto"
	m.mu.Unlock()

	for i := 0; i < recoveryReads; i++ {
		m.poll()
	}

	stats := m.GetStats()
	if stats.Mode != "curve" {
		t.Errorf("mode = %q, want curve after recovery", stats.Mode)
	}
	if got := r.count("--fansetduty"); got == 0 {
		t.Error("expected the curve to be applied after recovery")
	}
}

func TestMonitorConcurrentModeAndPoll(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	sensors := &fakeSensors{set: SensorSet{Max: 82.5}}
	m := newMonitor(config.FanConfig{Enabled: true, Mode: "curve"}, testEC(r), sensors)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			m.poll()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if err := m.SetMode("auto"); err != nil {
				t.Errorf("SetMode auto: %v", err)
			}
			if err := m.SetMode("curve"); err != nil {
				t.Errorf("SetMode curve: %v", err)
			}
		}
	}()
	wg.Wait()

	if mode := m.GetStats().Mode; mode != "auto" && mode != "curve" {
		t.Errorf("mode = %q, want auto or curve", mode)
	}
}

func TestMonitorStopRejectsSetMode(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	m := newMonitor(config.FanConfig{Enabled: true, Mode: "curve"}, testEC(r), &fakeSensors{set: SensorSet{Max: 60}})

	m.Stop()

	if got := r.count("--autofanctrl"); got != 1 {
		t.Errorf("autofanctrl calls = %d, want 1 from Stop", got)
	}
	if err := m.SetMode("curve"); err == nil {
		t.Error("expected SetMode to be rejected after Stop")
	}
}

func TestMonitorUserAutoDoesNotRecover(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	sensors := &fakeSensors{set: SensorSet{Max: 60}}
	m := newMonitor(config.FanConfig{Enabled: true, Mode: "curve"}, testEC(r), sensors)

	m.mu.Lock()
	m.mode = "auto"
	m.autoReason = "user"
	m.stats.Mode = "auto"
	m.mu.Unlock()

	for i := 0; i < recoveryReads+2; i++ {
		m.poll()
	}
	if got := m.GetStats().Mode; got != "auto" {
		t.Errorf("mode = %q, a user-selected auto must stay auto", got)
	}
}

func TestMonitorSetModeClearsPendingAuto(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	m := newMonitor(config.FanConfig{Enabled: true, Mode: "auto"}, testEC(r), &fakeSensors{set: SensorSet{Max: 60}})

	m.mu.Lock()
	m.pendingAuto = true
	m.stats.LastError = "autofanctrl failed"
	m.mu.Unlock()

	if err := m.SetMode("curve"); err != nil {
		t.Fatalf("SetMode curve: %v", err)
	}
	m.mu.RLock()
	pending := m.pendingAuto
	m.mu.RUnlock()
	if pending {
		t.Fatal("SetMode must clear the pending startup retry")
	}
	if got := m.GetStats().LastError; got != "" {
		t.Errorf("last error = %q, want cleared by a successful SetMode", got)
	}

	r.reset()
	m.poll()
	if got := r.count("--autofanctrl"); got != 0 {
		t.Errorf("autofanctrl calls = %d, want none after the pending retry was cleared", got)
	}
}

func TestMonitorRetriesPendingAuto(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}, failAuto: true}
	m := newMonitor(config.FanConfig{Enabled: true, Mode: "auto"}, testEC(r), &fakeSensors{set: SensorSet{Max: 60}})

	m.mu.Lock()
	m.pendingAuto = true
	m.stats.LastError = "autofanctrl failed"
	m.mu.Unlock()

	m.poll()
	if got := m.GetStats().LastError; got == "" {
		t.Error("expected the failed retry to stay visible")
	}

	r.failAuto = false
	m.poll()
	if got := m.GetStats().LastError; got != "" {
		t.Errorf("last error = %q, want cleared after a successful retry", got)
	}
	if m.GetStats().Mode != "auto" {
		t.Errorf("mode = %q, want auto", m.GetStats().Mode)
	}
}

func TestStatsJSONOmitsZeroTimes(t *testing.T) {
	out, err := json.Marshal(Stats{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(out)
	if strings.Contains(text, "applied_at") {
		t.Errorf("zero applied_at must be omitted: %s", text)
	}
	if strings.Contains(text, "last_update") {
		t.Errorf("zero last_update must be omitted: %s", text)
	}
}

func TestMonitorStartupAutoFailureSetsPending(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}, failAuto: true}
	m := newMonitor(config.FanConfig{Enabled: true, Mode: "auto"}, testEC(r), &fakeSensors{set: SensorSet{Max: 60}})

	m.assertStartupAuto()

	m.mu.RLock()
	pending := m.pendingAuto
	m.mu.RUnlock()
	if !pending {
		t.Fatal("a failed startup autofanctrl must schedule a retry")
	}
	if got := m.GetStats().LastError; got == "" {
		t.Error("expected the startup failure to be reported")
	}

	// A good sensor read must not mask the pending failure.
	m.poll()
	if got := m.GetStats().LastError; got == "" {
		t.Error("a good read must not clear the pending auto error")
	}

	r.failAuto = false
	m.poll()
	if got := m.GetStats().LastError; got != "" {
		t.Errorf("last error = %q, want cleared after the retry succeeds", got)
	}
}

func TestMonitorPendingAutoIgnoredInCurve(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"framework_tool": true}}
	m := newMonitor(config.FanConfig{Enabled: true, Mode: "curve"}, testEC(r), &fakeSensors{set: SensorSet{Max: 60}})

	m.mu.Lock()
	m.pendingAuto = true
	m.mu.Unlock()

	r.reset()
	m.poll()
	if got := r.count("--autofanctrl"); got != 0 {
		t.Errorf("autofanctrl calls = %d, want none while in curve mode", got)
	}
}
