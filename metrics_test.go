package circuit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type mockMetrics struct {
	mu            sync.Mutex
	successes     int
	errors        int
	timeouts      int
	stateChanges  int
	rejected      int
	excluded      int
	lastFrom      State
	lastTo        State
	lastRejState  State
}

func (m *mockMetrics) RecordSuccess(string, time.Duration) {
	m.mu.Lock()
	m.successes++
	m.mu.Unlock()
}

func (m *mockMetrics) RecordError(string, time.Duration, error) {
	m.mu.Lock()
	m.errors++
	m.mu.Unlock()
}

func (m *mockMetrics) RecordTimeout(string) {
	m.mu.Lock()
	m.timeouts++
	m.mu.Unlock()
}

func (m *mockMetrics) RecordStateChange(_ string, from, to State) {
	m.mu.Lock()
	m.stateChanges++
	m.lastFrom = from
	m.lastTo = to
	m.mu.Unlock()
}

func (m *mockMetrics) RecordRejected(_ string, state State) {
	m.mu.Lock()
	m.rejected++
	m.lastRejState = state
	m.mu.Unlock()
}

func (m *mockMetrics) RecordExcluded(string, error) {
	m.mu.Lock()
	m.excluded++
	m.mu.Unlock()
}

func TestWithMetrics(t *testing.T) {
	t.Parallel()

	t.Run("records success", func(t *testing.T) {
		t.Parallel()
		m := &mockMetrics{}
		b := mustNewBreaker(t, WithName("m1"), WithMetrics(m))
		Run(b, context.Background(), func(ctx context.Context) (int, error) {
			return 1, nil
		})
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.successes != 1 {
			t.Fatalf("expected 1 success, got %d", m.successes)
		}
	})

	t.Run("records error", func(t *testing.T) {
		t.Parallel()
		m := &mockMetrics{}
		b := mustNewBreaker(t, WithName("m2"), WithMetrics(m))
		Run(b, context.Background(), func(ctx context.Context) (int, error) {
			return 0, errors.New("fail")
		})
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.errors != 1 {
			t.Fatalf("expected 1 error, got %d", m.errors)
		}
	})

	t.Run("records timeout", func(t *testing.T) {
		t.Parallel()
		m := &mockMetrics{}
		b := mustNewBreaker(t, WithName("m3"), WithMetrics(m), WithTimeout(10*time.Millisecond))
		Run(b, context.Background(), func(ctx context.Context) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		})
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.timeouts != 1 {
			t.Fatalf("expected 1 timeout, got %d", m.timeouts)
		}
		if m.errors != 1 {
			t.Fatalf("expected 1 error for timeout, got %d", m.errors)
		}
	})

	t.Run("records state change", func(t *testing.T) {
		t.Parallel()
		m := &mockMetrics{}
		b := mustNewBreaker(t, WithName("m4"), WithMetrics(m), WithLockOut(time.Second))
		b.tracker.incr()
		b.State() // closed -> open
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.stateChanges != 1 {
			t.Fatalf("expected 1 state change, got %d", m.stateChanges)
		}
		if m.lastFrom != Closed || m.lastTo != Open {
			t.Fatalf("expected closed->open, got %s->%s", m.lastFrom, m.lastTo)
		}
	})

	t.Run("records rejected on open", func(t *testing.T) {
		t.Parallel()
		m := &mockMetrics{}
		b := mustNewBreaker(t, WithName("m5"), WithMetrics(m), WithLockOut(time.Second))
		b.tracker.incr()
		b.State() // closed -> open
		// Try to run — should be rejected
		Run(b, context.Background(), func(ctx context.Context) (int, error) {
			return 0, nil
		})
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.rejected != 1 {
			t.Fatalf("expected 1 rejection, got %d", m.rejected)
		}
		if m.lastRejState != Open {
			t.Fatalf("expected rejection in Open state, got %s", m.lastRejState)
		}
	})

	t.Run("records rejected on throttled", func(t *testing.T) {
		t.Parallel()
		m := &mockMetrics{}
		b := mustNewBreaker(t, WithName("m6"), WithMetrics(m),
			WithBackOff(time.Second), WithWindow(50*time.Millisecond))
		b.tracker.incr()
		b.State() // closed -> open
		time.Sleep(60 * time.Millisecond)
		b.State() // open -> throttled

		// Force estimation to always block
		b.estimate = func(int) uint32 { return 100 }
		Run(b, context.Background(), func(ctx context.Context) (int, error) {
			return 0, nil
		})
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.rejected < 1 {
			t.Fatalf("expected at least 1 rejection in throttled state, got %d", m.rejected)
		}
	})

	t.Run("records excluded", func(t *testing.T) {
		t.Parallel()
		m := &mockMetrics{}
		b := mustNewBreaker(t, WithName("m7"), WithMetrics(m),
			WithIsExcluded(func(err error) bool { return true }))
		Run(b, context.Background(), func(ctx context.Context) (int, error) {
			return 0, errors.New("excluded")
		})
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.excluded != 1 {
			t.Fatalf("expected 1 excluded, got %d", m.excluded)
		}
	})

	t.Run("records success for isSuccessful errors", func(t *testing.T) {
		t.Parallel()
		m := &mockMetrics{}
		b := mustNewBreaker(t, WithName("m8"), WithMetrics(m),
			WithIsSuccessful(func(err error) bool { return true }))
		Run(b, context.Background(), func(ctx context.Context) (int, error) {
			return 0, errors.New("ok error")
		})
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.successes != 1 {
			t.Fatalf("expected 1 success for isSuccessful error, got %d", m.successes)
		}
	})
}

func TestWithEstimationFunc(t *testing.T) {
	t.Parallel()
	custom := func(tick int) uint32 { return 50 }
	b := mustNewBreaker(t, WithName("est"), WithEstimationFunc(custom))
	if b.estimate(1) != 50 {
		t.Fatalf("expected custom estimation to return 50, got %d", b.estimate(1))
	}
}

func TestWithOpeningResetsErrors(t *testing.T) {
	t.Parallel()
	b := mustNewBreaker(t, WithName("reset"),
		WithOpeningResetsErrors(true),
		WithThreshold(2),
		WithWindow(time.Second),
	)

	b.tracker.incr()
	b.tracker.incr()
	b.tracker.incr() // exceed threshold
	b.State()        // closed -> open, should reset errors

	if b.Size() != 0 {
		t.Fatalf("expected errors to be reset on open, got %d", b.Size())
	}
}
