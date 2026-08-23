// Package sysinfo collects static system information at daemon startup.
package sysinfo

import (
	"bufio"
	"os"
	"strings"
)

// DaemonVersion is set by main.go at startup (from the CLI/manifest).
var DaemonVersion = "dev"

// SystemInfo holds static system information collected once at startup.
type SystemInfo struct {
	KernelVersion string `json:"kernel_version"`
	OSName        string `json:"os_name"`
	OSVersion     string `json:"os_version"`
	CPUModel      string `json:"cpu_model"`
	CPUCount      int    `json:"cpu_count"`
	TotalRAMBytes int64  `json:"total_ram_bytes"`
	DaemonVersion string `json:"daemon_version"`
}

// Collect gathers static system info once at startup. It never panics —
// missing files yield empty/zero values.
func Collect() SystemInfo {
	return SystemInfo{
		KernelVersion: readKernelVersion("/proc/sys/kernel/osrelease"),
		OSName:        readOSField("/etc/os-release", "NAME"),
		OSVersion:    readOSField("/etc/os-release", "VERSION"),
		CPUModel:      parseCPUModel("/proc/cpuinfo"),
		CPUCount:      countCPUs("/proc/cpuinfo"),
		TotalRAMBytes: parseMemTotal("/proc/meminfo"),
		DaemonVersion: DaemonVersion,
	}
}

// readKernelVersion reads the kernel release from the given path.
func readKernelVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readOSField extracts a single KEY=value field from an os-release file.
// Strips surrounding quotes from the value.
func readOSField(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	prefix := key + "="
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, prefix) {
			val := strings.TrimPrefix(line, prefix)
			val = strings.Trim(val, `"'`)
			return val
		}
	}
	return ""
}

// parseCPUModel reads /proc/cpuinfo and returns the first "model name" line.
func parseCPUModel(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// countCPUs counts the number of "processor" entries in /proc/cpuinfo.
func countCPUs(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "processor") {
			count++
		}
	}
	return count
}

// parseMemTotal reads /proc/meminfo and returns MemTotal in bytes.
func parseMemTotal(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			// Format: "MemTotal:       48838284 kB"
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				var kb int64
				for _, ch := range fields[1] {
					if ch >= '0' && ch <= '9' {
						kb = kb*10 + int64(ch-'0')
					} else {
						break
					}
				}
				return kb * 1024 // kB → bytes
			}
		}
	}
	return 0
}