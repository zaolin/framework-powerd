package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/persist"
)

const llmStateVersion = 1

// Monitor tails the proxy access log and aggregates LLM requests per client IP
// and per group.
type Monitor struct {
	mu sync.RWMutex

	cfg          config.LLMConfig
	groups       []config.IPGroup
	countedPaths map[string]bool
	pricePerKWh  float64
	historyDays  int
	statePath    string
	saveInterval time.Duration

	stats  Stats
	days   map[string]*DayStats
	tailer *tailer

	// logInode and logOffset mirror the tailer position so it can be persisted
	// together with the counters. They are only written by the poll loop and
	// guarded by mu for readers.
	logInode  uint64
	logOffset int64

	// powerProvider returns the current total system power in watts. It is
	// used to attribute energy to requests.
	powerProvider func() float64

	now func() time.Time
}

// NewMonitor creates an LLM request accounting monitor.
func NewMonitor(cfg config.LLMConfig, pricing config.PricingConfig) *Monitor {
	counted := make(map[string]bool, len(cfg.CountedPaths))
	for _, path := range cfg.CountedPaths {
		counted[path] = true
	}

	m := &Monitor{
		cfg:          cfg,
		groups:       cfg.Groups,
		countedPaths: counted,
		pricePerKWh:  pricing.EnergyPricePerKWh,
		historyDays:  cfg.GetHistoryDays(),
		statePath:    cfg.StateFile,
		saveInterval: cfg.GetSaveInterval(),
		days:         make(map[string]*DayStats),
		now:          time.Now,
	}
	m.stats = NewStats(pricing.EnergyPricePerKWh, strings.ToUpper(strings.TrimSpace(pricing.Currency)))
	m.stats.Enabled = cfg.Enabled
	m.tailer = newTailer(cfg.AccessLog)
	return m
}

// SetPowerProvider registers a callback that returns the current total system
// power draw in watts. Energy attribution is skipped when it is not set.
func (m *Monitor) SetPowerProvider(fn func() float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.powerProvider = fn
}

// Start follows the access log until ctx is cancelled.
func (m *Monitor) Start(ctx context.Context) {
	if !m.cfg.Enabled {
		return
	}

	log.Printf("[LLM] Following %s (groups: %d, retention: %d days)",
		m.cfg.AccessLog, len(m.groups), m.historyDays)

	ticker := time.NewTicker(2 * time.Second)
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

// poll reads new log entries and records them. The whole batch is applied
// under one lock acquisition together with the new log position, so a
// concurrent save can never persist a position that covers requests whose
// counters were not written yet.
func (m *Monitor) poll() {
	entries, err := m.tailer.read()
	inode, offset := m.tailer.position()

	prepared := make([]preparedRequest, 0, len(entries))
	for _, entry := range entries {
		if p, ok := m.prepare(entry); ok {
			prepared = append(prepared, p)
		}
	}

	m.mu.Lock()
	for _, p := range prepared {
		m.applyLocked(p)
	}
	// Prune here as well: a deployment without a state file and without API
	// consumers would otherwise never take a snapshot.
	m.pruneLocked(m.now())
	m.logInode = inode
	m.logOffset = offset
	if err != nil {
		m.stats.LastError = err.Error()
	} else {
		m.stats.LastError = ""
	}
	m.mu.Unlock()
}

// preparedRequest is a log entry that passed the filters, with the derived
// accounting values computed before taking the lock.
type preparedRequest struct {
	uri      string
	ip       string
	group    string
	at       time.Time
	duration time.Duration
	energy   float64
	cost     float64
}

// prepare validates an access log entry and computes its accounting values.
func (m *Monitor) prepare(entry accessEntry) (preparedRequest, bool) {
	uri := entry.Request.URI
	// Strip the query string: only the path identifies the endpoint.
	if idx := strings.IndexByte(uri, '?'); idx >= 0 {
		uri = uri[:idx]
	}
	if !m.countedPaths[uri] {
		return preparedRequest{}, false
	}

	ip := entry.Request.RemoteIP
	if IsExcluded(ip, m.cfg.ExcludeIPs, m.cfg.IgnoreLocalIP) {
		return preparedRequest{}, false
	}

	at := m.now()
	if entry.TS > 0 {
		seconds := int64(entry.TS)
		nanos := int64((entry.TS - float64(seconds)) * 1e9)
		at = time.Unix(seconds, nanos).UTC()
	}
	duration := time.Duration(entry.Duration * float64(time.Second))

	m.mu.RLock()
	provider := m.powerProvider
	pricePerKWh := m.pricePerKWh
	m.mu.RUnlock()

	energyKWh := 0.0
	if provider != nil {
		if watts := provider(); watts > 0 {
			energyKWh = watts * duration.Hours() / 1000
		}
	}

	return preparedRequest{
		uri:      uri,
		ip:       ip,
		group:    MatchGroup(ip, m.groups),
		at:       at,
		duration: duration,
		energy:   energyKWh,
		cost:     energyKWh * pricePerKWh,
	}, true
}

// applyLocked adds a prepared request to the aggregates.
//
// m.mu must be held.
func (m *Monitor) applyLocked(p preparedRequest) {
	m.stats.Total.Add(p.uri, p.duration, p.energy, p.cost, p.at)

	ipStats, ok := m.stats.ByIP[p.ip]
	if !ok {
		ipStats = newRequestStats()
	}
	ipStats.Add(p.uri, p.duration, p.energy, p.cost, p.at)
	m.stats.ByIP[p.ip] = ipStats

	if p.group != "" {
		groupStats, ok := m.stats.ByGroup[p.group]
		if !ok {
			groupStats = newRequestStats()
		}
		groupStats.Add(p.uri, p.duration, p.energy, p.cost, p.at)
		m.stats.ByGroup[p.group] = groupStats
	} else {
		m.stats.Ungrouped.Add(p.uri, p.duration, p.energy, p.cost, p.at)
	}

	if p.at.After(m.stats.LastRequest) {
		m.stats.LastRequest = p.at
	}

	day := m.dayLocked(p.at)
	day.Total++
	day.TotalEnergy += p.energy
	day.TotalCost += p.cost
	day.ByIP[p.ip]++
	if p.group != "" {
		day.ByGroup[p.group]++
	}
}

// record accounts a single access log entry.
func (m *Monitor) record(entry accessEntry) {
	p, ok := m.prepare(entry)
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applyLocked(p)
}

// dayLocked returns the day bucket for the given time, creating it when
// needed. It also drops buckets that fell out of the retention window.
//
// m.mu must be held.
func (m *Monitor) dayLocked(at time.Time) *DayStats {
	key := dayKey(at)
	day, ok := m.days[key]
	if !ok {
		created := newDayStats(key)
		day = &created
		m.days[key] = day
	}
	m.pruneDaysLocked(m.now())
	return day
}

// retentionCutoff returns the first UTC day key that is still retained.
//
// m.mu must be held.
func (m *Monitor) retentionCutoff(now time.Time) string {
	return now.UTC().AddDate(0, 0, -(m.historyDays - 1)).Format("2006-01-02")
}

// pruneDaysLocked removes day buckets outside the retention window. The window
// counts days including the current one, so history_days=30 keeps today and
// the 29 days before it.
//
// m.mu must be held.
func (m *Monitor) pruneDaysLocked(now time.Time) {
	cutoff := m.retentionCutoff(now)
	for key := range m.days {
		if key < cutoff {
			delete(m.days, key)
		}
	}
}

// pruneIPsLocked drops per-IP counters for addresses whose last counted request
// falls outside the retention window. The comparison uses day keys, so an
// address that was active on the first retained day is kept regardless of the
// time of day. Lifetime aggregates stay in Total and ByGroup.
//
// m.mu must be held.
func (m *Monitor) pruneIPsLocked(now time.Time) {
	cutoff := m.retentionCutoff(now)
	for ip, stats := range m.stats.ByIP {
		if !stats.LastRequest.IsZero() && dayKey(stats.LastRequest) < cutoff {
			delete(m.stats.ByIP, ip)
		}
	}
}

// pruneLocked applies both retention rules.
//
// m.mu must be held.
func (m *Monitor) pruneLocked(now time.Time) {
	m.pruneDaysLocked(now)
	m.pruneIPsLocked(now)
}

// copyRequestStats deep-copies a RequestStats, including its endpoint map.
func copyRequestStats(s RequestStats) RequestStats {
	out := s
	out.Endpoints = make(map[string]int, len(s.Endpoints))
	for k, v := range s.Endpoints {
		out.Endpoints[k] = v
	}
	return out
}

// copyDayStats deep-copies a DayStats, including its maps.
func copyDayStats(d DayStats) DayStats {
	out := d
	out.ByIP = make(map[string]int, len(d.ByIP))
	for k, v := range d.ByIP {
		out.ByIP[k] = v
	}
	out.ByGroup = make(map[string]int, len(d.ByGroup))
	for k, v := range d.ByGroup {
		out.ByGroup[k] = v
	}
	return out
}

// snapshotLocked builds a fully decoupled copy of the accounting state. The
// caller may marshal or hand it out without holding the lock.
//
// m.mu must be held.
func (m *Monitor) snapshotLocked(now time.Time) Stats {
	m.pruneLocked(now)

	stats := m.stats
	stats.Total = copyRequestStats(m.stats.Total)
	stats.Ungrouped = copyRequestStats(m.stats.Ungrouped)

	stats.ByIP = make(map[string]RequestStats, len(m.stats.ByIP))
	for k, v := range m.stats.ByIP {
		stats.ByIP[k] = copyRequestStats(v)
	}
	stats.ByGroup = make(map[string]RequestStats, len(m.stats.ByGroup))
	for k, v := range m.stats.ByGroup {
		stats.ByGroup[k] = copyRequestStats(v)
	}

	keys := make([]string, 0, len(m.days))
	for key := range m.days {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	stats.Days = make([]DayStats, 0, len(keys))
	for _, key := range keys {
		stats.Days = append(stats.Days, copyDayStats(*m.days[key]))
	}

	todayKey := dayKey(now)
	if day, ok := m.days[todayKey]; ok {
		stats.Today = copyDayStats(*day)
	} else {
		stats.Today = newDayStats(todayKey)
	}
	return stats
}

// GetStats returns a decoupled snapshot of the accounting state.
func (m *Monitor) GetStats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked(m.now())
}

// llmState is the persisted form of the accounting state.
type llmState struct {
	Version   int                     `json:"version"`
	SavedAt   time.Time               `json:"saved_at"`
	Total     RequestStats            `json:"total"`
	ByIP      map[string]RequestStats `json:"by_ip"`
	ByGroup   map[string]RequestStats `json:"by_group"`
	Ungrouped RequestStats            `json:"ungrouped"`
	Days      map[string]DayStats     `json:"days"`
	Since     time.Time               `json:"since"`
	LogInode  uint64                  `json:"log_inode"`
	LogOffset int64                   `json:"log_offset"`
}

// LoadState restores persisted accounting state.
func (m *Monitor) LoadState() (bool, error) {
	if m.statePath == "" {
		return false, nil
	}

	var state llmState
	if err := persist.LoadJSON(m.statePath, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if state.Version != llmStateVersion {
		return false, fmt.Errorf("unsupported llm state version %d", state.Version)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state.Total = normalizeRequestStats(state.Total)
	state.Ungrouped = normalizeRequestStats(state.Ungrouped)

	m.stats.Total = state.Total
	m.stats.Ungrouped = state.Ungrouped
	if !state.Since.IsZero() {
		m.stats.Since = state.Since
	}
	if !state.Total.LastRequest.IsZero() {
		m.stats.LastRequest = state.Total.LastRequest
	}

	m.stats.ByIP = make(map[string]RequestStats, len(state.ByIP))
	for ip, stats := range state.ByIP {
		m.stats.ByIP[ip] = normalizeRequestStats(stats)
	}
	m.stats.ByGroup = make(map[string]RequestStats, len(state.ByGroup))
	for group, stats := range state.ByGroup {
		m.stats.ByGroup[group] = normalizeRequestStats(stats)
	}

	m.days = make(map[string]*DayStats, len(state.Days))
	for key, day := range state.Days {
		copied := day
		if copied.ByIP == nil {
			copied.ByIP = make(map[string]int)
		}
		if copied.ByGroup == nil {
			copied.ByGroup = make(map[string]int)
		}
		m.days[key] = &copied
	}

	m.logInode = state.LogInode
	m.logOffset = state.LogOffset
	m.tailer.resume(state.LogInode, state.LogOffset)

	log.Printf("[LLM] Restored accounting state: %d requests, %d IPs, %d days",
		m.stats.Total.Count, len(m.stats.ByIP), len(m.days))
	return true, nil
}

// normalizeRequestStats makes sure a decoded RequestStats has a usable
// endpoint map.
func normalizeRequestStats(s RequestStats) RequestStats {
	if s.Endpoints == nil {
		s.Endpoints = make(map[string]int)
	}
	return s
}

// SaveState writes the accounting state atomically.
func (m *Monitor) SaveState() error {
	if m.statePath == "" {
		return nil
	}

	m.mu.Lock()
	now := m.now()
	stats := m.snapshotLocked(now)
	state := llmState{
		Version:   llmStateVersion,
		SavedAt:   now.UTC(),
		Total:     stats.Total,
		ByIP:      stats.ByIP,
		ByGroup:   stats.ByGroup,
		Ungrouped: stats.Ungrouped,
		Days:      make(map[string]DayStats, len(m.days)),
		Since:     stats.Since,
		LogInode:  m.logInode,
		LogOffset: m.logOffset,
	}
	for key, day := range m.days {
		state.Days[key] = copyDayStats(*day)
	}
	m.mu.Unlock()

	return persist.SaveJSON(m.statePath, state)
}

// StartPersistence runs the periodic save loop until ctx is cancelled. It
// blocks; the caller decides when the final save happens after the loop exits.
func (m *Monitor) StartPersistence(ctx context.Context) {
	if m.statePath == "" {
		return
	}
	interval := m.saveInterval
	if interval < time.Second {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.SaveState(); err != nil {
				log.Printf("[LLM] Failed to save state: %v", err)
			}
		}
	}
}

// Health returns a short status string for /healthz.
func (m *Monitor) Health() string {
	if !m.cfg.Enabled {
		return "disabled"
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.stats.LastError != "" {
		return "error"
	}
	if m.stats.LastRequest.IsZero() {
		return "waiting"
	}
	return "ok"
}
