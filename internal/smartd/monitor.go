package smartd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/zaolin/framework-powerd/internal/config"
	"github.com/zaolin/framework-powerd/internal/persist"
)

const alertsStateVersion = 1

var (
	deviceMessageRe = regexp.MustCompile(`^Device:\s*([^,]+),\s*(.+)$`)
	attributeRe     = regexp.MustCompile(`(?i)smart (?:usage )?attribute:\s*(.+?)\s+changed from`)
	intRe           = regexp.MustCompile(`-?\d+`)
	hexRe           = regexp.MustCompile(`(?i)0x[0-9a-f]+`)
)

// Monitor watches smartd journal messages and keeps sticky, persisted alerts.
type Monitor struct {
	mu             sync.RWMutex
	serviceUnit    string
	notifyService  string
	alertRetention time.Duration
	statePath      string
	alerts         map[string]Alert
	lastCheck      time.Time
	saveCh         chan struct{}
	saveMu         sync.Mutex
	now            func() time.Time
}

// NewMonitor creates a new SMART monitor.
func NewMonitor(cfg config.SmartdConfig) *Monitor {
	return &Monitor{
		serviceUnit:    cfg.ServiceUnit,
		notifyService:  cfg.NotifyService,
		alertRetention: cfg.GetAlertRetention(),
		statePath:      cfg.StateFile,
		alerts:         make(map[string]Alert),
		saveCh:         make(chan struct{}, 1),
		now:            time.Now,
	}
}

func (m *Monitor) nowTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// Start follows the configured smartd unit until ctx ends. Journal failures
// are retried with a short backoff instead of disabling monitoring forever.
func (m *Monitor) Start(ctx context.Context) error {
	if m.statePath != "" {
		go m.saveLoop(ctx)
	}

	backoff := 5 * time.Second
	for {
		err := m.runJournal(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("[SmartdMonitor] Journal reader stopped: %v; retrying in %s", err, backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}

func (m *Monitor) runJournal(ctx context.Context) error {
	journal, err := sdjournal.NewJournal()
	if err != nil {
		return err
	}
	defer journal.Close()

	match := "_SYSTEMD_UNIT=" + m.serviceUnit
	if err := journal.AddMatch(match); err != nil {
		return err
	}
	if err := journal.SeekTail(); err != nil {
		return err
	}
	journal.Previous()

	log.Printf("[SmartdMonitor] Watching %s logs (alert TTL %s)...", m.serviceUnit, m.alertRetention)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		r := journal.Wait(time.Second)
		if r < 0 {
			continue
		}

		for {
			n, err := journal.Next()
			if err != nil {
				return err
			}
			if n == 0 {
				break
			}

			msg, err := journal.GetDataValue("MESSAGE")
			if err != nil {
				continue
			}
			m.parseAlert(msg)
		}

		m.setLastCheck()
		m.cleanupExpiredAlerts()
	}
}

func (m *Monitor) setLastCheck() {
	now := m.nowTime()
	m.mu.Lock()
	m.lastCheck = now
	m.mu.Unlock()
}

// parseAlert classifies a journal line and records a sticky alert.
func (m *Monitor) parseAlert(msg string) {
	matches := deviceMessageRe.FindStringSubmatch(strings.TrimSpace(msg))
	if matches == nil {
		return
	}
	device := strings.TrimSpace(matches[1])
	text := strings.TrimSpace(matches[2])

	failType, severity, message, ok := classify(text)
	if !ok {
		return
	}
	m.recordAlert(device, failType, severity, message, msg)
}

func classify(text string) (failType, severity, message string, ok bool) {
	lower := strings.ToLower(text)

	switch {
	case strings.Contains(lower, "failed smart self-test") ||
		strings.Contains(lower, "self-test failed"):
		return "selftest", "critical", text, true

	case strings.Contains(lower, "failed smart health check") ||
		strings.Contains(lower, "overall-health self-assessment test result: failed"):
		return "health", "critical", text, true

	case strings.Contains(lower, "failed smart attribute") ||
		strings.Contains(lower, "failed smart usage attribute"):
		return "attribute", "critical", text, true

	case strings.Contains(lower, "critical warning"):
		if isZeroHex(text) {
			return "", "", "", false
		}
		return "critical_warning", "critical", text, true

	case strings.Contains(lower, "media and data integrity errors"):
		if n, ok := lastInt(text); !ok || n == 0 {
			return "", "", "", false
		} else if n >= 10 {
			return "media_errors", "critical", text, true
		}
		return "media_errors", "warning", text, true

	case strings.Contains(lower, "available spare"):
		if pct, ok := lastInt(text); ok {
			if pct <= 10 {
				return "available_spare", "critical", text, true
			}
			if pct < 100 {
				return "available_spare", "warning", text, true
			}
			return "", "", "", false
		}
		return "available_spare", "warning", text, true

	case strings.Contains(lower, "percentage used"):
		if pct, ok := lastInt(text); ok {
			if pct >= 90 {
				return "percentage_used", "critical", text, true
			}
			if pct >= 80 {
				return "percentage_used", "warning", text, true
			}
			return "", "", "", false
		}
		return "percentage_used", "warning", text, true

	case strings.Contains(lower, "error information log entries") ||
		strings.Contains(lower, "error log entries"):
		if n, ok := lastInt(text); !ok || n == 0 {
			return "", "", "", false
		} else if n >= 10 {
			return "error_log", "critical", text, true
		}
		return "error_log", "warning", text, true

	case strings.Contains(lower, "currently unreadable") ||
		strings.Contains(lower, "offline uncorrectable") ||
		strings.Contains(lower, "pending sector"):
		return "sector_errors", "critical", text, true

	case strings.Contains(lower, "reallocated_sector") ||
		strings.Contains(lower, "reallocated sector"):
		return "reallocated_sectors", "warning", text, true

	case strings.Contains(lower, "ata error count") ||
		strings.Contains(lower, "error count increased"):
		return "io_errors", "warning", text, true

	case strings.Contains(lower, "temperature") &&
		(strings.Contains(lower, "critical") || strings.Contains(lower, "too hot")):
		return "temperature", "critical", text, true

	case strings.Contains(lower, "temperature") && strings.Contains(lower, "warning"):
		return "temperature", "warning", text, true
	}

	// Risk-relevant attribute changes are worth keeping. Benign changes
	// (temperature history, load cycle counts, etc.) are ignored.
	if matches := attributeRe.FindStringSubmatch(text); matches != nil {
		attr := strings.ToLower(matches[1])
		for _, risk := range []string{"reallocated", "pending", "uncorrectable", "wear", "spare", "percentage", "critical", "media", "error"} {
			if strings.Contains(attr, risk) {
				return "attribute_change", "warning", text, true
			}
		}
		return "", "", "", false
	}

	// Benign "everything is fine" messages must not become generic alerts.
	if strings.Contains(lower, "no errors logged") ||
		strings.Contains(lower, "no error") ||
		strings.Contains(lower, "0 errors") {
		return "", "", "", false
	}

	if strings.Contains(lower, "failed") || strings.Contains(lower, "failure") ||
		strings.Contains(lower, "error") || strings.Contains(lower, "degraded") {
		return "event", "warning", text, true
	}

	return "", "", "", false
}

func isZeroHex(text string) bool {
	if match := hexRe.FindString(text); match != "" {
		if v, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(match), "0x"), 16, 64); err == nil {
			return v == 0
		}
	}
	return false
}

func lastInt(text string) (int, bool) {
	matches := intRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return 0, false
	}
	v, err := strconv.Atoi(matches[len(matches)-1])
	if err != nil {
		return 0, false
	}
	return v, true
}

func (m *Monitor) recordAlert(device, failType, severity, message, raw string) {
	now := m.nowTime()
	key := device + "|" + failType

	m.mu.Lock()
	if existing, ok := m.alerts[key]; ok {
		existing.LastSeen = now
		existing.Timestamp = now
		existing.Count++
		existing.Message = message
		existing.Raw = raw
		if severityRank(severity) > severityRank(existing.Severity) {
			existing.Severity = severity
		}
		m.alerts[key] = existing
	} else {
		m.alerts[key] = Alert{
			ID:        key,
			Device:    device,
			FailType:  failType,
			Severity:  severity,
			Message:   message,
			Raw:       raw,
			FirstSeen: now,
			LastSeen:  now,
			Count:     1,
			Timestamp: now,
		}
	}
	m.mu.Unlock()

	m.requestSave()
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func (m *Monitor) cleanupExpiredAlerts() {
	now := m.nowTime()
	m.mu.Lock()
	changed := false
	for key, alert := range m.alerts {
		if now.Sub(alert.LastSeen) > m.alertRetention {
			delete(m.alerts, key)
			changed = true
			log.Printf("[SmartdMonitor] Alert expired for %s: %s", alert.Device, alert.Message)
		}
	}
	m.mu.Unlock()
	if changed {
		m.requestSave()
	}
}

// GetStats returns a sorted snapshot of all active alerts.
func (m *Monitor) GetStats() Stats {
	m.mu.RLock()
	alerts := make([]Alert, 0, len(m.alerts))
	for _, alert := range m.alerts {
		alerts = append(alerts, alert)
	}
	lastCheck := m.lastCheck
	m.mu.RUnlock()

	sort.Slice(alerts, func(i, j int) bool {
		if severityRank(alerts[i].Severity) != severityRank(alerts[j].Severity) {
			return severityRank(alerts[i].Severity) > severityRank(alerts[j].Severity)
		}
		if alerts[i].Device != alerts[j].Device {
			return alerts[i].Device < alerts[j].Device
		}
		return alerts[i].FailType < alerts[j].FailType
	})

	lastCheckText := ""
	if !lastCheck.IsZero() {
		lastCheckText = lastCheck.UTC().Format(time.RFC3339)
	}

	return Stats{
		Alerts:        alerts,
		LastCheck:     lastCheckText,
		NotifyService: m.notifyService,
	}
}

func (m *Monitor) GetNotifyService() string {
	return m.notifyService
}

// LoadState restores persisted alerts. Missing files are a normal first start.
func (m *Monitor) LoadState() (bool, error) {
	if m.statePath == "" {
		return false, nil
	}

	var state alertsState
	if err := persist.LoadJSON(m.statePath, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if state.Version != alertsStateVersion {
		return false, fmt.Errorf("unsupported alerts state version %d", state.Version)
	}

	now := m.nowTime()
	restored := 0
	m.mu.Lock()
	for _, alert := range state.Alerts {
		if alert.LastSeen.IsZero() {
			alert.LastSeen = alert.Timestamp
		}
		if alert.FirstSeen.IsZero() {
			alert.FirstSeen = alert.LastSeen
		}
		if alert.LastSeen.IsZero() || now.Sub(alert.LastSeen) > m.alertRetention {
			continue
		}
		if alert.ID == "" {
			alert.ID = alert.Device + "|" + alert.FailType
		}
		if alert.Timestamp.IsZero() {
			alert.Timestamp = alert.LastSeen
		}
		m.alerts[alert.ID] = alert
		restored++
	}
	m.mu.Unlock()

	if restored > 0 {
		log.Printf("[SmartdMonitor] Restored %d persisted alert(s)", restored)
	}
	return restored > 0, nil
}

// SaveState writes the current alert set atomically.
func (m *Monitor) SaveState() error {
	if m.statePath == "" {
		return nil
	}

	m.mu.RLock()
	alerts := make([]Alert, 0, len(m.alerts))
	for _, alert := range m.alerts {
		alerts = append(alerts, alert)
	}
	m.mu.RUnlock()

	sort.Slice(alerts, func(i, j int) bool { return alerts[i].ID < alerts[j].ID })

	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	return persist.SaveJSON(m.statePath, alertsState{
		Version: alertsStateVersion,
		SavedAt: m.nowTime().UTC(),
		Alerts:  alerts,
	})
}

func (m *Monitor) requestSave() {
	if m.saveCh == nil || m.statePath == "" {
		return
	}
	select {
	case m.saveCh <- struct{}{}:
	default:
	}
}

func (m *Monitor) saveLoop(ctx context.Context) {
	interval := 5 * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := m.SaveState(); err != nil {
				log.Printf("[SmartdMonitor] Failed to save alerts on shutdown: %v", err)
			}
			return
		case <-ticker.C:
			if err := m.SaveState(); err != nil {
				log.Printf("[SmartdMonitor] Failed to save alerts: %v", err)
			}
		case <-m.saveCh:
			if err := m.SaveState(); err != nil {
				log.Printf("[SmartdMonitor] Failed to save alerts: %v", err)
			}
		}
	}
}
