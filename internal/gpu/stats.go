package gpu

import "time"

type Stats struct {
	VRAMUsedBytes   int64     `json:"vram_used_bytes"`
	VRAMTotalBytes  int64     `json:"vram_total_bytes"`
	VRAMFreeBytes   int64     `json:"vram_free_bytes"`
	VRAMUsedPercent float64   `json:"vram_used_percent"`
	GTTUsedBytes    int64     `json:"gtt_used_bytes"`
	GTTTotalBytes   int64     `json:"gtt_total_bytes"`
	GTTFreeBytes    int64     `json:"gtt_free_bytes"`
	GTTUsedPercent  float64   `json:"gtt_used_percent"`
	CPUUsagePercent float64   `json:"cpu_usage_percent"`
	TemperatureC    int       `json:"temperature_celsius"`
	PowerWatts      float64   `json:"power_watts"`
	LastUpdate      time.Time `json:"last_update"`
}

type CPUStats struct {
	User    uint64
	Nice    uint64
	System  uint64
	Idle    uint64
	IOWait  uint64
	IRQ     uint64
	SoftIRQ uint64
	Steal   uint64
	Total   uint64
}
