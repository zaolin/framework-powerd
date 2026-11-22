package led

import "time"

// Stats is the current RGB keyboard controller state.
type Stats struct {
	Mode            string    `json:"mode"`
	Supported       bool      `json:"supported"`
	Frame           int       `json:"frame"`
	FrameIntervalMS int       `json:"frame_interval_ms"`
	Driver          string    `json:"driver,omitempty"`
	LastUpdate      time.Time `json:"last_update,omitzero"`
	LastError       string    `json:"last_error,omitempty"`
}
