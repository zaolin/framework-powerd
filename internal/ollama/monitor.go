package ollama

import (
	"context"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/power"
)

// GIN log regex: [GIN] 2026/01/24 - 16:57:23 | 200 | 2m36s | 100.76.21.125 | POST "/api/chat"
var ginLogRe = regexp.MustCompile(`\[GIN\] .+ \| (\d+) \|\s+([^\s]+) \|\s+([^\s]+) \| (\w+)\s+"([^"]+)"`)

// Monitor watches Ollama logs and tracks usage statistics
type Monitor struct {
	mu          sync.RWMutex
	serviceUnit string
	groups      []config.IPGroup
	pricePerKWh float64
	currency    string
	stats       Stats

	pm       *power.PowerManager
	powerMon *power.PowerMonitor

	// Power management
	powersaveTimer *time.Timer
	lastRequest    time.Time
}

// NewMonitor creates a new Ollama monitor
func NewMonitor(pm *power.PowerManager, powerMon *power.PowerMonitor, cfg config.OllamaConfig, pricing config.PricingConfig) *Monitor {
	return &Monitor{
		serviceUnit: cfg.ServiceUnit,
		groups:      cfg.Groups,
		pricePerKWh: pricing.EnergyPricePerKWh,
		currency:    pricing.Currency,
		stats:       NewStats(pricing.EnergyPricePerKWh, pricing.Currency),
		pm:          pm,
		powerMon:    powerMon,
	}
}

// Start begins watching the journal for Ollama logs
func (m *Monitor) Start(ctx context.Context) error {
	journal, err := sdjournal.NewJournal()
	if err != nil {
		return err
	}
	defer journal.Close()

	// Filter to ollama service
	match := "_SYSTEMD_UNIT=" + m.serviceUnit
	if err := journal.AddMatch(match); err != nil {
		return err
	}

	// Seek to end (only watch new entries)
	if err := journal.SeekTail(); err != nil {
		return err
	}
	// Move back one so we start reading new entries
	journal.Previous()

	log.Printf("[OllamaMonitor] Watching %s logs...", m.serviceUnit)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Wait for new entries (up to 1 second timeout to check context)
		r := journal.Wait(time.Second)
		if r < 0 {
			continue
		}

		// Read all available entries
		for {
			n, err := journal.Next()
			if err != nil {
				log.Printf("[OllamaMonitor] Error reading journal: %v", err)
				break
			}
			if n == 0 {
				break
			}

			msg, err := journal.GetDataValue("MESSAGE")
			if err != nil {
				continue
			}

			if info := m.parseGINLog(msg); info != nil {
				m.recordRequest(info)
			}
		}
	}
}

// parseGINLog extracts request info from a GIN log line
func (m *Monitor) parseGINLog(msg string) *RequestInfo {
	matches := ginLogRe.FindStringSubmatch(msg)
	if matches == nil {
		return nil
	}

	status, _ := strconv.Atoi(matches[1])
	duration := parseDuration(matches[2])
	ip := matches[3]
	method := matches[4]
	endpoint := matches[5]

	return &RequestInfo{
		Timestamp: time.Now(),
		IP:        ip,
		Method:    method,
		Endpoint:  endpoint,
		Status:    status,
		Duration:  duration,
	}
}

// parseDuration handles various duration formats from GIN
func parseDuration(s string) time.Duration {
	// Try standard Go duration first
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}

	// Handle formats like "2m36s", "10.105733868s", "33.663µs"
	s = strings.TrimSpace(s)
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}

	return 0
}

// recordRequest updates statistics with a new request
func (m *Monitor) recordRequest(info *RequestInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Calculate energy: current watts × duration (hours)
	var avgWatts float64
	if m.powerMon != nil {
		ps := m.powerMon.GetStatus()
		avgWatts = ps.Current.PkgWatt + ps.Current.CorWatt + ps.Current.RAMWatt
	}

	durationHours := info.Duration.Hours()
	energyKWh := (avgWatts * durationHours) / 1000.0
	cost := energyKWh * m.pricePerKWh

	// Update by IP
	ipStats := m.stats.ByIP[info.IP]
	if ipStats.Endpoints == nil {
		ipStats = NewRequestStats()
	}
	ipStats.Add(info, energyKWh, cost)
	m.stats.ByIP[info.IP] = ipStats

	// Update by group
	groupName := MatchGroup(info.IP, m.groups)
	if groupName != "" {
		groupStats := m.stats.ByGroup[groupName]
		if groupStats.Endpoints == nil {
			groupStats = NewRequestStats()
		}
		groupStats.Add(info, energyKWh, cost)
		m.stats.ByGroup[groupName] = groupStats
	} else {
		m.stats.Ungrouped.Add(info, energyKWh, cost)
	}

	// Update last request time
	m.lastRequest = time.Now()

	// Set performance mode for this request
	if m.pm != nil {
		m.pm.SetPerformance("Ollama Request")
		m.pm.TriggerActivity()

		// Cancel any existing powersave timer
		if m.powersaveTimer != nil {
			m.powersaveTimer.Stop()
		}

		// Schedule powersave after 10 seconds of inactivity
		m.powersaveTimer = time.AfterFunc(10*time.Second, func() {
			m.mu.RLock()
			lastReq := m.lastRequest
			m.mu.RUnlock()

			// Only switch to powersave if no new request came in
			if time.Since(lastReq) >= 10*time.Second {
				log.Println("[OllamaMonitor] No requests for 10s, switching to powersave")
				m.pm.SetPowersave("Ollama Idle")
			}
		})
	}

	log.Printf("[OllamaMonitor] %s %s from %s (%.2fs, %.6f kWh, %.4f %s)",
		info.Method, info.Endpoint, info.IP,
		info.Duration.Seconds(), energyKWh, cost, m.currency)
}

// GetStats returns the current statistics
func (m *Monitor) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy
	stats := m.stats
	stats.ByIP = make(map[string]RequestStats)
	for k, v := range m.stats.ByIP {
		stats.ByIP[k] = v
	}
	stats.ByGroup = make(map[string]RequestStats)
	for k, v := range m.stats.ByGroup {
		stats.ByGroup[k] = v
	}
	return stats
}
