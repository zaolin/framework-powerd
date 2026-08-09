package power

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// ProcessPauser manages suspending and resuming processes
type ProcessPauser struct {
	mu         sync.Mutex
	pausedPIDs []int
	isPaused   bool
}

// NewProcessPauser creates a new pauser
func NewProcessPauser() *ProcessPauser {
	return &ProcessPauser{}
}

// Pause finds all descendants of the root PID and sends SIGSTOP
func (p *ProcessPauser) Pause(rootPID int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isPaused {
		return nil
	}

	if rootPID <= 0 {
		return fmt.Errorf("invalid pid %d", rootPID)
	}

	// Find *all* descendants
	descendants := findAllDescendants(rootPID)
	targets := append([]int{rootPID}, descendants...)

	log.Printf("[Pauser] Suspending Process Tree (Root: %d, Count: %d)...", rootPID, len(targets))

	for _, pid := range targets {
		if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
			// Log but continue
			// log.Printf("Failed to pause pid %d: %v", pid, err)
		}
	}

	p.isPaused = true
	p.pausedPIDs = targets
	return nil
}

// Resume sends SIGCONT to all paused processes
func (p *ProcessPauser) Resume() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isPaused {
		return nil
	}

	log.Printf("[Pauser] Resuming Process Tree (%d processes)...", len(p.pausedPIDs))

	// Resume in reverse order? Usually doesn't matter for SIGCONT, but let's just do it.
	for _, pid := range p.pausedPIDs {
		if err := syscall.Kill(pid, syscall.SIGCONT); err != nil {
			// log.Printf("Failed to resume pid %d: %v", pid, err)
		}
	}

	p.isPaused = false
	p.pausedPIDs = nil
	return nil
}

// IsPaused returns true if a process is currently suspended
func (p *ProcessPauser) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isPaused
}

// SyncState checks if the process is already paused (State 'T') and updates internal state.
// Only descendants that are themselves in a stopped state ('T' or 't') are recorded in
// pausedPIDs, so a later Resume() will not SIGCONT processes that were stopped by someone
// else (a debugger, another daemon) — A6.
func (p *ProcessPauser) SyncState(rootPID int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rootState, err := processState(rootPID)
	if err != nil {
		return
	}

	// 'T' = Stopped (on a signal) or (before Linux 2.6.33) trace stopped
	// 't' = Tracing stop
	if rootState == "T" || rootState == "t" {
		log.Printf("[Pauser] Detected process %d is ALREADY PAUSED (State: %s). Syncing state.", rootPID, rootState)
		p.isPaused = true

		// Only record descendants that are themselves stopped, so Resume()
		// never wakes a process we didn't stop (A6).
		descendants := findAllDescendants(rootPID)
		paused := []int{rootPID}
		for _, pid := range descendants {
			st, err := processState(pid)
			if err != nil {
				continue
			}
			if st == "T" || st == "t" {
				paused = append(paused, pid)
			}
		}
		p.pausedPIDs = paused
	} else {
		p.isPaused = false
		p.pausedPIDs = nil
	}
}

// processState reads /proc/<pid>/stat and returns the single-char process
// state (e.g. "R", "S", "T", "t"). Returns an error if the process is gone or
// the stat file is unparseable. Extracted from SyncState so it is testable.
func processState(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}

	str := string(data)
	lastParen := strings.LastIndex(str, ")")
	if lastParen == -1 {
		return "", fmt.Errorf("parse error: no closing paren")
	}
	rest := str[lastParen+2:]
	fields := strings.Fields(rest)
	if len(fields) < 1 {
		return "", fmt.Errorf("stat format error: no state field")
	}
	return fields[0], nil
}

// findAllDescendants brute-force scans /proc to find all children recursively
// This is somewhat expensive but robustness is key here.
func findAllDescendants(root int) []int {
	// 1. Build a map of PPID -> []PID
	tree := make(map[int][]int)

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Read Stat to get PPID
		// Using a simplified scanner since we just need the 4th field (usually)
		// But wait, comm can contain spaces and parens.
		// Let's reuse a simple parser logic here or duplicate it to avoid circular dependency (detector package has it)
		// Simpler: Just read /proc/<pid>/stat
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		str := string(data)
		lastParen := strings.LastIndex(str, ")")
		if lastParen == -1 {
			continue
		}
		rest := str[lastParen+2:]
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			continue
		}
		// parts[0] is state, parts[1] is ppid
		ppid, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		tree[ppid] = append(tree[ppid], pid)
	}

	// 2. Recursive Traversal
	var results []int
	var queue []int
	queue = append(queue, root)

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		children := tree[curr]
		results = append(results, children...)
		queue = append(queue, children...)
	}

	return results
}
