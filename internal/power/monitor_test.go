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
	colMap := map[string]int{"PkgWatt": 0, "CorWatt": 1, "RAMWatt": 2}
	m.updateCPUMetrics([]string{"12.5", "3.2", "1.1"}, colMap)

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
	colMap := map[string]int{"PkgWatt": 0}
	m.updateCPUMetrics([]string{"10.0"}, colMap)

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
// logic: energy_joules = (PkgWatt + RAMWatt) * 1.0 per sample (no
// peripherals attached, fallback formula).
func TestUpdateCPUMetrics_EnergyAccumulation(t *testing.T) {
	m := NewPowerMonitor()
	colMap := map[string]int{"PkgWatt": 0, "CorWatt": 1, "RAMWatt": 2}
	// PkgWatt=5.0, CorWatt=3.0 (subset, NOT added), RAMWatt=2.0
	// Without peripherals, fallback = PkgWatt + RAMWatt = 7W
	// Two samples → 14J accumulated.
	m.updateCPUMetrics([]string{"5.0", "3.0", "2.0"}, colMap) // 7W → 7J
	m.updateCPUMetrics([]string{"5.0", "3.0", "2.0"}, colMap) // 7W → 7J

	// The current hour slot should have 14J.
	// GetStatus converts to kWh: 14J / 3,600,000 = ~0.0000039 kWh.
	status := m.GetStatus()
	if status.Energy24hkWh <= 0 {
		t.Errorf("Energy24hkWh = %v, expected > 0 after accumulation", status.Energy24hkWh)
	}
	// 14J / 3.6e6 = 3.888...e-6 kWh
	expected := 14.0 / 3600000.0
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

// TestUpdateCPUMetrics_AMDColumnOrder verifies header-based parsing on AMD
// where turbostat outputs "CorWatt PkgWatt" (alphabetical), not the
// requested "PkgWatt CorWatt RAMWatt" order. Without header-based parsing,
// PkgWatt and CorWatt get swapped on AMD.
func TestUpdateCPUMetrics_AMDColumnOrder(t *testing.T) {
	m := NewPowerMonitor()
	// AMD turbostat outputs: CorWatt PkgWatt (no RAMWatt on AMD client)
	colMap := map[string]int{"CorWatt": 0, "PkgWatt": 1}
	// Data line: CorWatt=0.11, PkgWatt=8.53
	m.updateCPUMetrics([]string{"0.11", "8.53"}, colMap)

	if m.current.PkgWatt != 8.53 {
		t.Errorf("PkgWatt = %v, want 8.53 (AMD swaps column order)", m.current.PkgWatt)
	}
	if m.current.CorWatt != 0.11 {
		t.Errorf("CorWatt = %v, want 0.11", m.current.CorWatt)
	}
	if m.current.RAMWatt != 0 {
		t.Errorf("RAMWatt = %v, want 0 (not present on AMD)", m.current.RAMWatt)
	}
}

// TestUpdateCPUMetrics_IntelColumnOrder verifies header-based parsing on
// Intel where turbostat outputs "PkgWatt CorWatt RAMWatt" as requested.
func TestUpdateCPUMetrics_IntelColumnOrder(t *testing.T) {
	m := NewPowerMonitor()
	colMap := map[string]int{"PkgWatt": 0, "CorWatt": 1, "RAMWatt": 2}
	m.updateCPUMetrics([]string{"9.28", "3.2", "1.1"}, colMap)

	if m.current.PkgWatt != 9.28 {
		t.Errorf("PkgWatt = %v, want 9.28", m.current.PkgWatt)
	}
	if m.current.CorWatt != 3.2 {
		t.Errorf("CorWatt = %v, want 3.2", m.current.CorWatt)
	}
	if m.current.RAMWatt != 1.1 {
		t.Errorf("RAMWatt = %v, want 1.1", m.current.RAMWatt)
	}
}

// TestUpdateCPUMetrics_MissingColumn verifies that a missing column in the
// header (e.g. no RAMWatt on AMD) doesn't cause a panic and leaves the
// field at 0.
func TestUpdateCPUMetrics_MissingColumn(t *testing.T) {
	m := NewPowerMonitor()
	colMap := map[string]int{"CorWatt": 0, "PkgWatt": 1} // no RAMWatt
	m.updateCPUMetrics([]string{"0.05", "8.24"}, colMap)

	if m.current.PkgWatt != 8.24 {
		t.Errorf("PkgWatt = %v, want 8.24", m.current.PkgWatt)
	}
	if m.current.RAMWatt != 0 {
		t.Errorf("RAMWatt = %v, want 0 (column absent)", m.current.RAMWatt)
	}
}

// TestUpdateCPUMetrics_AMDInference verifies that during heavy GPU load
// (Ollama inference), PkgWatt is high and CorWatt stays low — confirming
// the GPU power is in PkgWatt, not CorWatt.
func TestUpdateCPUMetrics_AMDInference(t *testing.T) {
	m := NewPowerMonitor()
	colMap := map[string]int{"CorWatt": 0, "PkgWatt": 1}
	// Peak inference: CorWatt=4.79, PkgWatt=112.99
	m.updateCPUMetrics([]string{"4.79", "112.99"}, colMap)

	if m.current.PkgWatt != 112.99 {
		t.Errorf("PkgWatt = %v, want 112.99 (GPU load in PkgWatt)", m.current.PkgWatt)
	}
	if m.current.CorWatt != 4.79 {
		t.Errorf("CorWatt = %v, want 4.79 (CPU cores only)", m.current.CorWatt)
	}
}