package circuit

import (
	"errors"
	"fmt"
	"testing"
)

func TestError(t *testing.T) {
	t.Parallel()

	t.Run("Error method", func(t *testing.T) {
		t.Parallel()

		t.Run("without context", func(t *testing.T) {
			t.Parallel()
			e := Error{msg: "something broke"}
			if got := e.Error(); got != "something broke" {
				t.Fatalf("got %q, want %q", got, "something broke")
			}
		})

		t.Run("with breaker name", func(t *testing.T) {
			t.Parallel()
			e := Error{msg: "something broke", BreakerName: "my-breaker"}
			want := "something broke [breaker=my-breaker]"
			if got := e.Error(); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	})

	t.Run("Is method", func(t *testing.T) {
		t.Parallel()

		t.Run("matching error", func(t *testing.T) {
			t.Parallel()
			e := ErrStateOpen.withContext("test", Open)
			if !errors.Is(e, ErrStateOpen) {
				t.Fatal("expected Is to match ErrStateOpen")
			}
		})

		t.Run("non-matching circuit error", func(t *testing.T) {
			t.Parallel()
			e := ErrStateOpen.withContext("test", Open)
			if errors.Is(e, ErrTimeout) {
				t.Fatal("expected Is to not match ErrTimeout")
			}
		})

		t.Run("non-Error type", func(t *testing.T) {
			t.Parallel()
			e := ErrStateOpen
			if errors.Is(e, fmt.Errorf("random error")) {
				t.Fatal("expected Is to return false for non-Error type")
			}
		})
	})

	t.Run("withContext", func(t *testing.T) {
		t.Parallel()
		e := ErrStateOpen.withContext("breaker-1", Throttled)
		if e.BreakerName != "breaker-1" {
			t.Fatalf("expected BreakerName 'breaker-1', got %q", e.BreakerName)
		}
		if e.State != Throttled {
			t.Fatalf("expected state Throttled, got %s", e.State)
		}
	})

	t.Run("sentinel errors", func(t *testing.T) {
		t.Parallel()
		// Verify all sentinel errors produce non-empty messages
		sentinels := []Error{
			ErrNotInitialized,
			ErrTimeout,
			ErrStateUnknown,
			ErrStateOpen,
			ErrStateThrottled,
			ErrUnnamedBreaker,
		}
		for _, s := range sentinels {
			if s.Error() == "" {
				t.Fatalf("sentinel error has empty message: %+v", s)
			}
		}
	})
}
