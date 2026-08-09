package monitor

import (
	"context"
	"testing"
	"time"
)

// TestIdleMonitor_Transitions verifies A5: the idle flag is exposed via IsIdle()
// and the onIdle/onActive callbacks fire exactly once on each edge transition.
// Drives checkIdleTick directly so the test is deterministic and unaffected by
// real input device activity on the host.
func TestIdleMonitor_Transitions(t *testing.T) {
	m := NewIdleMonitor(100*time.Millisecond, false)

	var idleCount, activeCount int
	onIdle := func() { idleCount++ }
	onActive := func() { activeCount++ }

	// Freshly created monitor is NOT idle.
	if m.IsIdle() {
		t.Fatal("new monitor should report not idle")
	}

	// First tick: elapsed since creation is ~0 (< timeout), still active.
	m.checkIdleTick(onIdle, onActive)
	if m.IsIdle() || idleCount != 0 || activeCount != 0 {
		t.Fatalf("pre-timeout tick: isIdle=%v idleCount=%d activeCount=%d", m.IsIdle(), idleCount, activeCount)
	}

	// Wait past the timeout, then tick: should fire onIdle once.
	time.Sleep(150 * time.Millisecond)
	m.checkIdleTick(onIdle, onActive)
	if !m.IsIdle() || idleCount != 1 || activeCount != 0 {
		t.Fatalf("post-timeout tick: isIdle=%v idleCount=%d activeCount=%d", m.IsIdle(), idleCount, activeCount)
	}

	// Subsequent ticks while still idle must NOT re-fire onIdle.
	m.checkIdleTick(onIdle, onActive)
	if idleCount != 1 {
		t.Fatalf("redundant idle tick fired: idleCount=%d, want 1", idleCount)
	}

	// Reset activity; next tick fires onActive exactly once.
	m.ResetActivity()
	m.checkIdleTick(onIdle, onActive)
	if m.IsIdle() || activeCount != 1 || idleCount != 1 {
		t.Fatalf("post-reset tick: isIdle=%v idleCount=%d activeCount=%d", m.IsIdle(), idleCount, activeCount)
	}

	// Subsequent active ticks must NOT re-fire onActive.
	m.checkIdleTick(onIdle, onActive)
	if activeCount != 1 {
		t.Fatalf("redundant active tick fired: activeCount=%d, want 1", activeCount)
	}
}

// TestIdleMonitor_GetTimeUntilIdle_Monotonic verifies A5: GetTimeUntilIdle uses
// nanosecond resolution and decreases monotonically until it hits zero.
func TestIdleMonitor_GetTimeUntilIdle_Monotonic(t *testing.T) {
	m := NewIdleMonitor(500*time.Millisecond, false)

	first := m.GetTimeUntilIdle()
	if first <= 0 || first > 500*time.Millisecond {
		t.Fatalf("first remaining = %v, want (0, 500ms]", first)
	}

	// Sleep a bit and confirm it decreased.
	time.Sleep(120 * time.Millisecond)
	second := m.GetTimeUntilIdle()
	if second >= first {
		t.Fatalf("remaining did not decrease: first=%v second=%v", first, second)
	}

	// After the full timeout, it should clamp to zero.
	time.Sleep(500 * time.Millisecond)
	if got := m.GetTimeUntilIdle(); got != 0 {
		t.Fatalf("after timeout, remaining = %v, want 0", got)
	}
}

// TestIdleMonitor_StartReturnsNilOnFsnotifyFailure confirms A8: even if fsnotify
// fails to create a watcher, Start does not abort — it logs and continues with
// the ticker only. We can't easily force fsnotify to fail, so this test just
// asserts Start returns nil in the normal case (the failure path is logged).
func TestIdleMonitor_StartReturnsNil(t *testing.T) {
	m := NewIdleMonitor(time.Hour, false) // long timeout so no callbacks fire
	m.tickInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx, func() {}, func() {}); err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}
}