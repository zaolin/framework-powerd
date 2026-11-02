package fan

import "time"

// SensorSet is one temperature snapshot, in degrees Celsius. Optional values
// are nil when the source is not present on the machine.
type SensorSet struct {
	CPU    *float64 `json:"cpu_celsius,omitempty"`
	CPUEC  *float64 `json:"cpu_ec_celsius,omitempty"`
	Board  *float64 `json:"board_celsius,omitempty"`
	GPU    *float64 `json:"gpu_celsius,omitempty"`
	NVMe   *float64 `json:"nvme_celsius,omitempty"`
	FanRPM *int     `json:"fan_rpm,omitempty"`

	// Max is the hottest of all sources and drives the curve.
	Max float64 `json:"max_celsius"`
}

// Clone returns a deep copy of the snapshot.
func (s SensorSet) Clone() SensorSet {
	out := s
	out.CPU = cloneFloat(s.CPU)
	out.CPUEC = cloneFloat(s.CPUEC)
	out.Board = cloneFloat(s.Board)
	out.GPU = cloneFloat(s.GPU)
	out.NVMe = cloneFloat(s.NVMe)
	if s.FanRPM != nil {
		rpm := *s.FanRPM
		out.FanRPM = &rpm
	}
	return out
}

func cloneFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

// Stats is the fan controller snapshot.
type Stats struct {
	Enabled bool `json:"enabled"`
	// Supported is false when framework_tool is missing or the daemon is not root.
	Supported   bool      `json:"supported"`
	Mode        string    `json:"mode"` // auto | curve
	DutyPercent int       `json:"duty_percent"`
	AppliedAt   time.Time `json:"applied_at,omitzero"`
	Sensors     SensorSet `json:"sensors"`
	LastUpdate  time.Time `json:"last_update,omitzero"`
	LastError   string    `json:"last_error,omitempty"`
}

// Clone returns a deep copy of the snapshot.
func (s Stats) Clone() Stats {
	out := s
	out.Sensors = s.Sensors.Clone()
	return out
}
