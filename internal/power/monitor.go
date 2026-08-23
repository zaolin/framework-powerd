package power

import (
	"bufio"
	"context"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zaolin/framework-powerd/internal/peripherals"
)

// PowerMetrics holds the current power consumption values in Watts
type PowerMetrics struct {
	PkgWatt             float64 `json:"pkg_watt"`
	CorWatt             float64 `json:"cor_watt"`
	GFXWatt             float64 `json:"gfx_watt"`
	RAMWatt             float64 `json:"ram_watt"`
	EstimatedTotalWatts float64 `json:"estimated_total_watts"`
	PeripheralWatts    float64 `json:"peripheral_watts"`
}

// PowerStatus contains the full status response
type PowerStatus struct {
	Current      PowerMetrics `json:"current"`
	Energy24hkWh float64      `json:"energy_24h_kwh"`
	Energy7dkWh  float64      `json:"energy_7d_kwh"`
}

// PowerMonitor handles reading from turbostat and aggregating history
type PowerMonitor struct {
	mu      sync.RWMutex
	current PowerMetrics

	// peripherals is the auto-detected peripheral power estimator.
	// May be nil if not configured — in that case, only PkgWatt is used.
	peripherals *peripherals.PeripheralEstimator

	// modeProvider returns the current power mode ("performance" or "powersave")
	// for peripheral estimation. May be nil.
	modeProvider func() string

	// History Tracking
	// We use a ring buffer of 168 hours (7 days)
	// hourlyEnergy[0] is the current hour being accumulated
	hourlyEnergy [168]float64 // Energy in Joules for each hour window
	hourIndex    int          // Current index in the ring buffer
	lastHourTime time.Time    // Timestamp of when the current hour started
}

// NewPowerMonitor creates a new monitor instance
func NewPowerMonitor() *PowerMonitor {
	return &PowerMonitor{
		lastHourTime: time.Now(),
	}
}

// SetPeripherals attaches a peripheral power estimator.
func (m *PowerMonitor) SetPeripherals(est *peripherals.PeripheralEstimator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peripherals = est
}

// SetModeProvider sets a callback that returns the current power mode
// ("performance" or "powersave") for peripheral estimation.
func (m *PowerMonitor) SetModeProvider(fn func() string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modeProvider = fn
}

// Start begins the monitoring loop (CPU)
func (m *PowerMonitor) Start(ctx context.Context) {
	// Start CPU Monitoring (Turbostat) - Streamed
	go m.runTurbostat(ctx)
}

func (m *PowerMonitor) runTurbostat(ctx context.Context) {
	if _, err := exec.LookPath("turbostat"); err != nil {
		log.Println("[PowerMonitor] turbostat not found. CPU power monitoring disabled.")
		return
	}

	// Command: turbostat --Summary --quiet --show PkgWatt,CorWatt,RAMWatt --interval 1
	cmd := exec.CommandContext(ctx, "turbostat", "--Summary", "--quiet", "--show", "PkgWatt,CorWatt,RAMWatt", "--interval", "1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[PowerMonitor] Failed to create stdout pipe: %v\n", err)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[PowerMonitor] Failed to start turbostat: %v\n", err)
		return
	}

	log.Println("[PowerMonitor] Started CPU monitoring (turbostat).")

	scanner := bufio.NewScanner(stdout)

	// Read the header line and build a column-name → index map.
	// turbostat may output columns in a different order than requested
	// (e.g. AMD outputs "CorWatt PkgWatt" alphabetically, not
	// "PkgWatt CorWatt RAMWatt" as requested). Parsing by header name
	// instead of position fixes the column-swap bug on AMD.
	var colMap map[string]int
	if scanner.Scan() {
		header := scanner.Text()
		cols := strings.Fields(header)
		colMap = make(map[string]int, len(cols))
		for i, col := range cols {
			colMap[col] = i
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		m.updateCPUMetrics(parts, colMap)
	}

	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		log.Printf("[PowerMonitor] turbostat exited unexpectedly: %v\n", err)
	}
}

func (m *PowerMonitor) updateCPUMetrics(parts []string, colMap map[string]int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	parseFloat := func(s string) float64 {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}

	// Parse by column name (header-based), not by position.
	// This fixes the column-swap bug where AMD turbostat outputs
	// "CorWatt PkgWatt" (alphabetical) instead of the requested order.
	parseCol := func(name string) float64 {
		if idx, ok := colMap[name]; ok && idx < len(parts) {
			return parseFloat(parts[idx])
		}
		return 0
	}
	m.current.PkgWatt = parseCol("PkgWatt")
	m.current.CorWatt = parseCol("CorWatt")
	m.current.RAMWatt = parseCol("RAMWatt")

	// Energy accumulation: PkgWatt is the total CPU SoC package power
	// (cores + iGPU + uncore + memory controller). CorWatt is a SUBSET
	// of PkgWatt (just the cores) — adding it would double-count.
	// RAMWatt is separate (DRAM DIMMs) but may be zero on AMD client CPUs.
	//
	// If a peripheral estimator is attached, add the estimated peripheral
	// power (USB, NVMe, fan, WiFi, Ethernet) and apply VRM correction.
	// Otherwise, use PkgWatt + RAMWatt as the base.
	var energyJoules float64
	if m.peripherals != nil && m.modeProvider != nil {
		mode := m.modeProvider()
		peripheralWatts := m.peripherals.EstimateWatts(mode)
		m.current.PeripheralWatts = peripheralWatts

		subtotal := m.current.PkgWatt + peripheralWatts
		totalWatts := peripherals.ApplyVRMCorrection(subtotal, m.peripherals.VRMLossPercent())
		m.current.EstimatedTotalWatts = totalWatts
		energyJoules = totalWatts * 1.0
	} else {
		// Fallback: PkgWatt + RAMWatt (if available), no peripherals
		base := m.current.PkgWatt + m.current.RAMWatt
		m.current.EstimatedTotalWatts = base
		m.current.PeripheralWatts = 0
		energyJoules = base * 1.0
	}

	now := time.Now()
	if now.Sub(m.lastHourTime) >= time.Hour {
		m.hourIndex = (m.hourIndex + 1) % 168
		m.hourlyEnergy[m.hourIndex] = 0
		m.lastHourTime = now
	}
	m.hourlyEnergy[m.hourIndex] += energyJoules
}

// GetStatus returns the current power stats
func (m *PowerMonitor) GetStatus() PowerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sum24h, sum7d float64

	for i := 0; i < 168; i++ {
		sum7d += m.hourlyEnergy[i]
	}

	idx := m.hourIndex
	for i := 0; i < 24; i++ {
		sum24h += m.hourlyEnergy[idx]
		idx--
		if idx < 0 {
			idx = 167
		}
	}

	// Convert Joules to kWh
	// 1 kWh = 3,600,000 Joules
	conversion := 3600000.0

	return PowerStatus{
		Current:      m.current,
		Energy24hkWh: sum24h / conversion,
		Energy7dkWh:  sum7d / conversion,
	}
}

// GetUptimeSeconds returns the system uptime in seconds
func GetUptimeSeconds() float64 {
	return getUptimeSecondsFrom("/proc/uptime")
}

// getUptimeSecondsFrom reads uptime from the given path and returns seconds.
// Extracted so it is testable with a temp file (T3).
func getUptimeSecondsFrom(procPath string) float64 {
	data, err := os.ReadFile(procPath)
	if err != nil {
		return 0
	}

	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return 0
	}

	secs, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}

	return secs
}

// GetUptime returns the system uptime as a formatted string
func GetUptime() string {
	return getUptimeFrom("/proc/uptime")
}

// getUptimeFrom reads uptime from the given path and returns a formatted
// duration string. Extracted so it is testable (T3).
func getUptimeFrom(procPath string) string {
	secs := getUptimeSecondsFrom(procPath)
	if secs == 0 {
		return "unknown"
	}
	d := time.Duration(secs) * time.Second
	return d.String()
}
