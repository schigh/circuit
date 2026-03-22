package circuit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBreakerBox(t *testing.T) {
	t.Parallel()

	t.Run("NewBreakerBox", func(t *testing.T) {
		t.Parallel()
		bb := NewBreakerBox()
		if bb == nil {
			t.Fatal("expected non-nil BreakerBox")
		}
	})

	t.Run("Create", func(t *testing.T) {
		t.Parallel()

		t.Run("success", func(t *testing.T) {
			t.Parallel()
			bb := NewBreakerBox()
			b, err := bb.Create(WithName("test"))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if b == nil {
				t.Fatal("expected non-nil breaker")
			}
		})

		t.Run("unnamed breaker", func(t *testing.T) {
			t.Parallel()
			bb := NewBreakerBox()
			// NewBreaker auto-generates a name, so Create won't fail
			// unless we explicitly check. The auto-generated name is non-empty.
			b, err := bb.Create()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if b.name == "" {
				t.Fatal("expected auto-generated name")
			}
		})

		t.Run("replaces existing", func(t *testing.T) {
			t.Parallel()
			bb := NewBreakerBox()
			b1, _ := bb.Create(WithName("test"), WithThreshold(5))
			b2, _ := bb.Create(WithName("test"), WithThreshold(10))

			loaded := bb.Load("test")
			if loaded != b2 {
				t.Fatal("expected the second breaker to replace the first")
			}
			_ = b1
		})
	})

	t.Run("Load", func(t *testing.T) {
		t.Parallel()

		t.Run("existing", func(t *testing.T) {
			t.Parallel()
			bb := NewBreakerBox()
			bb.Create(WithName("test"))

			b := bb.Load("test")
			if b == nil {
				t.Fatal("expected to find breaker")
			}
			if b.name != "test" {
				t.Fatalf("expected name 'test', got '%s'", b.name)
			}
		})

		t.Run("missing", func(t *testing.T) {
			t.Parallel()
			bb := NewBreakerBox()
			if b := bb.Load("nonexistent"); b != nil {
				t.Fatal("expected nil for missing breaker")
			}
		})
	})

	t.Run("LoadOrCreate", func(t *testing.T) {
		t.Parallel()

		t.Run("creates new", func(t *testing.T) {
			t.Parallel()
			bb := NewBreakerBox()
			b, err := bb.LoadOrCreate("test", WithThreshold(5))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if b.threshold != 5 {
				t.Fatalf("expected threshold 5, got %d", b.threshold)
			}
		})

		t.Run("loads existing", func(t *testing.T) {
			t.Parallel()
			bb := NewBreakerBox()
			b1, _ := bb.Create(WithName("test"), WithThreshold(5))
			b2, _ := bb.LoadOrCreate("test", WithThreshold(10))

			if b1 != b2 {
				t.Fatal("expected same breaker instance")
			}
			if b2.threshold != 5 {
				t.Fatal("expected original threshold to be preserved")
			}
		})
	})

	t.Run("AddBYO", func(t *testing.T) {
		t.Parallel()

		t.Run("success", func(t *testing.T) {
			t.Parallel()
			bb := NewBreakerBox()
			b := mustNewBreaker(t, WithName("byo"))
			if err := bb.AddBYO(b); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if loaded := bb.Load("byo"); loaded != b {
				t.Fatal("expected to find the BYO breaker")
			}
		})

		t.Run("unnamed rejected", func(t *testing.T) {
			t.Parallel()
			bb := NewBreakerBox()
			b := &Breaker{} // no name
			if err := bb.AddBYO(b); !errors.Is(err, ErrUnnamedBreaker) {
				t.Fatalf("expected ErrUnnamedBreaker, got %v", err)
			}
		})
	})

	t.Run("state change forwarding", func(t *testing.T) {
		t.Parallel()
		bb := NewBreakerBox()
		b, _ := bb.Create(WithName("test"), WithWindow(50*time.Millisecond), WithLockOut(time.Second))

		// Drain the initial "closed" state from breaker's own channel
		select {
		case <-b.StateChange():
		default:
		}

		// Trigger a state change
		b.tracker.incr()
		b.State() // closed -> open

		// Should appear on the box's channel with full timing info
		select {
		case state := <-bb.StateChange():
			if state.State != Open {
				t.Fatalf("expected Open state, got %s", state.State)
			}
			if state.Name != "test" {
				t.Fatalf("expected name 'test', got '%s'", state.Name)
			}
			if state.Opened == nil {
				t.Fatal("box channel should receive full BreakerState with Opened timestamp")
			}
			if state.LockoutEnds == nil {
				t.Fatal("box channel should receive full BreakerState with LockoutEnds timestamp")
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for state change on box channel")
		}
	})

	t.Run("state change with user hook", func(t *testing.T) {
		t.Parallel()
		bb := NewBreakerBox()
		var hookCalled bool
		b, _ := bb.Create(
			WithName("test"),
			WithWindow(50*time.Millisecond),
			WithOnStateChange(func(name string, from, to State) {
				hookCalled = true
			}),
		)

		b.tracker.incr()
		b.State()

		if !hookCalled {
			t.Fatal("user's OnStateChange hook should have been called")
		}

		// Box channel should also get the event
		select {
		case <-bb.StateChange():
		case <-time.After(100 * time.Millisecond):
			t.Fatal("box channel should also receive state change")
		}
	})
}

func TestConcurrentRun(t *testing.T) {
	t.Parallel()

	t.Run("parallel success", func(t *testing.T) {
		t.Parallel()
		b := mustNewBreaker(t, WithName("concurrent"))

		errs := make(chan error, 100)
		for i := 0; i < 100; i++ {
			go func() {
				_, err := Run(b, context.Background(), func(ctx context.Context) (int, error) {
					return 42, nil
				})
				errs <- err
			}()
		}

		for i := 0; i < 100; i++ {
			if err := <-errs; err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	})

	t.Run("parallel errors trigger open", func(t *testing.T) {
		t.Parallel()
		b := mustNewBreaker(t, WithName("concurrent-err"), WithThreshold(5), WithLockOut(time.Second))

		boom := errors.New("boom")
		errs := make(chan error, 20)
		for i := 0; i < 20; i++ {
			go func() {
				_, err := Run(b, context.Background(), func(ctx context.Context) (int, error) {
					return 0, boom
				})
				errs <- err
			}()
		}

		for i := 0; i < 20; i++ {
			<-errs
		}

		// After enough errors, breaker should be open
		state := b.State()
		if state != Open {
			t.Fatalf("expected Open after many errors, got %s", state)
		}
	})

	t.Run("parallel Allow", func(t *testing.T) {
		t.Parallel()
		b := mustNewBreaker(t, WithName("concurrent-allow"))

		errs := make(chan error, 50)
		for i := 0; i < 50; i++ {
			go func() {
				done, err := b.Allow(context.Background())
				if err != nil {
					errs <- err
					return
				}
				done(nil)
				errs <- nil
			}()
		}

		for i := 0; i < 50; i++ {
			if err := <-errs; err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	})
}
