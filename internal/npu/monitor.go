package npu

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

// queryFunc collects one runtime snapshot from the device.
type queryFunc func(dev Device) (Stats, error)

// Monitor polls the AMD XDNA NPU through the accel DRM interface.
type Monitor struct {
	mu sync.RWMutex

	cfg      config.NPUConfig
	device   Device
	present  bool
	interval time.Duration
	stats    Stats
	query    queryFunc
	now      func() time.Time
}

// NewMonitor creates a monitor. A missing NPU is not an error: the monitor
// reports itself as absent.
func NewMonitor(cfg config.NPUConfig) *Monitor {
	dev, present := Detect(cfg.Device)
	m := &Monitor{
		cfg:      cfg,
		device:   dev,
		present:  present,
		interval: cfg.GetPollInterval(),
		query:    queryDevice,
		now:      time.Now,
	}
	m.stats = Stats{Present: present, Device: dev.Path}
	return m
}

// Present reports whether an NPU was detected.
func (m *Monitor) Present() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.present
}

// Start polls until ctx is cancelled.
func (m *Monitor) Start(ctx context.Context) {
	if !m.cfg.Enabled {
		return
	}
	if m.Present() {
		m.mu.RLock()
		dev := m.device
		m.mu.RUnlock()
		log.Printf("[NPU] Following %s (%s, firmware %s, every %s)",
			dev.Path, dev.VBNV, dev.Firmware, m.interval)
	} else {
		log.Printf("[NPU] No AMD XDNA device found yet, watching for it")
	}

	m.poll()

	ticker := time.NewTicker(m.interval)
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

// poll refreshes the cached snapshot. A missing device is re-detected on every
// poll, so an NPU that is probed after the daemon started is picked up without
// a restart.
func (m *Monitor) poll() {
	m.mu.RLock()
	dev := m.device
	present := m.present
	m.mu.RUnlock()

	if !present {
		found, ok, err := detect(m.cfg.Device)
		if err != nil || !ok {
			// A not-yet-present device is not an error. A broken sysfs while
			// absent is logged and retried on the next poll.
			if err != nil {
				log.Printf("[NPU] Device detection failed: %v", err)
			}
			return
		}
		dev, present = found, true
		m.mu.Lock()
		m.device = found
		m.present = true
		m.stats.Present = true
		m.stats.Device = found.Path
		m.mu.Unlock()
		log.Printf("[NPU] Device appeared: %s (%s)", found.Path, found.VBNV)
	}

	stats, err := m.query(dev)
	if err != nil {
		found, ok, detErr := detect(m.cfg.Device)
		switch {
		case detErr != nil:
			// Sysfs is having trouble; keep the last snapshot and presence.
			m.mu.Lock()
			m.stats.LastError = detErr.Error()
			m.stats.LastUpdate = m.now()
			m.mu.Unlock()
			return
		case !ok:
			m.mu.Lock()
			m.present = false
			m.stats = Stats{LastUpdate: m.now(), LastError: err.Error()}
			m.mu.Unlock()
			log.Printf("[NPU] Device disappeared")
			return
		case found.Path != dev.Path:
			// The device was renumbered; switch to the new node.
			m.mu.Lock()
			m.device = found
			m.stats.Device = found.Path
			m.stats.LastError = err.Error()
			m.stats.LastUpdate = m.now()
			m.mu.Unlock()
			log.Printf("[NPU] Device moved to %s", found.Path)
			return
		}
		// Keep the last good snapshot, but surface the error.
		m.mu.Lock()
		m.stats.LastError = err.Error()
		m.stats.LastUpdate = m.now()
		m.mu.Unlock()
		return
	}

	summarize(&stats)
	m.mu.Lock()
	m.stats = stats
	m.stats.Present = true
	if m.stats.Device == "" {
		m.stats.Device = dev.Path
	}
	m.stats.LastUpdate = m.now()
	m.mu.Unlock()
}

// summarize derives the aggregate utilization values from the per-column
// readings, so every query source produces a consistent snapshot.
func summarize(stats *Stats) {
	if len(stats.ColumnUtilization) == 0 {
		return
	}
	var sum, max float64
	for _, value := range stats.ColumnUtilization {
		sum += value
		if value > max {
			max = value
		}
	}
	stats.ColumnUtilizationAvg = sum / float64(len(stats.ColumnUtilization))
	stats.ColumnUtilizationMax = max
}

// GetStats returns a decoupled snapshot.
func (m *Monitor) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats.Clone()
}

// Health returns a short status for /healthz.
func (m *Monitor) Health() string {
	if !m.cfg.Enabled {
		return "disabled"
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.present {
		return "absent"
	}
	if m.stats.LastError != "" {
		return "error"
	}
	if m.stats.LastUpdate.IsZero() {
		return "waiting"
	}
	return "ok"
}

// SetPowerMode switches the NPU power mode. The driver restricts this to root,
// so the daemon must run with sufficient privileges.
func (m *Monitor) SetPowerMode(mode string) error {
	if !m.cfg.Enabled {
		return errors.New("npu: monitoring is disabled")
	}
	if _, ok := powerModeValue(mode); !ok {
		return fmt.Errorf("npu: unknown power mode %q", mode)
	}

	m.mu.RLock()
	present := m.present
	path := m.device.Path
	m.mu.RUnlock()
	if !present {
		return errors.New("npu: no device present")
	}

	h, err := openHandle(path)
	if err != nil {
		return fmt.Errorf("npu: open %s: %w", path, err)
	}
	defer h.close()

	if err := h.setPowerMode(mode); err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return fmt.Errorf("npu: setting the power mode requires root: %w", err)
		}
		return fmt.Errorf("npu: set power mode: %w", err)
	}

	m.mu.Lock()
	m.stats.PowerMode = mode
	m.mu.Unlock()
	return nil
}

// queryDevice collects one runtime snapshot from the kernel driver.
func queryDevice(dev Device) (Stats, error) {
	h, err := openHandle(dev.Path)
	if err != nil {
		return Stats{}, fmt.Errorf("npu: open %s: %w", dev.Path, err)
	}
	defer h.close()

	stats := Stats{
		Present:    true,
		Device:     dev.Path,
		PCIAddress: dev.PCIAddress,
		VBNV:       dev.VBNV,
		DeviceType: dev.DeviceType,
		Firmware:   dev.Firmware,
		Revision:   dev.Revision,
		PowerState: readOrEmpty(dev.Sysfs + "/device/power_state"),
	}

	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil && !isUnsupported(err) {
			firstErr = err
		}
	}

	sensors, err := h.readSensors()
	keep(err)
	for _, sensor := range sensors {
		switch sensor.Type {
		case sensorPower:
			stats.PowerWatts += sensorWatts(sensor)
		case sensorColumnUtil:
			stats.ColumnUtilization = append(stats.ColumnUtilization, sensorPercent(sensor))
		}
	}

	clocks, err := h.readClocks()
	keep(err)
	stats.MPNPUClockMHz = clocks.MPNPU
	stats.HClockMHz = clocks.H
	resources, err := h.readResources()
	keep(err)
	stats.TOPsCurrent = resources.TOPsCurr
	stats.TOPsMax = resources.TOPsMax
	stats.TasksCurrent = resources.TaskCurr
	stats.TasksMax = resources.TaskMax
	stats.NPUClockMaxMHz = resources.NPUClkMax

	if firmware, err := h.readFirmware(); err != nil {
		keep(err)
	} else if firmware != "" {
		stats.Firmware = firmware
	}

	if mode, err := h.readPowerMode(); err != nil {
		keep(err)
	} else {
		stats.PowerMode = mode
	}

	contexts, err := h.readContexts()
	keep(err)
	stats.Contexts = contexts

	return stats, firstErr
}
