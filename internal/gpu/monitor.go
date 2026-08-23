package gpu

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

type Monitor struct {
	mu           sync.RWMutex
	devicePath   string
	hwmonPath    string
	pollInterval time.Duration
	stats        Stats
	cpuStats     CPUStats
	lastCPUCheck time.Time
}

func NewMonitor(cfg config.GPUConfig) (*Monitor, error) {
	devicePath, err := findAMDGPUPath("/sys/class/drm")
	if err != nil {
		return nil, fmt.Errorf("failed to find AMD GPU: %w", err)
	}

	hwmonPath, err := findHwmonPath(devicePath)
	if err != nil {
		hwmonPath = ""
	}

	pollInterval := 5 * time.Second
	if cfg.PollInterval != "" {
		if d, err := time.ParseDuration(cfg.PollInterval); err == nil && d > 0 {
			pollInterval = d
		}
	}

	return &Monitor{
		devicePath:   devicePath,
		hwmonPath:    hwmonPath,
		pollInterval: pollInterval,
		stats:        Stats{},
	}, nil
}

// findAMDGPUPath scans a drm base directory for an AMD GPU (vendor 0x1002).
// Takes baseDir as a parameter so it is testable with a temp directory (T9).
func findAMDGPUPath(baseDir string) (string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", baseDir, err)
	}

	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "card") {
			continue
		}

		devicePath := filepath.Join(baseDir, e.Name(), "device")

		vendorPath := filepath.Join(devicePath, "vendor")
		vendor, err := os.ReadFile(vendorPath)
		if err != nil {
			continue
		}

		if strings.Contains(string(vendor), "0x1002") {
			return devicePath, nil
		}
	}

	return "", errors.New("AMD GPU not found in " + baseDir)
}

func findHwmonPath(devicePath string) (string, error) {
	hwmonPath := filepath.Join(devicePath, "hwmon")

	entries, err := os.ReadDir(hwmonPath)
	if err != nil {
		return "", fmt.Errorf("failed to read hwmon: %w", err)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "hwmon") {
			return filepath.Join(hwmonPath, e.Name()), nil
		}
	}

	return "", errors.New("hwmon not found")
}

func (m *Monitor) Start(ctx context.Context) error {
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	if err := m.updateStats(); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := m.updateStats(); err != nil {
				continue
			}
		}
	}
}

func (m *Monitor) updateStats() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	vramUsed := m.readSysfsFile(filepath.Join(m.devicePath, "mem_info_vram_used"))
	vramTotal := m.readSysfsFile(filepath.Join(m.devicePath, "mem_info_vram_total"))
	gttUsed := m.readSysfsFile(filepath.Join(m.devicePath, "mem_info_gtt_used"))
	gttTotal := m.readSysfsFile(filepath.Join(m.devicePath, "mem_info_gtt_total"))

	m.stats.VRAMUsedBytes = vramUsed
	m.stats.VRAMTotalBytes = vramTotal
	m.stats.GTTUsedBytes = gttUsed
	m.stats.GTTTotalBytes = gttTotal

	if m.hwmonPath != "" {
		m.stats.TemperatureC = m.readTemperature()
		m.stats.PowerWatts = m.readPower()
	}

	m.stats.CPUUsagePercent = m.calculateCPUUsageLocked()
	m.stats.LastUpdate = time.Now()

	return nil
}

func (m *Monitor) readSysfsFile(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	val, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return val
}

func (m *Monitor) readTemperature() int {
	if m.hwmonPath == "" {
		return 0
	}
	entries, err := os.ReadDir(m.hwmonPath)
	if err != nil {
		return 0
	}
	return parseTemperatureFromEntries(entries, m.hwmonPath)
}

// parseTemperatureFromEntries scans hwmon dir entries for a temp*_input file,
// reads it, and returns the value in degrees Celsius (divided by 1000).
// Extracted from readTemperature so it is testable with fixture data (T9).
func parseTemperatureFromEntries(entries []os.DirEntry, hwmonPath string) int {
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_input") && strings.HasPrefix(e.Name(), "temp") {
			data, err := os.ReadFile(filepath.Join(hwmonPath, e.Name()))
			if err != nil {
				continue
			}
			val, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			return int(val / 1000)
		}
	}
	return 0
}

func (m *Monitor) readPower() float64 {
	if m.hwmonPath == "" {
		return 0
	}
	entries, err := os.ReadDir(m.hwmonPath)
	if err != nil {
		return 0
	}
	return parsePowerFromEntries(entries, m.hwmonPath)
}

// parsePowerFromEntries scans hwmon dir entries for a power*_average file,
// reads it, and returns the value in Watts (divided by 1,000,000).
// Extracted from readPower so it is testable with fixture data (T9).
func parsePowerFromEntries(entries []os.DirEntry, hwmonPath string) float64 {
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_average") && strings.HasPrefix(e.Name(), "power") {
			data, err := os.ReadFile(filepath.Join(hwmonPath, e.Name()))
			if err != nil {
				continue
			}
			val, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			return float64(val) / 1000000.0
		}
	}
	return 0
}

func (m *Monitor) calculateCPUUsageLocked() float64 {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0
	}

	line := scanner.Text()
	return m.calculateCPUUsageFromLine(line)
}

// calculateCPUUsageFromLine parses a single /proc/stat "cpu " line, updates the
// monitor's CPU stats, and returns the usage percentage since the last call.
// Extracted from calculateCPUUsageLocked so it is testable with fixture data (T9).
func (m *Monitor) calculateCPUUsageFromLine(line string) float64 {
	if !strings.HasPrefix(line, "cpu ") {
		return 0
	}

	fields := strings.Fields(line)[1:]
	if len(fields) < 4 {
		return 0
	}

	var stats CPUStats
	for i, f := range fields {
		val, _ := strconv.ParseUint(f, 10, 64)
		switch i {
		case 0:
			stats.User = val
		case 1:
			stats.Nice = val
		case 2:
			stats.System = val
		case 3:
			stats.Idle = val
		case 4:
			stats.IOWait = val
		case 5:
			stats.IRQ = val
		case 6:
			stats.SoftIRQ = val
		case 7:
			stats.Steal = val
		}
	}

	stats.Total = stats.User + stats.Nice + stats.System + stats.Idle +
		stats.IOWait + stats.IRQ + stats.SoftIRQ + stats.Steal

	if m.lastCPUCheck.IsZero() {
		m.cpuStats = stats
		m.lastCPUCheck = time.Now()
		return 0
	}

	totalDelta := stats.Total - m.cpuStats.Total
	idleDelta := stats.Idle - m.cpuStats.Idle

	m.cpuStats = stats
	m.lastCPUCheck = time.Now()

	if totalDelta == 0 {
		return 0
	}

	return float64(totalDelta-idleDelta) / float64(totalDelta) * 100.0
}

func (m *Monitor) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.stats
}

func (m *Monitor) GetDevicePath() string {
	return m.devicePath
}

func (m *Monitor) GetHwmonPath() string {
	return m.hwmonPath
}
