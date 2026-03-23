package smartd

import "time"

type Alert struct {
	Device    string    `json:"device"`
	FailType  string    `json:"fail_type"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type Stats struct {
	Alerts        []Alert `json:"alerts"`
	LastCheck     string  `json:"last_check"`
	NotifyService string  `json:"notify_service,omitempty"`
}
