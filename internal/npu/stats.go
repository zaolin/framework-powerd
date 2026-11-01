package npu

import "time"

// Context is one hardware context currently allocated on the NPU.
type Context struct {
	ContextID   uint32 `json:"context_id"`
	StartColumn uint32 `json:"start_column"`
	NumColumns  uint32 `json:"num_columns"`
	PID         int64  `json:"pid"`
	Submitted   uint64 `json:"command_submissions"`
	Completed   uint64 `json:"command_completions"`
	Migrations  uint64 `json:"migrations"`
	Preemptions uint64 `json:"preemptions"`
	Errors      uint64 `json:"errors"`
}

// Stats is a snapshot of the AMD XDNA NPU state.
type Stats struct {
	Present    bool   `json:"present"`
	Device     string `json:"device,omitempty"`
	PCIAddress string `json:"pci_address,omitempty"`
	VBNV       string `json:"vbnv,omitempty"`
	DeviceType string `json:"device_type,omitempty"`
	Firmware   string `json:"firmware_version,omitempty"`
	Revision   string `json:"revision,omitempty"`
	PowerState string `json:"power_state,omitempty"`
	PowerMode  string `json:"power_mode,omitempty"`

	// PowerWatts is the NPU's own power draw as reported by the platform
	// telemetry, not the whole system.
	PowerWatts float64 `json:"power_watts"`

	// ColumnUtilization holds one percentage per AIE column reported by the
	// driver, in column order.
	ColumnUtilization    []float64 `json:"column_utilization,omitempty"`
	ColumnUtilizationAvg float64   `json:"column_utilization_avg"`
	ColumnUtilizationMax float64   `json:"column_utilization_max"`

	MPNPUClockMHz uint32 `json:"mp_npu_clock_mhz"`
	HClockMHz     uint32 `json:"h_clock_mhz"`
	// NPUClockMaxMHz is the maximum H clock reported by the driver.
	NPUClockMaxMHz uint64 `json:"npu_clk_max_mhz"`

	TOPsCurrent  uint64 `json:"tops_current"`
	TOPsMax      uint64 `json:"tops_max"`
	TasksCurrent uint64 `json:"tasks_current"`
	TasksMax     uint64 `json:"tasks_max"`

	Contexts []Context `json:"contexts,omitempty"`

	LastUpdate time.Time `json:"last_update"`
	LastError  string    `json:"last_error,omitempty"`
}

// Clone returns a deep copy of the snapshot.
func (s Stats) Clone() Stats {
	out := s
	if s.ColumnUtilization != nil {
		out.ColumnUtilization = append([]float64(nil), s.ColumnUtilization...)
	}
	if s.Contexts != nil {
		out.Contexts = append([]Context(nil), s.Contexts...)
	}
	return out
}

// PowerModes returns the supported power mode names, in the order the driver
// defines them.
func PowerModes() []string {
	return []string{"default", "low", "medium", "high", "turbo"}
}

// powerModeValue maps a power mode name to the driver enum value.
func powerModeValue(name string) (uint8, bool) {
	for i, mode := range PowerModes() {
		if mode == name {
			return uint8(i), true
		}
	}
	return 0, false
}

// powerModeName maps a driver enum value to its name.
func powerModeName(value uint8) string {
	modes := PowerModes()
	if int(value) < len(modes) {
		return modes[value]
	}
	return "unknown"
}
