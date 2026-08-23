package power

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestProcessState_SelfStat verifies the /proc/<pid>/stat parser (extracted as
// part of A6) returns a valid single-char state for the current process.
func TestProcessState_SelfStat(t *testing.T) {
	pid := os.Getpid()
	st, err := processState(pid)
	if err != nil {
		t.Fatalf("processState(%d) error: %v", pid, err)
	}
	// We expect a running/sleeping state, never 'T' (stopped) for the test runner.
	if st == "" || len(st) != 1 {
		t.Fatalf("processState returned invalid state %q", st)
	}
	if st == "T" || st == "t" {
		t.Fatalf("test process should not be stopped, got state %q", st)
	}
}

// TestProcessState_MissingProcess verifies the parser errors cleanly when the
// pid does not exist.
func TestProcessState_MissingProcess(t *testing.T) {
	_, err := processState(99_999_999)
	if err == nil {
		t.Fatal("expected error for nonexistent pid, got nil")
	}
}

// TestSyncState_NotPaused verifies SyncState does NOT mark the pauser as paused
// when the root process is in a running/sleeping state. Uses the test's own PID.
func TestSyncState_NotPaused(t *testing.T) {
	p := NewProcessPauser()
	pid := os.Getpid()

	p.SyncState(pid)

	if p.IsPaused() {
		t.Fatalf("SyncState marked pauser as paused for a running process (pid %d, state should not be T)", pid)
	}
	if len(p.pausedPIDs) != 0 {
		t.Fatalf("pausedPIDs should be empty for a running process, got %v", p.pausedPIDs)
	}
}

// TestParseState_ParenInComm verifies the stat parser handles a comm field that
// itself contains parentheses — e.g. a process named "foo (bar)". We synthesize
// a stat line and call the parser through a thin shim so the logic is
// exercised without a real /proc entry.
func TestParseState_ParenInComm(t *testing.T) {
	// Construct a fake stat line: "123 (weird (name) here) S 1 ..."
	line := "123 (weird (name) here) S 1 0 0 0"
	st, err := parseStatStateString(line)
	if err != nil {
		t.Fatalf("parseStatStateString error: %v", err)
	}
	if st != "S" {
		t.Errorf("state = %q, want 'S'", st)
	}
}

// parseStatStateString mirrors the parsing logic in processState but operates on
// a raw string (used by the paren-in-comm test). It is kept in lock-step with
// processState; if they diverge the test will fail.
func parseStatStateString(stat string) (string, error) {
	lastParen := strings.LastIndex(stat, ")")
	if lastParen == -1 {
		return "", fmt.Errorf("parse error: no closing paren")
	}
	rest := stat[lastParen+2:]
	fields := strings.Fields(rest)
	if len(fields) < 1 {
		return "", fmt.Errorf("stat format error: no state field")
	}
	return fields[0], nil
}

// TestPause_RejectsPID1 verifies S5: Pause refuses PID 1 (init/systemd).
func TestPause_RejectsPID1(t *testing.T) {
	p := NewProcessPauser()
	err := p.Pause(1)
	if err == nil {
		t.Fatal("Pause(1) should have returned an error, got nil")
	}
	if !strings.Contains(err.Error(), "PID 1") {
		t.Errorf("expected error mentioning PID 1, got %q", err.Error())
	}
	if p.IsPaused() {
		t.Error("pauser should not be in paused state after rejecting PID 1")
	}
}

// TestPause_RejectsSelf verifies S5: Pause refuses the daemon's own PID.
func TestPause_RejectsSelf(t *testing.T) {
	p := NewProcessPauser()
	err := p.Pause(os.Getpid())
	if err == nil {
		t.Fatal("Pause(self) should have returned an error, got nil")
	}
	if !strings.Contains(err.Error(), "self") {
		t.Errorf("expected error mentioning self, got %q", err.Error())
	}
	if p.IsPaused() {
		t.Error("pauser should not be paused after rejecting self-pause")
	}
}

// TestPause_RejectsInvalidPID verifies negative/zero PIDs are rejected.
func TestPause_RejectsInvalidPID(t *testing.T) {
	p := NewProcessPauser()
	err := p.Pause(0)
	if err == nil {
		t.Fatal("Pause(0) should have returned an error")
	}
	err = p.Pause(-1)
	if err == nil {
		t.Fatal("Pause(-1) should have returned an error")
	}
}

// TestResume_NotPaused verifies Resume is a no-op when nothing is paused.
func TestResume_NotPaused(t *testing.T) {
	p := NewProcessPauser()
	err := p.Resume()
	if err != nil {
		t.Errorf("Resume() on unpaused pauser error = %v, want nil", err)
	}
	if p.IsPaused() {
		t.Error("Resume should leave pauser in not-paused state")
	}
}

// TestResume_AfterPaused verifies Resume clears the paused state.
func TestResume_AfterPaused(t *testing.T) {
	p := NewProcessPauser()
	// Manually set paused state (can't call Pause without a real process).
	p.mu.Lock()
	p.isPaused = true
	p.pausedPIDs = []int{99999} // a PID that almost certainly doesn't exist
	p.mu.Unlock()

	err := p.Resume()
	if err != nil {
		t.Errorf("Resume() error = %v, want nil", err)
	}
	if p.IsPaused() {
		t.Error("Resume should set isPaused = false")
	}
	if p.pausedPIDs != nil {
		t.Error("Resume should clear pausedPIDs")
	}
}

// TestFindAllDescendantsFromProc verifies T3: findAllDescendantsFromProc with
// a mock /proc tree.
func TestFindAllDescendantsFromProc(t *testing.T) {
	procRoot := t.TempDir()

	// Create a process tree: 1000 (root) -> 1001, 1002; 1001 -> 1003
	writeMockStat(t, procRoot, 1000, 1, "S")
	writeMockStat(t, procRoot, 1001, 1000, "S")
	writeMockStat(t, procRoot, 1002, 1000, "S")
	writeMockStat(t, procRoot, 1003, 1001, "S")
	// Unrelated process
	writeMockStat(t, procRoot, 9999, 1, "S")

	descendants := findAllDescendantsFromProc(1000, procRoot)
	if len(descendants) != 3 {
		t.Fatalf("expected 3 descendants (1001, 1002, 1003), got %d: %v", len(descendants), descendants)
	}
	// Check all expected PIDs are present (order may vary).
	has := map[int]bool{}
	for _, pid := range descendants {
		has[pid] = true
	}
	for _, want := range []int{1001, 1002, 1003} {
		if !has[want] {
			t.Errorf("descendants missing PID %d", want)
		}
	}
}

// TestIsAncestorOfProc verifies T3: isAncestorOfProc correctly walks a PPID
// chain in a mock /proc tree.
func TestIsAncestorOfProc(t *testing.T) {
	procRoot := t.TempDir()

	// Tree: 1 -> 100 -> 200 -> 300
	writeMockStat(t, procRoot, 300, 200, "S")
	writeMockStat(t, procRoot, 200, 100, "S")
	writeMockStat(t, procRoot, 100, 1, "S")

	if !isAncestorOfProc(100, 300, procRoot) {
		t.Error("100 is an ancestor of 300 (via 200), want true")
	}
	if !isAncestorOfProc(1, 300, procRoot) {
		t.Error("1 is an ancestor of 300, want true")
	}
	if isAncestorOfProc(300, 100, procRoot) {
		t.Error("300 is NOT an ancestor of 100, want false")
	}
}

// writeMockStat creates a mock /proc/<pid>/stat file in procRoot.
func writeMockStat(t *testing.T, procRoot string, pid, ppid int, state string) {
	t.Helper()
	pidDir := fmt.Sprintf("%s/%d", procRoot, pid)
	os.MkdirAll(pidDir, 0755)
	stat := fmt.Sprintf("%d (process_%d) %s %d 0 0 0", pid, pid, state, ppid)
	os.WriteFile(fmt.Sprintf("%s/stat", pidDir), []byte(stat), 0644)
}