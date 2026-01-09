package detector

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"syscall"
	"time"
)

// SteamDetector monitors running processes for Steam games
type SteamDetector struct {
	Interval time.Duration
	LastPID  int
}

// NewSteamDetector creates a new detector
func NewSteamDetector(interval time.Duration) *SteamDetector {
	return &SteamDetector{
		Interval: interval,
	}
}

// Start begins the polling loop
func (d *SteamDetector) Start(ctx context.Context, onGameStart func(pid int), onGameStop func()) {
	ticker := time.NewTicker(d.Interval)
	defer ticker.Stop()

	// Initial check
	d.check(onGameStart, onGameStop)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.check(onGameStart, onGameStop)
		}
	}
}

func (d *SteamDetector) check(onGameStart func(pid int), onGameStop func()) {
	pid, err := d.detect()
	if err != nil {
		log.Printf("[SteamDetector] Error scanning processes: %v\n", err)
		return
	}

	if pid > 0 {
		// specific game detected
		if d.LastPID != pid {
			log.Printf("[SteamDetector] Game detected (PID: %d)\n", pid)
			d.LastPID = pid
			onGameStart(pid)
		}
	} else {
		// no game detected
		if d.LastPID > 0 {
			log.Printf("[SteamDetector] Game stopped (PID: %d)\n", d.LastPID)
			d.LastPID = 0
			onGameStop()
		}
	}
}

// detect scans /proc for the highest PID having SteamAppId set owned by UID 1000
func (d *SteamDetector) detect() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}

	var maxPID int
	const targetUID = 1000

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a PID directory
		}

		// Optimization: Check process ownership first to avoid expensive environ read
		// We use entry.Info() which does an Lstat
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			if int(stat.Uid) != targetUID {
				continue
			}
		}

		if hasSteamAppId(pid) {
			if pid > maxPID {
				maxPID = pid
			}
		}
	}

	return maxPID, nil
}

func hasSteamAppId(pid int) bool {
	// 1. Check /proc/<pid>/environ
	// It is a series of "KEY=VAL\0"
	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return false // process likely vanished or permission denied
	}

	// Efficient search: look for "SteamAppId=" byte sequence
	// We handle the case where it might be at start or preceded by \0

	// We specifically look for "SteamAppId="
	// And verify it has a value.

	// Convert to string for easier searching? Or bytes?
	// Bytes is faster.

	sub := []byte("SteamAppId=")

	idx := bytes.Index(content, sub)
	if idx == -1 {
		return false
	}

	// Check boundary: it must be at index 0 OR preceded by null byte
	if idx > 0 && content[idx-1] != 0 {
		return false // e.g. "FakeSteamAppId="
	}

	// Verify it has a value after "=" until the next \0
	// "SteamAppId=123\0"

	valStart := idx + len(sub)
	if valStart >= len(content) {
		return false
	}

	// Check if the value is not empty (i.e., next char is not \0, or end of slice)
	if content[valStart] == 0 {
		return false
	}

	// We found it!
	return true
}
