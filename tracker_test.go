package circuit

import (
	"testing"
	"time"
)

func newTracker(t *testing.T, dur time.Duration) *errTracker {
	t.Helper()
	if dur == 0 {
		dur = time.Minute
	}
	return newErrTracker(dur)
}

func TestTracker(t *testing.T) {
	t.Run("size tests", func(t *testing.T) {
		t.Parallel()
		t.Run("empty tracker should be size zero", func(t *testing.T) {
			t.Parallel()
			tracker := newTracker(t, 0)
			if size := tracker.size(); size != 0 {
				t.Errorf("size(): expected 0, got %d", size)
			}
		})
		t.Run("tracker should have one item after increment", func(t *testing.T) {
			t.Parallel()
			tracker := newTracker(t, 0)
			tracker.incr()
			if size := tracker.size(); size != 1 {
				t.Errorf("size(): expected 1, got %d", size)
			}
		})
		t.Run("calling incr 20 times", func(t *testing.T) {
			t.Parallel()
			tracker := newTracker(t, 0)
			for i := 0; i < 20; i++ {
				tracker.incr()
			}
			if size := tracker.size(); size != 20 {
				t.Errorf("size(): expected 20, got %d", size)
			}
		})
		t.Run("calling incr 1000 times", func(t *testing.T) {
			t.Parallel()
			tracker := newTracker(t, 0)
			for i := 0; i < 1000; i++ {
				tracker.incr()
			}
			if size := tracker.size(); size != 1000 {
				t.Errorf("size(): expected 1000, got %d", size)
			}
		})
	})

	t.Run("eviction tests", func(t *testing.T) {
		t.Parallel()

		t.Run("entries outside window are evicted on size()", func(t *testing.T) {
			t.Parallel()
			tracker := newTracker(t, 500*time.Millisecond)
			tracker.incr()
			tracker.incr()
			tracker.incr()
			if size := tracker.size(); size != 3 {
				t.Fatalf("expected 3, got %d", size)
			}

			time.Sleep(600 * time.Millisecond)
			if size := tracker.size(); size != 0 {
				t.Fatalf("expected 0 after window expiry, got %d", size)
			}
		})

		t.Run("mixed age entries", func(t *testing.T) {
			t.Parallel()
			tracker := newTracker(t, 500*time.Millisecond)
			tracker.incr()
			tracker.incr()
			time.Sleep(300 * time.Millisecond)
			tracker.incr()
			tracker.incr()

			if size := tracker.size(); size != 4 {
				t.Fatalf("expected 4, got %d", size)
			}

			time.Sleep(300 * time.Millisecond)
			// first 2 should be evicted, last 2 should remain
			size := tracker.size()
			if size != 2 {
				t.Fatalf("expected 2 after partial eviction, got %d", size)
			}
		})

		t.Run("different windows", func(t *testing.T) {
			t.Parallel()
			short := newTracker(t, time.Second)
			long := newTracker(t, 10*time.Second)

			for i := 0; i < 10; i++ {
				time.Sleep(200 * time.Millisecond)
				short.incr()
				long.incr()
			}

			// short window should have lost early entries
			shortSize := short.size()
			longSize := long.size()

			if longSize != 10 {
				t.Fatalf("long tracker: expected 10, got %d", longSize)
			}
			if shortSize >= 10 {
				t.Fatalf("short tracker should have fewer than 10 entries, got %d", shortSize)
			}
		})
	})

	t.Run("reset", func(t *testing.T) {
		t.Parallel()
		tracker := newTracker(t, 10*time.Second)
		tracker.incr()
		tracker.incr()
		tracker.incr()
		if sz := tracker.size(); sz != 3 {
			t.Fatalf("expected size 3, got %d", sz)
		}

		tracker.reset(false)
		if sz := tracker.size(); sz != 3 {
			t.Fatalf("expected size 3 after reset(false), got %d", sz)
		}

		tracker.reset(true)
		if sz := tracker.size(); sz != 0 {
			t.Fatalf("expected size 0 after reset(true), got %d", sz)
		}
	})
}
