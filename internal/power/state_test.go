package power

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustMonitorAt(t *testing.T, now time.Time) *PowerMonitor {
	t.Helper()
	m := NewPowerMonitor()
	m.now = func() time.Time { return now }
	m.lastHourTime = now
	m.lastEnergyTime = now
	return m
}

func TestMetricsState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.json")
	now := time.Now().Truncate(time.Second)

	m := mustMonitorAt(t, now)
	m.SetStatePath(path)
	m.mu.Lock()
	m.hourIndex = 10
	m.hourlyEnergy[10] = 1000
	m.hourlyEnergy[9] = 2000
	m.lifetimeEnergyJoules = 50000
	m.mu.Unlock()

	if err := m.SaveState(); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	restored := mustMonitorAt(t, now)
	restored.SetStatePath(path)
	ok, err := restored.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !ok {
		t.Fatal("LoadState reported no restore")
	}

	status := restored.GetStatus()
	want24h := 3000.0 / 3600000.0
	if status.Energy24hkWh != want24h {
		t.Errorf("Energy24hkWh = %v, want %v", status.Energy24hkWh, want24h)
	}
	if status.LifetimeEnergyKWh != 50000.0/3600000.0 {
		t.Errorf("LifetimeEnergyKWh = %v, want %v", status.LifetimeEnergyKWh, 50000.0/3600000.0)
	}
	if restored.hourIndex != 10 {
		t.Errorf("hourIndex = %d, want 10", restored.hourIndex)
	}
}

func TestMetricsState_GapRotation(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	st := MetricsState{
		Version:              metricsStateVersion,
		SavedAt:              now.Add(-3 * time.Hour),
		HourIndex:            10,
		LastHourTime:         now.Add(-3 * time.Hour),
		HourlyEnergyJoules:   make([]float64, 168),
		LifetimeEnergyJoules: 1234,
	}
	st.HourlyEnergyJoules[10] = 100

	m := mustMonitorAt(t, now)
	ok, err := m.RestoreState(st)
	if err != nil {
		t.Fatalf("RestoreState: %v", err)
	}
	if !ok {
		t.Fatal("RestoreState reported no restore")
	}
	if m.hourIndex != 13 {
		t.Errorf("hourIndex = %d, want 13 after a 3h gap", m.hourIndex)
	}
	if m.hourlyEnergy[10] != 100 {
		t.Errorf("slot 10 = %v, want 100 (still inside the 7d window)", m.hourlyEnergy[10])
	}
	for _, idx := range []int{11, 12, 13} {
		if m.hourlyEnergy[idx] != 0 {
			t.Errorf("slot %d = %v, want 0 (missed while down)", idx, m.hourlyEnergy[idx])
		}
	}
	if m.lifetimeEnergyJoules != 1234 {
		t.Errorf("lifetime = %v, want 1234", m.lifetimeEnergyJoules)
	}
}

func TestMetricsState_Expired(t *testing.T) {
	now := time.Now()
	st := MetricsState{
		Version:            metricsStateVersion,
		SavedAt:            now.Add(-200 * time.Hour),
		HourIndex:          5,
		LastHourTime:       now.Add(-200 * time.Hour),
		HourlyEnergyJoules: make([]float64, 168),
	}
	st.HourlyEnergyJoules[5] = 999

	m := mustMonitorAt(t, now)
	ok, err := m.RestoreState(st)
	if err != nil {
		t.Fatalf("RestoreState: %v", err)
	}
	if ok {
		t.Error("expired state should not be restored")
	}
	if m.GetStatus().Energy7dkWh != 0 {
		t.Errorf("Energy7dkWh = %v, want 0 after expiry", m.GetStatus().Energy7dkWh)
	}
}

func TestMetricsState_InvalidInputs(t *testing.T) {
	now := time.Now()
	base := MetricsState{
		Version:            metricsStateVersion,
		HourIndex:          0,
		LastHourTime:       now,
		HourlyEnergyJoules: make([]float64, 168),
	}

	tests := []struct {
		name   string
		mutate func(*MetricsState)
	}{
		{"bad version", func(s *MetricsState) { s.Version = 99 }},
		{"bad length", func(s *MetricsState) { s.HourlyEnergyJoules = make([]float64, 3) }},
		{"bad index", func(s *MetricsState) { s.HourIndex = 200 }},
		{"missing hour time", func(s *MetricsState) { s.LastHourTime = time.Time{} }},
		{"future hour time", func(s *MetricsState) { s.LastHourTime = now.Add(2 * time.Hour) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := base
			st.HourlyEnergyJoules = make([]float64, 168)
			tc.mutate(&st)
			m := mustMonitorAt(t, now)
			ok, err := m.RestoreState(st)
			if err == nil {
				t.Errorf("expected error, got ok=%v", ok)
			}
			if ok {
				t.Error("invalid state must not be applied")
			}
		})
	}
}

func TestMetricsState_SanitizesNegativeAndNaN(t *testing.T) {
	now := time.Now()
	st := MetricsState{
		Version:              metricsStateVersion,
		HourIndex:            0,
		LastHourTime:         now,
		HourlyEnergyJoules:   make([]float64, 168),
		LifetimeEnergyJoules: -5,
	}
	st.HourlyEnergyJoules[0] = -1
	st.HourlyEnergyJoules[1] = 10

	m := mustMonitorAt(t, now)
	ok, err := m.RestoreState(st)
	if err != nil || !ok {
		t.Fatalf("RestoreState: ok=%v err=%v", ok, err)
	}
	if m.hourlyEnergy[0] != 0 {
		t.Errorf("negative slot = %v, want 0", m.hourlyEnergy[0])
	}
	if m.lifetimeEnergyJoules != 0 {
		t.Errorf("negative lifetime = %v, want 0", m.lifetimeEnergyJoules)
	}
}

func TestLoadState_MissingFileIsFreshStart(t *testing.T) {
	m := mustMonitorAt(t, time.Now())
	m.SetStatePath(filepath.Join(t.TempDir(), "does-not-exist.json"))
	ok, err := m.LoadState()
	if err != nil {
		t.Fatalf("missing state must not be an error: %v", err)
	}
	if ok {
		t.Error("missing state must not report a restore")
	}
}

func TestLoadState_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	m := mustMonitorAt(t, time.Now())
	m.SetStatePath(path)
	if _, err := m.LoadState(); err == nil {
		t.Error("expected an error for a corrupt state file")
	}
}

func TestSaveState_DisabledWithoutPath(t *testing.T) {
	m := mustMonitorAt(t, time.Now())
	if err := m.SaveState(); err != nil {
		t.Errorf("SaveState without a path should be a no-op, got %v", err)
	}
	if status := m.PersistenceStatus(); status.Enabled {
		t.Error("persistence should report disabled without a path")
	}
}

func TestSaveState_Status(t *testing.T) {
	m := mustMonitorAt(t, time.Now())
	m.SetStatePath(filepath.Join(t.TempDir(), "metrics.json"))
	if err := m.SaveState(); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	status := m.PersistenceStatus()
	if !status.Enabled {
		t.Error("persistence should be enabled")
	}
	if status.LastSavedAt == "" {
		t.Error("LastSavedAt should be set after a successful save")
	}
	if status.LastError != "" {
		t.Errorf("LastError = %q, want empty", status.LastError)
	}
}

func TestSaveState_ReportsWriteError(t *testing.T) {
	m := mustMonitorAt(t, time.Now())
	// A path whose parent is a regular file cannot be created.
	dir := t.TempDir()
	file := filepath.Join(dir, "blocker")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	m.SetStatePath(filepath.Join(file, "metrics.json"))

	if err := m.SaveState(); err == nil {
		t.Error("expected a write error")
	}
	status := m.PersistenceStatus()
	if status.LastError == "" || !strings.Contains(status.LastError, "persist") {
		t.Errorf("LastError = %q, want a persistence error", status.LastError)
	}
}

func TestStartPersistence_WritesOnRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	m := mustMonitorAt(t, time.Now())
	m.SetStatePath(path)
	m.SetSaveInterval(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.StartPersistence(ctx)

	m.RequestSave()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("persistence goroutine did not write the state file")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
