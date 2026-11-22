package netaudit

import "time"

// Socket describes one listening TCP socket or bound UDP socket and its owner.
type Socket struct {
	Protocol    string `json:"protocol"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	PID         int    `json:"pid,omitempty"`
	Process     string `json:"process,omitempty"`
	Unit        string `json:"unit,omitempty"`
	Container   string `json:"container,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
}

// AllowedPort is one firewall rule that accepts traffic to a port or range.
type AllowedPort struct {
	Protocol string `json:"protocol"`
	Ports    string `json:"ports"`
	Table    string `json:"table,omitempty"`
	Chain    string `json:"chain,omitempty"`
}

// FirewallStats summarizes the detected local firewall backend.
type FirewallStats struct {
	Backend string        `json:"backend"`
	Rules   int           `json:"rules"`
	Allowed []AllowedPort `json:"allowed"`
	Error   string        `json:"error,omitempty"`
}

// Stats is the current network inventory.
type Stats struct {
	Enabled    bool          `json:"enabled"`
	Listening  []Socket      `json:"listening"`
	Firewall   FirewallStats `json:"firewall"`
	LastUpdate time.Time     `json:"last_update,omitzero"`
	LastError  string        `json:"last_error,omitempty"`
}
