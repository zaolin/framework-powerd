package nvme

import "time"

// Health values for a device.
const (
	HealthOK       = "ok"
	HealthWarning  = "warning"
	HealthCritical = "critical"
)

// DeviceStats is the health snapshot for one NVMe controller.
type DeviceStats struct {
	Device                  string    `json:"device"`
	Model                   string    `json:"model,omitempty"`
	Serial                  string    `json:"serial,omitempty"`
	Firmware                string    `json:"firmware,omitempty"`
	Source                  string    `json:"source,omitempty"` // nvme | smartctl
	CriticalWarning         int       `json:"critical_warning"`
	PercentageUsed          int       `json:"percentage_used"`
	AvailableSpare          int       `json:"available_spare"`
	AvailableSpareThreshold int       `json:"available_spare_threshold"`
	MediaErrors             int64     `json:"media_errors"`
	ErrorLogEntries         int64     `json:"error_log_entries"`
	TemperatureC            int       `json:"temperature_celsius"`
	DataWrittenBytes        int64     `json:"data_written_bytes"`
	PowerOnHours            int64     `json:"power_on_hours"`
	UnsafeShutdowns         int64     `json:"unsafe_shutdowns"`
	Health                  string    `json:"health"`
	Issues                  []string  `json:"issues,omitempty"`
	LastUpdate              time.Time `json:"last_update"`
}

// Stats is the complete NVMe polling state.
type Stats struct {
	Enabled    bool          `json:"enabled"`
	Devices    []DeviceStats `json:"devices"`
	LastUpdate time.Time     `json:"last_update"`
	LastError  string        `json:"last_error,omitempty"`
}

// HealthSummary returns ok, warning or critical for the whole set of devices.
func (s Stats) HealthSummary() string {
	if len(s.Devices) == 0 && s.LastError != "" {
		return "disabled"
	}
	health := HealthOK
	for _, d := range s.Devices {
		switch d.Health {
		case HealthCritical:
			return HealthCritical
		case HealthWarning:
			health = HealthWarning
		}
	}
	return health
}
