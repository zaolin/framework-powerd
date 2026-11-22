package netaudit

import (
	"context"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zaolin/framework-powerd/internal/config"
)

// Monitor periodically inventories local listening sockets and firewall rules.
type Monitor struct {
	mu          sync.RWMutex
	cfg         config.NetworkAuditConfig
	procRoot    string
	runner      CommandRunner
	runtimeStat func(string) error
	now         func() time.Time
	stats       Stats
}

// NewMonitor creates the network inventory monitor.
func NewMonitor(cfg config.NetworkAuditConfig) *Monitor {
	return &Monitor{
		cfg:      cfg,
		procRoot: "/proc",
		runner:   realRunner{timeout: 10 * time.Second},
		now:      time.Now,
		stats: Stats{
			Enabled:   cfg.Enabled,
			Listening: []Socket{},
			Firewall: FirewallStats{
				Backend: "waiting",
				Allowed: []AllowedPort{},
			},
		},
	}
}

// Start polls immediately and then on the configured interval until stopped.
func (m *Monitor) Start(ctx context.Context) {
	if !m.cfg.Enabled {
		return
	}
	m.poll(ctx)

	ticker := time.NewTicker(m.cfg.GetPollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll(ctx)
		}
	}
}

// GetStats returns the latest completed snapshot.
func (m *Monitor) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats
}

// Health summarizes monitor state for /healthz.
func (m *Monitor) Health() string {
	stats := m.GetStats()
	if !stats.Enabled {
		return "disabled"
	}
	if stats.LastUpdate.IsZero() {
		return "waiting"
	}
	if stats.LastError != "" {
		return "error"
	}
	return "ok"
}

func (m *Monitor) poll(ctx context.Context) {
	stats := Stats{
		Enabled:    true,
		Listening:  []Socket{},
		LastUpdate: m.now(),
	}

	listening, err := m.collectSockets(ctx)
	if err != nil {
		stats.LastError = err.Error()
	}
	stats.Listening = listening
	stats.Firewall = collectFirewall(ctx, m.runner)
	if stats.Firewall.Error != "" && stats.LastError == "" {
		stats.LastError = stats.Firewall.Error
	}

	m.mu.Lock()
	m.stats = stats
	m.mu.Unlock()
}

func (m *Monitor) collectSockets(ctx context.Context) ([]Socket, error) {
	var refs []socketRef
	readAny := false
	for _, protocol := range []string{"tcp", "tcp6", "udp", "udp6"} {
		data, err := os.ReadFile(m.procRoot + "/net/" + protocol)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		readAny = true
		parsed, err := parseProcNet(data, protocol)
		if err != nil {
			return nil, err
		}
		refs = append(refs, parsed...)
	}
	if !readAny {
		return nil, os.ErrNotExist
	}

	if len(refs) == 0 {
		return []Socket{}, nil
	}

	wanted := map[string]bool{}
	for _, ref := range refs {
		wanted[ref.Inode] = true
	}
	processes := findProcesses(m.procRoot, wanted)
	inventory, runtimeErr := discoverRuntimes(ctx, m.runtimeStat)

	seen := map[string]bool{}
	sockets := make([]Socket, 0, len(refs))
	for _, ref := range refs {
		key := ref.Protocol + "/" + ref.Address + "/" + ref.Inode
		if seen[key] {
			continue
		}
		seen[key] = true

		socket := Socket{
			Protocol: ref.Protocol,
			Address:  ref.Address,
			Port:     ref.Port,
		}
		if meta, ok := processes[ref.Inode]; ok {
			socket.PID = meta.PID
			socket.Process = meta.Name
			socket.Unit = meta.Unit
			socket.ContainerID = meta.ContainerID
			socket.Runtime = meta.Runtime
			if meta.ContainerID != "" {
				if identity, ok := inventory.lookupID(meta.ContainerID); ok {
					socket.Container = identity.Name
					socket.Runtime = identity.Runtime
					socket.ContainerID = identity.ID
				}
			}
		}
		// docker-proxy and similar host-side proxies do not share the
		// container's cgroup. Resolve those through the published port map.
		if socket.Container == "" {
			if identity, ok := inventory.lookupPort(ref.Protocol, ref.Address, ref.Port); ok {
				socket.Container = identity.Name
				socket.ContainerID = identity.ID
				socket.Runtime = identity.Runtime
			}
		}
		if socket.Container == "" && socket.ContainerID != "" && len(socket.ContainerID) > 12 {
			socket.ContainerID = socket.ContainerID[:12]
		}
		sockets = append(sockets, socket)
	}

	sort.Slice(sockets, func(i, j int) bool {
		if sockets[i].Port != sockets[j].Port {
			return sockets[i].Port < sockets[j].Port
		}
		if sockets[i].Protocol != sockets[j].Protocol {
			return sockets[i].Protocol < sockets[j].Protocol
		}
		return strings.Compare(sockets[i].Address, sockets[j].Address) < 0
	})
	return sockets, runtimeErr
}
