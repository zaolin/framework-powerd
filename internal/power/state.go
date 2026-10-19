package power

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/zaolin/framework-powerd/internal/persist"
)

// metricsStateVersion is bumped whenever the persisted schema changes.
const metricsStateVersion = 1

// MetricsState is the persisted form of the aggregated energy metrics. Only
// the hour ring and the lifetime counter are stored; 24h/7d totals are derived
// from the ring on load, which keeps the file small and the math consistent.
type MetricsState struct {
	Version              int       `json:"version"`
	SavedAt              time.Time `json:"saved_at"`
	HourIndex            int       `json:"hour_index"`
	LastHourTime         time.Time `json:"last_hour_time"`
	HourlyEnergyJoules   []float64 `json:"hourly_energy_joules"`
	LifetimeEnergyJoules float64   `json:"lifetime_energy_joules"`
	LastEnergyTime       time.Time `json:"last_energy_time"`
}

// PersistenceStatus reports the state of the metrics state file for /healthz.
type PersistenceStatus struct {
	Enabled     bool   `json:"enabled"`
	StateFile   string `json:"state_file,omitempty"`
	LastSavedAt string `json:"last_saved_at,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

// SetStatePath configures where the metrics state is persisted. An empty path
// disables persistence.
func (m *PowerMonitor) SetStatePath(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statePath = path
}

// SetSaveInterval configures the periodic save interval.
func (m *PowerMonitor) SetSaveInterval(d time.Duration) {
	if d < time.Second {
		d = time.Second
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveInterval = d
}

// SnapshotState returns a consistent copy of the persisted metrics.
func (m *PowerMonitor) SnapshotState() MetricsState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	energy := make([]float64, len(m.hourlyEnergy))
	copy(energy, m.hourlyEnergy[:])

	return MetricsState{
		Version:              metricsStateVersion,
		SavedAt:              m.now().UTC(),
		HourIndex:            m.hourIndex,
		LastHourTime:         m.lastHourTime,
		HourlyEnergyJoules:   energy,
		LifetimeEnergyJoules: m.lifetimeEnergyJoules,
		LastEnergyTime:       m.lastEnergyTime,
	}
}

func sanitizeEnergy(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

// RestoreState merges a persisted snapshot into the monitor. It reports
// whether the snapshot was applied. A snapshot older than the 7-day window is
// discarded and reported as (false, nil) so a fresh start is not treated as an
// error.
func (m *PowerMonitor) RestoreState(st MetricsState) (bool, error) {
	if st.Version != metricsStateVersion {
		return false, fmt.Errorf("unsupported metrics state version %d", st.Version)
	}
	if len(st.HourlyEnergyJoules) != len(m.hourlyEnergy) {
		return false, fmt.Errorf("invalid metrics state: got %d hour slots, want %d", len(st.HourlyEnergyJoules), len(m.hourlyEnergy))
	}
	if st.HourIndex < 0 || st.HourIndex >= len(m.hourlyEnergy) {
		return false, fmt.Errorf("invalid metrics state: hour_index %d out of range", st.HourIndex)
	}

	now := m.now()
	if st.LastHourTime.IsZero() {
		return false, fmt.Errorf("invalid metrics state: missing last_hour_time")
	}
	if st.LastHourTime.After(now.Add(time.Hour)) {
		return false, fmt.Errorf("invalid metrics state: last_hour_time is in the future")
	}

	elapsed := now.Sub(st.LastHourTime)
	if elapsed < 0 {
		elapsed = 0
	}
	elapsedHours := int(elapsed / time.Hour)
	if elapsedHours >= len(m.hourlyEnergy) {
		return false, nil // older than 7 days: start fresh
	}

	energy := make([]float64, len(st.HourlyEnergyJoules))
	for i, v := range st.HourlyEnergyJoules {
		energy[i] = sanitizeEnergy(v)
	}

	// Rotate the restored ring forward to the current hour and zero the
	// slots the daemon missed while it was down.
	index := st.HourIndex
	for i := 0; i < elapsedHours; i++ {
		index = (index + 1) % len(energy)
		energy[index] = 0
	}
	lastHour := st.LastHourTime.Add(time.Duration(elapsedHours) * time.Hour)

	var sum float64
	for _, v := range energy {
		sum += v
	}
	lifetime := sanitizeEnergy(st.LifetimeEnergyJoules)

	m.mu.Lock()
	copy(m.hourlyEnergy[:], energy)
	m.hourIndex = index
	m.lastHourTime = lastHour
	m.lifetimeEnergyJoules = lifetime
	// The outage is not attributed to any request: energy integration
	// resumes from now instead of counting the downtime.
	m.lastEnergyTime = now
	m.mu.Unlock()

	log.Printf("[PowerMonitor] Restored energy metrics from state (7d=%.3f kWh, lifetime=%.3f kWh, %dh gap)",
		sum/3600000.0, lifetime/3600000.0, elapsedHours)
	return true, nil
}

// LoadState reads the metrics state file, if configured. A missing file is a
// normal fresh start and returns (false, nil).
func (m *PowerMonitor) LoadState() (bool, error) {
	m.mu.RLock()
	path := m.statePath
	m.mu.RUnlock()
	if path == "" {
		return false, nil
	}

	var st MetricsState
	if err := persist.LoadJSON(path, &st); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return m.RestoreState(st)
}

// SaveState writes the current aggregated metrics atomically.
func (m *PowerMonitor) SaveState() error {
	m.mu.RLock()
	path := m.statePath
	m.mu.RUnlock()
	if path == "" {
		return nil
	}

	err := persist.SaveJSON(path, m.SnapshotState())

	m.stateMu.Lock()
	if err == nil {
		m.lastSavedAt = m.now()
	}
	m.lastSaveErr = err
	m.stateMu.Unlock()

	return err
}

// RequestSave asks the persistence goroutine to save soon. It never blocks;
// concurrent requests are coalesced.
func (m *PowerMonitor) RequestSave() {
	select {
	case m.saveCh <- struct{}{}:
	default:
	}
}

// StartPersistence runs the periodic save loop until ctx is cancelled.
func (m *PowerMonitor) StartPersistence(ctx context.Context) {
	m.mu.RLock()
	path := m.statePath
	interval := m.saveInterval
	m.mu.RUnlock()
	if path == "" {
		return
	}

	log.Printf("[PowerMonitor] Persisting energy metrics to %s every %s", path, interval)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.saveAndLog()
			case <-m.saveCh:
				m.saveAndLog()
			}
		}
	}()
}

func (m *PowerMonitor) saveAndLog() {
	before := m.PersistenceStatus().LastError
	err := m.SaveState()
	after := m.PersistenceStatus().LastError

	if err != nil && after != before {
		log.Printf("[PowerMonitor] Failed to save metrics state: %v", err)
	}
	if err == nil && before != "" {
		log.Printf("[PowerMonitor] Metrics state persistence recovered")
	}
}

// PersistenceStatus returns the current state of the metrics file for health
// reporting.
func (m *PowerMonitor) PersistenceStatus() PersistenceStatus {
	m.mu.RLock()
	path := m.statePath
	m.mu.RUnlock()

	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	status := PersistenceStatus{
		Enabled:   path != "",
		StateFile: path,
	}
	if !m.lastSavedAt.IsZero() {
		status.LastSavedAt = m.lastSavedAt.UTC().Format(time.RFC3339)
	}
	if m.lastSaveErr != nil {
		status.LastError = m.lastSaveErr.Error()
	}
	return status
}
