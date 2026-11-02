package fan

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

// reapplyInterval re-asserts the duty even when the target has not changed, so
// an EC reset cannot leave a stale setting behind.
const reapplyInterval = 60 * time.Second

// recoveryReads is the number of consecutive good reads required before a
// fallback to auto returns to the configured curve.
const recoveryReads = 3

// Monitor drives the EC fan curve.
type Monitor struct {
	mu sync.RWMutex
	// applyMu serializes EC writes with mode changes, so a poll cannot apply a
	// manual duty after a switch to auto.
	applyMu sync.Mutex

	cfg     config.FanConfig
	ec      *EC
	sensors sensorSource
	params  params

	mode        string
	autoReason  string // user | fallback
	state       curveState
	lastApply   time.Time
	stats       Stats
	failures    int
	goodReads   int
	pendingAuto bool
	stopped     bool
	now         func() time.Time
}

// NewMonitor creates a fan monitor with the real EC and hwmon sources.
func NewMonitor(cfg config.FanConfig) *Monitor {
	return newMonitor(cfg, newEC(realRunner{}), newHwmonReader())
}

// newMonitor is the testable constructor.
func newMonitor(cfg config.FanConfig, ec *EC, sensors sensorSource) *Monitor {
	m := &Monitor{
		cfg:     cfg,
		ec:      ec,
		sensors: sensors,
		params:  paramsFromConfig(cfg),
		mode:    cfg.GetMode(),
		state:   curveState{Duty: cfg.GetMinDutyPercent()},
		now:     time.Now,
	}
	m.stats = Stats{
		Enabled:   cfg.Enabled,
		Supported: ec.Available(),
		Mode:      m.mode,
		Sensors:   SensorSet{},
	}
	return m
}

// Start polls and applies the curve until ctx is cancelled. It always returns
// the fan to EC control on exit.
func (m *Monitor) Start(ctx context.Context) {
	if !m.cfg.Enabled {
		return
	}
	defer m.Stop()

	if !m.ec.Available() {
		log.Printf("[FAN] control unavailable: framework_tool missing or not running as root")
		m.setError(errors.New("fan control requires framework_tool and root"))
		return
	}

	m.mu.Lock()
	m.stats.Supported = true
	m.mu.Unlock()

	mode := m.assertStartupAuto()

	log.Printf("[FAN] mode %s, floor %d%%, ramp %d-%d C, every %s",
		mode, m.params.Min, m.params.Start, m.params.Full, m.cfg.GetPollInterval())

	m.poll()

	ticker := time.NewTicker(m.cfg.GetPollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll()
		}
	}
}

// Stop returns the fan to EC control and rejects further mode changes.
func (m *Monitor) Stop() {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()

	if err := m.ec.Auto(); err != nil {
		log.Printf("[FAN] autofanctrl failed: %v", err)
	}

	m.mu.Lock()
	m.stats.DutyPercent = 0
	m.stats.AppliedAt = time.Time{}
	m.mu.Unlock()
}

// poll reads the sensors and applies the curve.
func (m *Monitor) poll() {
	m.retryPendingAuto()

	sensors, err := m.sensors.Read()
	now := m.now()

	if err != nil {
		m.handleReadError(err, now)
		return
	}

	m.mu.Lock()
	m.failures = 0
	m.goodReads++
	m.stats.Sensors = sensors.Clone()
	m.stats.LastUpdate = now
	if !m.pendingAuto {
		m.stats.LastError = ""
	}

	// Return from a fallback once the sensors have been stable for a while.
	if m.mode == "auto" && m.autoReason == "fallback" && m.goodReads >= recoveryReads {
		m.mode = "curve"
		m.autoReason = ""
		m.stats.Mode = "curve"
		m.state = curveState{Duty: m.stats.DutyPercent}
		log.Printf("[FAN] sensors recovered, resuming the curve")
	}

	mode := m.mode
	apply := false
	duty := m.stats.DutyPercent
	if mode == "curve" {
		next, state := nextDuty(sensors, m.params, m.state, now)
		m.state = state
		if next != m.stats.DutyPercent || now.Sub(m.lastApply) >= reapplyInterval {
			apply = true
			duty = next
		}
	}
	m.mu.Unlock()

	if !apply {
		return
	}

	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	// A mode switch or shutdown may have happened while unlocked.
	m.mu.RLock()
	blocked := m.stopped || m.mode != "curve"
	m.mu.RUnlock()
	if blocked {
		return
	}

	if err := m.ec.SetDuty(duty); err != nil {
		m.mu.Lock()
		m.stats.LastError = err.Error()
		m.mu.Unlock()
		log.Printf("[FAN] setting duty %d%% failed: %v", duty, err)
		return
	}

	m.mu.Lock()
	m.stats.DutyPercent = duty
	m.stats.AppliedAt = now
	m.lastApply = now
	m.mu.Unlock()
}

// assertStartupAuto returns the fan to EC control once when the monitor starts
// in auto mode, so a manual duty left by a previous crash cannot survive a
// restart. A failure sets pendingAuto and is retried by the poll loop.
func (m *Monitor) assertStartupAuto() string {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.RLock()
	mode := m.mode
	stopped := m.stopped
	m.mu.RUnlock()
	if mode != "auto" || stopped {
		return mode
	}

	if err := m.ec.Auto(); err != nil {
		log.Printf("[FAN] initial autofanctrl failed, will retry: %v", err)
		m.mu.Lock()
		m.pendingAuto = true
		m.stats.LastError = err.Error()
		m.mu.Unlock()
		return mode
	}
	m.enterAuto("user")
	return mode
}

// retryPendingAuto repeats a failed startup autofanctrl until the EC accepts it.
func (m *Monitor) retryPendingAuto() {
	m.mu.RLock()
	pending := m.pendingAuto
	stopped := m.stopped
	mode := m.mode
	m.mu.RUnlock()
	if !pending || stopped || mode != "auto" {
		return
	}

	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.RLock()
	pending = m.pendingAuto
	stopped = m.stopped
	mode = m.mode
	m.mu.RUnlock()
	if !pending || stopped || mode != "auto" {
		return
	}

	if err := m.ec.Auto(); err != nil {
		m.mu.Lock()
		m.stats.LastError = err.Error()
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	m.pendingAuto = false
	m.stats.LastError = ""
	m.mu.Unlock()
	log.Printf("[FAN] initial autofanctrl succeeded")
}

// handleReadError degrades to EC control after repeated failures. A failed
// autofanctrl is retried on every subsequent poll, and the mode stays curve
// until the EC has actually taken over.
func (m *Monitor) handleReadError(err error, now time.Time) {
	m.mu.Lock()
	m.failures++
	m.goodReads = 0
	m.stats.LastError = err.Error()
	m.stats.LastUpdate = now
	needFallback := m.failures >= 2 && m.mode == "curve"
	m.mu.Unlock()

	if !needFallback {
		return
	}

	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.RLock()
	still := m.failures >= 2 && m.mode == "curve" && !m.stopped
	m.mu.RUnlock()
	if !still {
		return
	}

	if autoErr := m.ec.Auto(); autoErr != nil {
		m.mu.Lock()
		m.stats.LastError = autoErr.Error()
		m.mu.Unlock()
		log.Printf("[FAN] fallback autofanctrl failed, will retry: %v", autoErr)
		return
	}
	m.enterAuto("fallback")
	log.Printf("[FAN] sensor reads failed twice, returned to EC control")
}

// enterAuto switches to EC control and clears the commanded duty.
func (m *Monitor) enterAuto(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = "auto"
	m.autoReason = reason
	m.pendingAuto = false
	m.stats.Mode = "auto"
	m.stats.DutyPercent = 0
	m.stats.AppliedAt = time.Time{}
	m.state = curveState{Duty: m.cfg.GetMinDutyPercent()}
}

// GetStats returns a snapshot.
func (m *Monitor) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats.Clone()
}

// Health returns disabled, unsupported, waiting, auto, curve or error.
func (m *Monitor) Health() string {
	if !m.cfg.Enabled {
		return "disabled"
	}
	if !m.ec.Available() {
		return "unsupported"
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.stats.LastError != "" {
		return "error"
	}
	if m.stats.LastUpdate.IsZero() {
		return "waiting"
	}
	return m.mode
}

// SetMode switches between EC automatic control and the daemon curve.
func (m *Monitor) SetMode(mode string) error {
	if mode != "auto" && mode != "curve" {
		return fmt.Errorf("fan: unknown mode %q", mode)
	}
	if !m.cfg.Enabled {
		return errors.New("fan: control is disabled")
	}
	if !m.ec.Available() {
		return errors.New("fan: framework_tool missing or not running as root")
	}

	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.RLock()
	stopped := m.stopped
	m.mu.RUnlock()
	if stopped {
		return errors.New("fan: shutting down")
	}

	if mode == "auto" {
		if err := m.ec.Auto(); err != nil {
			return fmt.Errorf("fan: return to auto: %w", err)
		}
		m.enterAuto("user")
		m.mu.Lock()
		m.pendingAuto = false
		m.stats.LastError = ""
		m.mu.Unlock()
		return nil
	}

	sensors, err := m.sensors.Read()
	if err != nil {
		return fmt.Errorf("fan: read sensors: %w", err)
	}
	now := m.now()

	m.mu.RLock()
	prev := m.state
	m.mu.RUnlock()
	duty, state := nextDuty(sensors, m.params, prev, now)

	if err := m.ec.SetDuty(duty); err != nil {
		return fmt.Errorf("fan: set duty: %w", err)
	}

	m.mu.Lock()
	m.mode = "curve"
	m.autoReason = ""
	m.failures = 0
	m.goodReads = 0
	m.state = state
	m.stats.Mode = "curve"
	m.stats.DutyPercent = duty
	m.stats.Sensors = sensors.Clone()
	m.stats.LastUpdate = now
	m.stats.AppliedAt = now
	m.stats.LastError = ""
	m.pendingAuto = false
	m.lastApply = now
	m.mu.Unlock()
	return nil
}

func (m *Monitor) setError(err error) {
	m.mu.Lock()
	m.stats.LastError = err.Error()
	m.stats.LastUpdate = m.now()
	m.mu.Unlock()
}
