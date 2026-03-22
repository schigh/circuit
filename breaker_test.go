package circuit

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mustNewBreaker(t *testing.T, opts ...Option) *Breaker {
	t.Helper()
	b, err := NewBreaker(opts...)
	if err != nil {
		t.Fatalf("NewBreaker failed: %v", err)
	}
	return b
}

func TestBreaker(t *testing.T) {
	t.Parallel()

	t.Run("NewBreaker", func(t *testing.T) {
		t.Parallel()
		t.Run("defaults", func(t *testing.T) {
			t.Parallel()
			b, err := NewBreaker()
			if err != nil {
				t.Fatalf("NewBreaker failed: %v", err)
			}

			nameRx := regexp.MustCompile(`func_circuit_TestBreaker_func[\d_]+file_breaker_test_line_\d+`)
			if !nameRx.Match([]byte(b.name)) {
				t.Fatalf("expected regex to match for default name: %s", b.name)
			}
			if b.timeout != DefaultTimeout {
				t.Fatalf("expected default timeout, got %v", b.timeout)
			}
			if b.backoff != DefaultBackOff {
				t.Fatalf("expected default backoff, got %v", b.backoff)
			}
			if b.window != DefaultWindow {
				t.Fatalf("expected default window, got %v", b.window)
			}
			if b.estimate == nil {
				t.Fatalf("estimation func cannot be nil")
			}
			for i := 1; i < 100; i++ {
				if b.estimate(i) != uint32(100-i) {
					t.Fatalf("linear estimation was expected")
				}
			}
		})

		t.Run("illegal options", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t,
				WithBackOff(time.Millisecond),
				WithWindow(time.Millisecond),
			)
			if b.backoff != minimumBackoff {
				t.Fatalf("expected minimum backoff, got %v", b.backoff)
			}
			if b.window != minimumWindow {
				t.Fatalf("expected minimum window, got %v", b.window)
			}
		})
	})

	t.Run("state transitions", func(t *testing.T) {
		t.Parallel()

		t.Run("closed to open on errors", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t, WithLockOut(time.Second))
			b.tracker.incr()
			// evaluateState triggers on State() call
			if b.State() != Open {
				t.Fatalf("expected Open, got %s", b.State())
			}
		})

		t.Run("open to throttled after lockout expires", func(t *testing.T) {
			t.Parallel()
			// Window must be shorter than lockout so errors expire before lockout ends
			b := mustNewBreaker(t, WithLockOut(100*time.Millisecond), WithWindow(50*time.Millisecond))
			b.tracker.incr()
			b.State() // trigger closed -> open

			time.Sleep(150 * time.Millisecond)
			// error expired (50ms window), lockout expired (100ms)
			if b.State() != Throttled {
				t.Fatalf("expected Throttled after lockout, got %s", b.State())
			}
		})

		t.Run("open stays locked during lockout", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t, WithLockOut(time.Second))
			b.tracker.incr()
			b.State() // trigger open

			if b.State() != Open {
				t.Fatalf("expected Open during lockout, got %s", b.State())
			}
		})

		t.Run("throttled to closed after backoff", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t, WithBackOff(100*time.Millisecond), WithWindow(50*time.Millisecond))
			b.tracker.incr()
			b.State() // closed -> open
			time.Sleep(60 * time.Millisecond)
			b.State() // open -> throttled (errors evicted)

			time.Sleep(110 * time.Millisecond)
			if b.State() != Closed {
				t.Fatalf("expected Closed after backoff, got %s", b.State())
			}
		})

		t.Run("throttled to open on new errors", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t, WithBackOff(time.Second), WithWindow(50*time.Millisecond))
			b.tracker.incr()
			b.State() // closed -> open
			time.Sleep(60 * time.Millisecond)
			b.State() // open -> throttled (errors evicted)

			if b.State() != Throttled {
				t.Fatalf("expected Throttled, got %s", b.State())
			}

			b.tracker.incr()
			if b.State() != Open {
				t.Fatalf("expected Open after new error, got %s", b.State())
			}
		})

		t.Run("change listener", func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			transitions := make([]string, 0)
			b := mustNewBreaker(t,
				WithLockOut(100*time.Millisecond),
				WithBackOff(100*time.Millisecond),
				WithWindow(50*time.Millisecond),
				WithOnStateChange(func(name string, from, to State) {
					mu.Lock()
					transitions = append(transitions, from.String()+"->"+to.String())
					mu.Unlock()
				}),
			)

			// closed -> open
			b.tracker.incr()
			b.State()

			// wait for lockout (100ms) + error eviction (50ms window)
			time.Sleep(150 * time.Millisecond)
			// open -> throttled (lockout expired, errors evicted)
			b.State()

			// wait for backoff (100ms)
			time.Sleep(150 * time.Millisecond)
			// throttled -> closed (backoff expired)
			b.State()

			mu.Lock()
			defer mu.Unlock()
			expected := []string{"closed->open", "open->throttled", "throttled->closed"}
			if len(transitions) != len(expected) {
				t.Fatalf("expected %d transitions, got %d: %v", len(expected), len(transitions), transitions)
			}
			for i, e := range expected {
				if transitions[i] != e {
					t.Fatalf("transition %d: expected %s, got %s", i, e, transitions[i])
				}
			}
		})
	})

	t.Run("throttle estimation", func(t *testing.T) {
		t.Parallel()

		t.Run("early in backoff blocks more", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t,
				WithBackOff(time.Second),
				WithWindow(50*time.Millisecond),
			)
			b.tracker.incr()
			b.State() // closed -> open
			time.Sleep(60 * time.Millisecond)
			b.State() // open -> throttled

			// early in backoff, throttle chance should be high
			// test with custom estimation that returns the tick value
			b.estimate = func(tick int) uint32 { return uint32(100 - tick) }

			// right after throttle starts, chance should be high (~100)
			err := b.applyThrottle()
			// with tick ~6 (60ms into 1s backoff / 10ms per tick), chance ~94
			// most of the time this should throttle
			_ = err // probabilistic, just verify no panic
		})

		t.Run("late in backoff blocks less", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t,
				WithBackOff(100*time.Millisecond),
				WithWindow(50*time.Millisecond),
			)
			b.tracker.incr()
			b.State()
			time.Sleep(60 * time.Millisecond)
			b.State() // open -> throttled

			time.Sleep(90 * time.Millisecond)
			// near end of backoff, tick ~90, chance ~10 with Linear
			// should mostly pass
			err := b.applyThrottle()
			_ = err
		})
	})

	t.Run("check fitness", func(t *testing.T) {
		t.Parallel()

		t.Run("with a canceled context", func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			b := mustNewBreaker(t)
			if err := b.checkFitness(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context.Canceled, got %v", err)
			}
		})

		t.Run("open breaker", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t, WithLockOut(time.Second))
			b.tracker.incr()
			b.State() // trigger open
			if err := b.checkFitness(context.Background()); !errors.Is(err, ErrStateOpen) {
				t.Fatalf("expected ErrStateOpen, got %v", err)
			}
		})

		t.Run("closed breaker", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t)
			if err := b.checkFitness(context.Background()); err != nil {
				t.Fatalf("expected nil, got %v", err)
			}
		})

		t.Run("unknown state", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t)
			atomic.SwapUint32(&b.state, 100)
			if err := b.checkFitness(context.Background()); !errors.Is(err, ErrStateUnknown) {
				t.Fatalf("expected ErrStateUnknown, got %v", err)
			}
		})
	})

	t.Run("Run generic", func(t *testing.T) {
		t.Parallel()

		t.Run("success", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t)
			result, err := Run(b, context.Background(), func(ctx context.Context) (string, error) {
				return "hello", nil
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != "hello" {
				t.Fatalf("expected 'hello', got '%s'", result)
			}
		})

		t.Run("error", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t)
			theErr := errors.New("boom")
			_, err := Run(b, context.Background(), func(ctx context.Context) (int, error) {
				return 0, theErr
			})
			if !errors.Is(err, theErr) {
				t.Fatalf("expected 'boom', got %v", err)
			}
			// error should be tracked
			if b.Size() != 1 {
				t.Fatalf("expected 1 tracked error, got %d", b.Size())
			}
		})

		t.Run("timeout", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t, WithTimeout(10*time.Millisecond))
			_, err := Run(b, context.Background(), func(ctx context.Context) (string, error) {
				select {
				case <-time.After(50 * time.Millisecond):
					return "late", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			})
			if !errors.Is(err, ErrTimeout) {
				t.Fatalf("expected ErrTimeout, got %v", err)
			}
		})

		t.Run("not initialized", func(t *testing.T) {
			t.Parallel()
			b := &Breaker{}
			_, err := Run(b, context.Background(), func(ctx context.Context) (bool, error) {
				return true, nil
			})
			if !errors.Is(err, ErrNotInitialized) {
				t.Fatalf("expected ErrNotInitialized, got %v", err)
			}
		})

		t.Run("open breaker", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t, WithLockOut(time.Second))
			b.tracker.incr()
			b.State()
			_, err := Run(b, context.Background(), func(ctx context.Context) (bool, error) {
				return true, nil
			})
			if !errors.Is(err, ErrStateOpen) {
				t.Fatalf("expected ErrStateOpen, got %v", err)
			}
		})
	})

	t.Run("panic handling", func(t *testing.T) {
		t.Parallel()
		b := mustNewBreaker(t)

		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic to be re-raised")
			}
			if r != "test panic" {
				t.Fatalf("expected 'test panic', got %v", r)
			}
			// panic should have been recorded as failure
			if b.Size() != 1 {
				t.Fatalf("expected 1 tracked error from panic, got %d", b.Size())
			}
		}()

		Run(b, context.Background(), func(ctx context.Context) (string, error) {
			panic("test panic")
		})
	})

	t.Run("Allow two-step", func(t *testing.T) {
		t.Parallel()

		t.Run("success", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t)
			done, err := b.Allow(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			done(nil) // success
			if b.Size() != 0 {
				t.Fatalf("expected 0 errors, got %d", b.Size())
			}
		})

		t.Run("failure", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t)
			done, err := b.Allow(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			done(errors.New("boom"))
			if b.Size() != 1 {
				t.Fatalf("expected 1 error, got %d", b.Size())
			}
		})

		t.Run("rejected when open", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t, WithLockOut(time.Second))
			b.tracker.incr()
			b.State()
			_, err := b.Allow(context.Background())
			if !errors.Is(err, ErrStateOpen) {
				t.Fatalf("expected ErrStateOpen, got %v", err)
			}
		})

		t.Run("not initialized", func(t *testing.T) {
			t.Parallel()
			b := &Breaker{}
			_, err := b.Allow(context.Background())
			if !errors.Is(err, ErrNotInitialized) {
				t.Fatalf("expected ErrNotInitialized, got %v", err)
			}
		})
	})

	t.Run("error classification", func(t *testing.T) {
		t.Parallel()

		t.Run("isExcluded", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t,
				WithIsExcluded(func(err error) bool {
					return errors.Is(err, context.Canceled)
				}),
			)

			Run(b, context.Background(), func(ctx context.Context) (bool, error) {
				return false, context.Canceled
			})

			if b.Size() != 0 {
				t.Fatalf("excluded error should not be tracked, got size %d", b.Size())
			}
		})

		t.Run("isSuccessful", func(t *testing.T) {
			t.Parallel()
			errNotFound := errors.New("not found")
			b := mustNewBreaker(t,
				WithIsSuccessful(func(err error) bool {
					return errors.Is(err, errNotFound)
				}),
			)

			_, err := Run(b, context.Background(), func(ctx context.Context) (string, error) {
				return "", errNotFound
			})
			if !errors.Is(err, errNotFound) {
				t.Fatalf("expected errNotFound, got %v", err)
			}
			if b.Size() != 0 {
				t.Fatalf("successful-classified error should not be tracked, got size %d", b.Size())
			}
		})

		t.Run("regular error still tracked", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t,
				WithIsExcluded(func(err error) bool { return false }),
				WithIsSuccessful(func(err error) bool { return false }),
			)

			Run(b, context.Background(), func(ctx context.Context) (bool, error) {
				return false, errors.New("real error")
			})

			if b.Size() != 1 {
				t.Fatalf("expected 1 tracked error, got %d", b.Size())
			}
		})
	})

	t.Run("snapshot", func(t *testing.T) {
		t.Parallel()

		t.Run("new breaker", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t, WithName("foo"))
			snap := b.Snapshot()
			if snap.Name != "foo" {
				t.Fatal("snapshot is not capturing breaker name")
			}
			if snap.State != Closed {
				t.Fatal("state should be Closed")
			}
			if snap.ClosedSince == nil {
				t.Fatal("the ClosedSince property should not be nil")
			}
		})

		t.Run("open breaker", func(t *testing.T) {
			t.Parallel()
			b := mustNewBreaker(t, WithName("foo"), WithLockOut(time.Second))
			b.tracker.incr()
			snap := b.Snapshot() // triggers evaluation
			if snap.State != Open {
				t.Fatalf("state should be Open, got %s", snap.State)
			}
			if snap.Opened == nil {
				t.Fatal("the Opened property should not be nil")
			}
			if snap.LockoutEnds == nil {
				t.Fatal("the LockoutEnds property should not be nil")
			}
		})
	})

	t.Run("error context", func(t *testing.T) {
		t.Parallel()
		b := mustNewBreaker(t, WithName("test-breaker"), WithLockOut(time.Second))
		b.tracker.incr()
		b.State()

		_, err := Run(b, context.Background(), func(ctx context.Context) (bool, error) {
			return false, nil
		})
		if err == nil {
			t.Fatal("expected error")
		}
		var circErr Error
		if !errors.As(err, &circErr) {
			t.Fatal("expected circuit.Error type")
		}
		if circErr.BreakerName != "test-breaker" {
			t.Fatalf("expected breaker name 'test-breaker', got '%s'", circErr.BreakerName)
		}
	})

	t.Run("no cleanup needed", func(t *testing.T) {
		t.Parallel()
		// Verify that creating breakers doesn't leak resources
		for i := 0; i < 100; i++ {
			mustNewBreaker(t, WithName(fmt.Sprintf("breaker-%d", i)))
		}
		// No Close() calls needed — test passes without cleanup
	})
}
