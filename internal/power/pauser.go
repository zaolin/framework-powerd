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

	// S5: Safety guard — never pause PID 1 (init/systemd), the daemon's own
	// PID, or any ancestor of the daemon. Pausing init would freeze the
	// entire system; pausing the daemon would deadlock it.
	if rootPID == 1 {
		return fmt.Errorf("refusing to pause PID 1 (init)")
	}
	if rootPID == os.Getpid() {
		return fmt.Errorf("refusing to pause self (PID %d)", rootPID)
	}
	if isAncestorOf(rootPID, os.Getpid()) {
		return fmt.Errorf("refusing to pause ancestor PID %d of the daemon", rootPID)
	}

	// Find *all* descendants
	descendants := findAllDescendants(rootPID)
	targets := append([]int{rootPID}, descendants...)

	// S5: Filter out PID 1 and the daemon's own PID from descendants too.
	daemonPID := os.Getpid()
	safe := targets[:0]
	for _, pid := range targets {
		if pid == 1 || pid == daemonPID {
			log.Printf("[Pauser] Skipping unsafe PID %d in target list", pid)
			continue
		}
		safe = append(safe, pid)
	}
	targets = safe

	if len(targets) == 0 {
		return fmt.Errorf("no safe targets to pause after filtering PID 1/self")
	}

	log.Printf("[Pauser] Suspending Process Tree (Root: %d, Count: %d)...", rootPID, len(targets))

	// S11: TOCTOU mitigation — re-validate that each target PID's PPID
	// chain still leads back to rootPID before signaling. A PID may have
	// exited and been reused between findAllDescendants and now.
	// This is best-effort (the race window still exists, just narrower);
	// a full fix would use pidfd_open (Linux 5.3+).
	rootPPID := getRootPPID(rootPID)
	for _, pid := range targets {
		if pid == rootPID {
			continue
		}
		if !isDescendantOf(pid, rootPID, rootPPID) {
			log.Printf("[Pauser] Skipping PID %d: PPID chain no longer leads to root %d (possible PID reuse)\n", pid, rootPID)
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
			// Log but continue
		}
	}
	// Always SIGSTOP the root.
	syscall.Kill(rootPID, syscall.SIGSTOP)

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

// findAllDescendants brute-force scans /proc to find all children recursively.
// Takes procRoot (default "/proc") so it is testable with a temp directory (T3).
func findAllDescendants(root int) []int {
	return findAllDescendantsFromProc(root, "/proc")
}

func findAllDescendantsFromProc(root int, procRoot string) []int {
	// 1. Build a map of PPID -> []PID
	tree := make(map[int][]int)

	entries, err := os.ReadDir(procRoot)
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

		data, err := os.ReadFile(fmt.Sprintf("%s/%d/stat", procRoot, pid))
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

// getRootPPID returns the PPID of rootPID (used as a quick consistency check).
func getRootPPID(rootPID int) int {
	return getRootPPIDFromProc(rootPID, "/proc")
}

func getRootPPIDFromProc(rootPID int, procRoot string) int {
	ppid, _, err := readPPIDAndNameFromProc(rootPID, procRoot)
	if err != nil {
		return -1
	}
	return ppid
}

// isDescendantOf walks the PPID chain of pid and returns true if it reaches
// rootPID (or rootPPID, as a shortcut). Bounded to 20 levels to prevent loops.
func isDescendantOf(pid, rootPID, rootPPID int) bool {
	return isDescendantOfProc(pid, rootPID, rootPPID, "/proc")
}

func isDescendantOfProc(pid, rootPID, rootPPID int, procRoot string) bool {
	current := pid
	for i := 0; i < 20; i++ {
		ppid, _, err := readPPIDAndNameFromProc(current, procRoot)
		if err != nil {
			return false
		}
		if ppid == rootPID || (rootPPID > 0 && ppid == rootPPID) {
			return true
		}
		if ppid <= 1 {
			return false
		}
		current = ppid
	}
	return false
}

// isAncestorOf walks the PPID chain of descendant and returns true if
// ancestor appears in it. Used by Pause to prevent the daemon from
// pausing its own parent chain (S5).
func isAncestorOf(ancestor, descendant int) bool {
	return isAncestorOfProc(ancestor, descendant, "/proc")
}

func isAncestorOfProc(ancestor, descendant int, procRoot string) bool {
	current := descendant
	for i := 0; i < 20; i++ { // bounded walk to prevent loops
		ppid, _, err := readPPIDAndNameFromProc(current, procRoot)
		if err != nil {
			return false
		}
		if ppid == ancestor {
			return true
		}
		if ppid <= 1 {
			return false
		}
		current = ppid
	}
	return false
}

// getProcessPPIDAndName reads /proc/<pid>/stat and returns the PPID and
// process name. Shared by isAncestorOf and the detector package's
// getProcessInfo (which has its own copy — future consolidation target).
func getProcessPPIDAndName(pid int) (int, string, error) {
	return readPPIDAndNameFromProc(pid, "/proc")
}

// readPPIDAndNameFromProc reads /<procRoot>/<pid>/stat and returns the PPID
// and process name. Takes procRoot so it is testable with a temp directory (T3).
func readPPIDAndNameFromProc(pid int, procRoot string) (int, string, error) {
	data, err := os.ReadFile(fmt.Sprintf("%s/%d/stat", procRoot, pid))
	if err != nil {
		return 0, "", err
	}

	str := string(data)
	start := strings.Index(str, "(")
	end := strings.LastIndex(str, ")")
	if start == -1 || end == -1 || end < start {
		return 0, "", fmt.Errorf("parse error")
	}

	name := str[start+1 : end]
	rest := str[end+2:]
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
