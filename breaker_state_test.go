package circuit

import (
	"context"
	"testing"
	"time"
)

func TestBreakerState_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		state State
		want  string
	}{
		{"closed", Closed, "closed"},
		{"throttled", Throttled, "throttled"},
		{"open", Open, "open"},
		{"unknown", State(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bs := BreakerState{State: tt.state}
			if got := bs.String(); got != tt.want {
				t.Fatalf("BreakerState.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBreaker_Name(t *testing.T) {
	t.Parallel()
	b := mustNewBreaker(t, WithName("my-breaker"))
	if b.Name() != "my-breaker" {
		t.Fatalf("Name() = %q, want %q", b.Name(), "my-breaker")
	}
}

func TestSnapshot_ClosedSinceZeroed(t *testing.T) {
	t.Parallel()
	b := mustNewBreaker(t, WithName("snap-zero"))
	// Zero out closedSince to hit the l==0 branch
	b.closedSince = 0
	snap := b.Snapshot()
	if snap.ClosedSince != nil {
		t.Fatal("expected ClosedSince to be nil when closedSince is zero")
	}
}

func TestSnapshot_Throttled(t *testing.T) {
	t.Parallel()
	b := mustNewBreaker(t,
		WithName("snap-throttle"),
		WithBackOff(time.Second),
		WithWindow(50*time.Millisecond),
	)
	b.tracker.incr()
	b.State() // closed -> open
	time.Sleep(60 * time.Millisecond)
	b.State() // open -> throttled

	snap := b.Snapshot()
	if snap.State != Throttled {
		t.Fatalf("expected Throttled, got %s", snap.State)
	}
	if snap.Throttled == nil {
		t.Fatal("expected Throttled timestamp to be set")
	}
	if snap.BackOffEnds == nil {
		t.Fatal("expected BackOffEnds timestamp to be set")
	}
}

func TestSnapshot_ThrottledSinceZeroed(t *testing.T) {
	t.Parallel()
	b := mustNewBreaker(t,
		WithName("snap-throttle-zero"),
		WithBackOff(time.Second),
		WithWindow(50*time.Millisecond),
	)
	b.tracker.incr()
	b.State() // closed -> open
	time.Sleep(60 * time.Millisecond)
	b.State() // open -> throttled

	// Zero out throttledSince to hit l==0 branch
	b.throttledSince = 0
	snap := b.Snapshot()
	if snap.Throttled != nil {
		t.Fatal("expected Throttled timestamp to be nil when zeroed")
	}
}

func TestApplyThrottle_NoTimestamp(t *testing.T) {
	t.Parallel()
	b := mustNewBreaker(t, WithName("throttle-zero"))
	// throttledSince is 0 by default on a closed breaker
	err := b.applyThrottle()
	if err != nil {
		t.Fatalf("expected nil error when not throttled, got %v", err)
	}
}

func TestApplyThrottle_PastBackoff(t *testing.T) {
	t.Parallel()
	b := mustNewBreaker(t,
		WithName("throttle-past"),
		WithBackOff(10*time.Millisecond),
		WithWindow(10*time.Millisecond),
	)
	// Set throttledSince to well in the past so tick > 100
	b.throttledSince = time.Now().Add(-time.Second).UnixNano()
	// Should clamp tick to 100 and not panic
	_ = b.applyThrottle()
}

func TestCheckFitness_TransitionWithOnStateChange(t *testing.T) {
	t.Parallel()
	var called bool
	b := mustNewBreaker(t,
		WithName("fitness-hook"),
		WithOnStateChange(func(name string, from, to State) {
			called = true
		}),
	)
	// Add errors to trigger transition during checkFitness
	b.tracker.incr()
	err := b.checkFitness(context.Background())
	if err == nil {
		// If transition happened, we should get an error (open)
		// or nil if still closed — depends on threshold
	}
	_ = err
	if !called {
		t.Fatal("expected onStateChange to be called during checkFitness")
	}
}

func TestEvaluateState_OpenWithErrorsAboveThreshold(t *testing.T) {
	t.Parallel()
	// Open breaker without lockout, but errors still exceed threshold
	// so it stays open (doesn't transition to throttled)
	b := mustNewBreaker(t, WithName("open-high-err"), WithThreshold(2), WithWindow(time.Second))
	b.tracker.incr()
	b.tracker.incr()
	b.tracker.incr() // exceed threshold
	b.State()        // closed -> open

	// Add more errors to keep above threshold
	b.tracker.incr()
	// No lockout, so lockout check passes, but errors > threshold
	// evaluateState should return without transitioning
	if b.State() != Open {
		t.Fatalf("expected Open (errors still above threshold), got %s", b.State())
	}
}

func TestEvaluateState_UnknownState(t *testing.T) {
	t.Parallel()
	b := mustNewBreaker(t, WithName("eval-unknown"))
	// Force an impossible state to exercise the default branch
	b.state = 99
	b.stateMX.Lock()
	_, _, transitioned := b.evaluateState()
	b.stateMX.Unlock()
	if transitioned {
		t.Fatal("expected no transition from unknown state")
	}
}

func TestChangeStateTo_SameState(t *testing.T) {
	t.Parallel()
	b := mustNewBreaker(t, WithName("same-state"))
	// Breaker starts in closed (0), calling changeStateTo(internalClosed) should be a no-op
	b.stateMX.Lock()
	_, changed := b.changeStateTo(internalClosed)
	b.stateMX.Unlock()
	if changed {
		t.Fatal("expected no transition when changing to same state")
	}
}

func TestSnapshot_TransitionWithOnStateChange(t *testing.T) {
	t.Parallel()
	var called bool
	b := mustNewBreaker(t,
		WithName("snap-hook"),
		WithOnStateChange(func(name string, from, to State) {
			called = true
		}),
	)
	b.tracker.incr()
	_ = b.Snapshot() // triggers evaluation and transition
	if !called {
		t.Fatal("expected onStateChange to be called during Snapshot")
	}
}
