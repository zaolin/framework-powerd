package sysinfo

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// MemoryInfo is the dynamic memory state, refreshed on every status request.
// Unlike SystemInfo this is not collected once at startup.
type MemoryInfo struct {
	TotalBytes     int64   `json:"total_bytes"`
	FreeBytes      int64   `json:"free_bytes"`
	AvailableBytes int64   `json:"available_bytes"`
	UsedBytes      int64   `json:"used_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

// CollectMemory reads /proc/meminfo. It never fails: missing values are
// reported as zero.
func CollectMemory() MemoryInfo {
	return collectMemoryFrom("/proc/meminfo")
}

func collectMemoryFrom(path string) MemoryInfo {
	values := make(map[string]int64)

	f, err := os.Open(path)
	if err != nil {
		return MemoryInfo{}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		values[key] = kb * 1024 // /proc/meminfo values are in kB
	}

	total := values["MemTotal"]
	free := values["MemFree"]

	available, ok := values["MemAvailable"]
	if !ok {
		// Kernels before 3.14 do not export MemAvailable.
		available = free + values["Buffers"] + values["Cached"] +
			values["SReclaimable"] - values["Shmem"]
	}
	if available > total {
		available = total
	}
	if available < 0 {
		available = 0
	}

	used := total - available
	if used < 0 {
		used = 0
	}

	var usedPercent float64
	if total > 0 {
		usedPercent = float64(used) / float64(total) * 100
	}

	return MemoryInfo{
		TotalBytes:     total,
		FreeBytes:      free,
		AvailableBytes: available,
		UsedBytes:      used,
		UsedPercent:    usedPercent,
	}
}
