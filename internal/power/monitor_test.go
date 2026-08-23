package power

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUpdateCPUMetrics_FloatParsing verifies T1: updateCPUMetrics correctly
// parses the PkgWatt, CorWatt, RAMWatt fields and accumulates energy.
func TestUpdateCPUMetrics_FloatParsing(t *testing.T) {
	m := NewPowerMonitor()
	m.updateCPUMetrics([]string{"12.5", "3.2", "1.1"})

	if m.current.PkgWatt != 12.5 {
		t.Errorf("PkgWatt = %v, want 12.5", m.current.PkgWatt)
	}
	if m.current.CorWatt != 3.2 {
		t.Errorf("CorWatt = %v, want 3.2", m.current.CorWatt)
	}
	if m.current.RAMWatt != 1.1 {
		t.Errorf("RAMWatt = %v, want 1.1", m.current.RAMWatt)
	}
}

// TestUpdateCPUMetrics_PartialFields verifies graceful handling of fewer
// than 3 fields (e.g., turbostat outputting only PkgWatt).
func TestUpdateCPUMetrics_PartialFields(t *testing.T) {
	m := NewPowerMonitor()
	m.updateCPUMetrics([]string{"10.0"})

	if m.current.PkgWatt != 10.0 {
		t.Errorf("PkgWatt = %v, want 10.0", m.current.PkgWatt)
	}
	if m.current.CorWatt != 0 {
		t.Errorf("CorWatt = %v, want 0", m.current.CorWatt)
	}
	if m.current.RAMWatt != 0 {
		t.Errorf("RAMWatt = %v, want 0", m.current.RAMWatt)
	}
}

// TestUpdateCPUMetrics_EnergyAccumulation verifies the energy accumulation
// logic: energy_joules = (PkgWatt + CorWatt + RAMWatt) * 1.0 per sample.
func TestUpdateCPUMetrics_EnergyAccumulation(t *testing.T) {
	m := NewPowerMonitor()
	// Two samples of 10W each → 20 joules accumulated in the current hour slot.
	m.updateCPUMetrics([]string{"5.0", "3.0", "2.0"}) // total 10W → 10J
	m.updateCPUMetrics([]string{"5.0", "3.0", "2.0"}) // total 10W → 10J

	// The current hour slot should have 20J.
	// GetStatus converts to kWh: 20J / 3,600,000 = ~0.0000056 kWh.
	status := m.GetStatus()
	if status.Energy24hkWh <= 0 {
		t.Errorf("Energy24hkWh = %v, expected > 0 after accumulation", status.Energy24hkWh)
	}
	// 20J / 3.6e6 = 5.555...e-6 kWh
	expected := 20.0 / 3600000.0
	if status.Energy24hkWh < expected*0.9 || status.Energy24hkWh > expected*1.1 {
		t.Errorf("Energy24hkWh = %v, expected ~%v", status.Energy24hkWh, expected)
	}
}

// TestGetStatus_24hWindow verifies the 24-hour window summation in GetStatus.
func TestGetStatus_24hWindow(t *testing.T) {
	m := NewPowerMonitor()

	// Pre-fill the ring buffer: put 100J in each of 24 hours starting from
	// the current hour slot going backwards.
	m.mu.Lock()
	now := time.Now()
	m.lastHourTime = now
	for i := 0; i < 24; i++ {
		idx := (m.hourIndex - i + 168) % 168
		m.hourlyEnergy[idx] = 100.0
	}
	m.mu.Unlock()

	status := m.GetStatus()
	// 24 hours × 100J = 2400J → 2400 / 3.6e6 kWh
	expected24h := 2400.0 / 3600000.0
	if status.Energy24hkWh < expected24h*0.9 || status.Energy24hkWh > expected24h*1.1 {
		t.Errorf("Energy24hkWh = %v, expected ~%v", status.Energy24hkWh, expected24h)
	}
	// 7d should include at least the 24h we filled (plus any pre-existing zeros).
	if status.Energy7dkWh < expected24h*0.9 {
		t.Errorf("Energy7dkWh = %v, expected >= ~%v", status.Energy7dkWh, expected24h)
	}
}

// TestGetStatus_EmptyRingBuffer verifies a fresh monitor reports ~0 energy.
func TestGetStatus_EmptyRingBuffer(t *testing.T) {
	m := NewPowerMonitor()
	status := m.GetStatus()

	if status.Energy24hkWh != 0 {
		t.Errorf("fresh monitor Energy24hkWh = %v, want 0", status.Energy24hkWh)
	}
	if status.Energy7dkWh != 0 {
		t.Errorf("fresh monitor Energy7dkWh = %v, want 0", status.Energy7dkWh)
	}
}

// TestGetUptimeSecondsFrom verifies T3: getUptimeSecondsFrom reads uptime from
// a temp file.
func TestGetUptimeSecondsFrom(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "uptime")
	os.WriteFile(tmpFile, []byte("3600.50 7201.00"), 0644)

	got := getUptimeSecondsFrom(tmpFile)
	if got != 3600.50 {
		t.Errorf("uptime seconds = %v, want 3600.50", got)
	}
}

// TestGetUptimeSecondsFrom_Nonexistent verifies it returns 0 for a missing file.
func TestGetUptimeSecondsFrom_Nonexistent(t *testing.T) {
	got := getUptimeSecondsFrom("/nonexistent/uptime")
	if got != 0 {
		t.Errorf("uptime seconds = %v, want 0 for missing file", got)
	}
}

// TestGetUptimeFrom verifies T3: getUptimeFrom returns a formatted duration.
func TestGetUptimeFrom(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "uptime")
	os.WriteFile(tmpFile, []byte("3600 7200"), 0644)

	got := getUptimeFrom(tmpFile)
	if got != "1h0m0s" {
		t.Errorf("uptime = %q, want 1h0m0s", got)
	}
}

// TestGetUptimeFrom_Nonexistent verifies it returns "unknown" for missing file.
func TestGetUptimeFrom_Nonexistent(t *testing.T) {
	got := getUptimeFrom("/nonexistent/uptime")
	if got != "unknown" {
		t.Errorf("uptime = %q, want unknown", got)
	}
}