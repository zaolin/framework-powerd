package smartd

import "time"

// Alert is a sticky, de-duplicated SMART event. It stays visible for
// alert_retention after the last time it was seen, instead of disappearing
// seconds after smartd logged once.
type Alert struct {
	ID        string    `json:"id"`
	Device    string    `json:"device"`
	FailType  string    `json:"fail_type"`
	Severity  string    `json:"severity"` // info | warning | critical
	Message   string    `json:"message"`
	Raw       string    `json:"raw,omitempty"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     int       `json:"count"`
	// Timestamp mirrors LastSeen for older clients.
	Timestamp time.Time `json:"timestamp"`
}

// Stats holds the complete SMART state exposed via /smartd/stats.
type Stats struct {
	Alerts        []Alert `json:"alerts"`
	LastCheck     string  `json:"last_check"`
	NotifyService string  `json:"notify_service,omitempty"`
}

// alertsState is the persisted form.
type alertsState struct {
	Version int       `json:"version"`
	SavedAt time.Time `json:"saved_at"`
	Alerts  []Alert   `json:"alerts"`
}
