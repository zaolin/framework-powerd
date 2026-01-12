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

// SyncState checks if the process is already paused (State 'T') and updates internal state
func (p *ProcessPauser) SyncState(rootPID int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", rootPID))
	if err != nil {
		return
	}

	str := string(data)
	lastParen := strings.LastIndex(str, ")")
	if lastParen == -1 {
		return
	}
	// format: pid (comm) state ...
	// state is the first field after the closing paren
	rest := str[lastParen+2:]
	fields := strings.Fields(rest)
	if len(fields) < 1 {
		return
	}

	state := fields[0]
	// 'T' = Stopped (on a signal) or (before Linux 2.6.33) trace stopped
	// 't' = Tracing stop
	if state == "T" {
		log.Printf("[Pauser] Detected process %d is ALREADY PAUSED (State: %s). Syncing state.", rootPID, state)
		p.isPaused = true

		// If it's paused, we assume the whole tree is paused (or should be).
		// We need to populate pausedPIDs so Resume() works later.
		descendants := findAllDescendants(rootPID)
		p.pausedPIDs = append([]int{rootPID}, descendants...)
	} else {
		// Log verbose?
		// log.Printf("[Pauser] Process %d state is %s (Running/Sleeping).", rootPID, state)
		p.isPaused = false
		p.pausedPIDs = nil
	}
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
