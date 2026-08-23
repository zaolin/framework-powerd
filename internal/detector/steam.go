package detector

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
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
	pid, err := d.Detect()
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

// Detect scans /proc for the highest PID having SteamAppId set owned by UID 1000
func (d *SteamDetector) Detect() (int, error) {
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

	if maxPID > 0 {
		return d.findGameRoot(maxPID), nil
	}

	return 0, nil
}

// findGameRoot walks up the process tree to find the best process to pause
// Priority:
// 1. wineserver (for Proton/Wine games) - pausing this pauses the entire Windows environment safely
// 2. Child of 'reaper' - pausing the reaper itself can cause instability/disconnects.
func (d *SteamDetector) findGameRoot(pid int) int {
	current := pid
	prev := pid // Keep track of the previous PID (child of current)

	for i := 0; i < 10; i++ { // Max depth 10 to avoid infinite loops
		ppid, name, err := getProcessInfo(current)
		if err != nil {
			return pid // Fallback to last known good
		}

		// 1. Prefer wineserver
		if strings.Contains(name, "wineserver") {
			log.Printf("[SteamDetector] Found wineserver root: %d\n", current)
			return current
		}

		// 2. Stop at reaper, but return its child
		if strings.Contains(name, "reaper") {
			log.Printf("[SteamDetector] Found Reaper (%d). Using child (%d) as root to avoid pausing reaper.\n", current, prev)
			return prev
		}

		if ppid <= 1 {
			return pid // Reached init
		}

		prev = current
		current = ppid
	}
	return pid
}

func getProcessInfo(pid int) (int, string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, "", err
	}
	return parseStatLine(string(data))
}

// parseStatLine extracts the PPID and process name from a /proc/<pid>/stat
// content string. Extracted from getProcessInfo so it is testable without
// /proc (T5).
func parseStatLine(stat string) (int, string, error) {
	str := stat
	start := strings.Index(str, "(")
	end := strings.LastIndex(str, ")")
	if start == -1 || end == -1 || end < start {
		return 0, "", fmt.Errorf("parse error")
	}

	name := str[start+1 : end]
	rest := ""
	if end+2 < len(str) {
		rest = str[end+2:]
	} else if end+1 < len(str) {
		rest = str[end+1:]
	}
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return 0, "", fmt.Errorf("stat format error")
	}

	ppid, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, "", err
	}

	return ppid, name, nil
}

func hasSteamAppId(pid int) bool {
	content, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return false
	}
	return environHasSteamAppId(content)
}

// environHasSteamAppId checks whether the environ byte content contains a
// non-empty SteamAppId= entry. Extracted from hasSteamAppId so it is testable
// without /proc (T6).
func environHasSteamAppId(content []byte) bool {
	sub := []byte("SteamAppId=")

	idx := bytes.Index(content, sub)
	if idx == -1 {
		return false
	}

	// Check boundary: it must be at index 0 OR preceded by null byte
	if idx > 0 && content[idx-1] != 0 {
		return false // e.g. "FakeSteamAppId="
	}

	valStart := idx + len(sub)
	if valStart >= len(content) {
		return false
	}

	// Check if the value is not empty (i.e., next char is not \0)
	if content[valStart] == 0 {
		return false
	}

	return true
}
