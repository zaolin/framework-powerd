package smartd

import (
	"context"
	"log"
	"regexp"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/zaolin/framework-powerd/internal/config"
)

var (
	selfTestFailRe = regexp.MustCompile(`Device: (.+), FAILED SMART self-test`)
	healthFailRe   = regexp.MustCompile(`Device: (.+), Failed SMART health check`)
	attrFailRe     = regexp.MustCompile(`Device: (.+), Failed SMART Attribute: (.+)`)
)

type Monitor struct {
	mu             sync.RWMutex
	serviceUnit    string
	notifyService  string
	alertRetention time.Duration
	alerts         map[string]Alert
	stats          Stats
}

func NewMonitor(cfg config.SmartdConfig) *Monitor {
	retention := 30 * time.Second
	if cfg.AlertRetention != "" {
		if d, err := time.ParseDuration(cfg.AlertRetention); err == nil && d > 0 {
			retention = d
		}
	}

	return &Monitor{
		serviceUnit:    cfg.ServiceUnit,
		notifyService:  cfg.NotifyService,
		alertRetention: retention,
		alerts:         make(map[string]Alert),
		stats:          Stats{},
	}
}

func (m *Monitor) Start(ctx context.Context) error {
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

	log.Printf("[SmartdMonitor] Watching %s logs...", m.serviceUnit)

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
				log.Printf("[SmartdMonitor] Error reading journal: %v", err)
				break
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

		m.cleanupResolvedAlerts()
	}
}

func (m *Monitor) parseAlert(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if matches := selfTestFailRe.FindStringSubmatch(msg); matches != nil {
		device := matches[1]
		m.alerts[device] = Alert{
			Device:    device,
			FailType:  "SelfTest",
			Message:   "FAILED SMART self-test",
			Timestamp: time.Now(),
		}
		log.Printf("[SmartdMonitor] Self-test failure detected on %s", device)
		return
	}

	if matches := healthFailRe.FindStringSubmatch(msg); matches != nil {
		device := matches[1]
		m.alerts[device] = Alert{
			Device:    device,
			FailType:  "Health",
			Message:   "Failed SMART health check",
			Timestamp: time.Now(),
		}
		log.Printf("[SmartdMonitor] Health check failure detected on %s", device)
		return
	}

	if matches := attrFailRe.FindStringSubmatch(msg); matches != nil {
		device := matches[1]
		attr := matches[2]
		m.alerts[device] = Alert{
			Device:    device,
			FailType:  "Attribute",
			Message:   "Failed SMART Attribute: " + attr,
			Timestamp: time.Now(),
		}
		log.Printf("[SmartdMonitor] Attribute failure detected on %s: %s", device, attr)
		return
	}
}

func (m *Monitor) cleanupResolvedAlerts() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for device, alert := range m.alerts {
		if now.Sub(alert.Timestamp) > m.alertRetention {
			delete(m.alerts, device)
			log.Printf("[SmartdMonitor] Alert resolved for %s", device)
		}
	}
}

func (m *Monitor) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	alerts := make([]Alert, 0, len(m.alerts))
	for _, alert := range m.alerts {
		alerts = append(alerts, alert)
	}

	return Stats{
		Alerts:        alerts,
		LastCheck:     time.Now().Format(time.RFC3339),
		NotifyService: m.notifyService,
	}
}

func (m *Monitor) GetNotifyService() string {
	return m.notifyService
}
