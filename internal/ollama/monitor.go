package ollama

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/power"
)

// ollamaAPIEndpoint is the Ollama /api/ps URL. It is a var so tests can point
// it at a httptest.Server (A4).
var ollamaAPIEndpoint = "http://localhost:11434/api/ps"

// GIN log regex: [GIN] 2026/01/24 - 16:57:23 | 200 | 2m36s | 100.76.21.125 | POST "/api/chat"
var ginLogRe = regexp.MustCompile(`\[GIN\] .+ \| (\d+) \|\s+([^\s]+) \|\s+([^\s]+) \| (\w+)\s+"([^"]+)"`)

type psResponse struct {
	Models []struct {
		Name     string `json:"name"`
		SizeVRAM int64  `json:"size_vram"`
	} `json:"models"`
}

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

	// lastRequest records the most recent request time ( informational only;
	// not used to drive power state since A3 ).
	lastRequest time.Time

	// httpClients is the seam used to poll Ollama's /api/ps endpoint. It is
	// injectable for tests; production uses a client with a 2s timeout (A4).
	httpClient httpClient
}

// httpClient is the minimal seam over http.Client used by the monitor.
type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type realHTTPClient struct{ c *http.Client }

func (r realHTTPClient) Do(req *http.Request) (*http.Response, error) { return r.c.Do(req) }

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
		httpClient:  realHTTPClient{c: &http.Client{Timeout: 2 * time.Second}},
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

	// Power control (A3): Ollama requests reset the idle timer so the central
	// updatePowerState in main.go is the single source of truth for the mode.
	// We deliberately do NOT call SetPerformance/SetPowersave here — that used
	// to race the idle monitor and could force powersave during active gaming.
	if m.pm != nil {
		m.pm.TriggerActivity()
	}

	log.Printf("[OllamaMonitor] %s %s from %s (%.2fs, %.6f kWh, %.4f %s)",
		info.Method, info.Endpoint, info.IP,
		info.Duration.Seconds(), energyKWh, cost, m.currency)
}

// GetStats returns the current statistics. The Ollama /api/ps poll runs OUTSIDE
// the lock with a bounded timeout (A4) so a hung Ollama cannot stall the journal
// reader or the HA polling cycle.
func (m *Monitor) GetStats() Stats {
	// 1. Poll loaded models without holding any lock.
	models, loadedVRAM := m.pollLoadedModels()

	// 2. Apply the polled values under a short-lived write lock.
	m.mu.Lock()
	m.stats.Models = models
	m.stats.LoadedVRAMBytes = loadedVRAM

	// 3. Snapshot a deep copy of the stats under the same lock.
	stats := m.stats
	stats.ByIP = make(map[string]RequestStats, len(m.stats.ByIP))
	for k, v := range m.stats.ByIP {
		stats.ByIP[k] = v
	}
	stats.ByGroup = make(map[string]RequestStats, len(m.stats.ByGroup))
	for k, v := range m.stats.ByGroup {
		stats.ByGroup[k] = v
	}
	m.mu.Unlock()

	return stats
}

// pollLoadedModels queries Ollama's /api/ps endpoint with a bounded timeout and
// returns the loaded model names and total VRAM bytes. Returns zero values on
// any error (caller treats absence gracefully). No locks taken here (A4).
func (m *Monitor) pollLoadedModels() ([]string, int64) {
	req, err := http.NewRequest(http.MethodGet, ollamaAPIEndpoint, nil)
	if err != nil {
		return nil, 0
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0
	}

	var psResp psResponse
	if err := json.NewDecoder(resp.Body).Decode(&psResp); err != nil {
		return nil, 0
	}

	models := make([]string, 0, len(psResp.Models))
	var totalVRAM int64
	for _, model := range psResp.Models {
		models = append(models, model.Name)
		totalVRAM += model.SizeVRAM
	}
	return models, totalVRAM
}
