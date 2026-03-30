package circuit

import "testing"

func TestAlgorithms(t *testing.T) {
	t.Parallel()

	t.Run("Linear", func(t *testing.T) {
		t.Parallel()
		if v := Linear(1); v != 99 {
			t.Fatalf("Linear(1) = %d, want 99", v)
		}
		if v := Linear(50); v != 50 {
			t.Fatalf("Linear(50) = %d, want 50", v)
		}
		if v := Linear(100); v != 0 {
			t.Fatalf("Linear(100) = %d, want 0", v)
		}
	})

	t.Run("Logarithmic", func(t *testing.T) {
		t.Parallel()
		// tick 1 should be highest (100)
		if v := Logarithmic(1); v != 100 {
			t.Fatalf("Logarithmic(1) = %d, want 100", v)
		}
		// tick 100 should be 0
		if v := Logarithmic(100); v != 0 {
			t.Fatalf("Logarithmic(100) = %d, want 0", v)
		}
		// monotonically non-increasing
		for i := 2; i <= 100; i++ {
			if Logarithmic(i) > Logarithmic(i-1) {
				t.Fatalf("Logarithmic is not monotonically non-increasing at tick %d", i)
			}
		}
	})

	t.Run("Exponential", func(t *testing.T) {
		t.Parallel()
		if v := Exponential(1); v != 100 {
			t.Fatalf("Exponential(1) = %d, want 100", v)
		}
		if v := Exponential(100); v != 0 {
			t.Fatalf("Exponential(100) = %d, want 0", v)
		}
		for i := 2; i <= 100; i++ {
			if Exponential(i) > Exponential(i-1) {
				t.Fatalf("Exponential is not monotonically non-increasing at tick %d", i)
			}
		}
	})

	t.Run("EaseInOut", func(t *testing.T) {
		t.Parallel()
		if v := EaseInOut(1); v != 100 {
			t.Fatalf("EaseInOut(1) = %d, want 100", v)
		}
		if v := EaseInOut(100); v != 0 {
			t.Fatalf("EaseInOut(100) = %d, want 0", v)
		}
		for i := 2; i <= 100; i++ {
			if EaseInOut(i) > EaseInOut(i-1) {
				t.Fatalf("EaseInOut is not monotonically non-increasing at tick %d", i)
			}
		}
	})

	t.Run("JitteredLinear", func(t *testing.T) {
		t.Parallel()
		// Should always be in [0, 100]
		for tick := 1; tick <= 100; tick++ {
			v := JitteredLinear(tick)
			if v > 100 {
				t.Fatalf("JitteredLinear(%d) = %d, exceeds 100", tick, v)
			}
		}
		// At tick 1, base=99, jitter in [-5,5], so result in [94, 100]
		for range 100 {
			v := JitteredLinear(1)
			if v < 94 || v > 100 {
				t.Fatalf("JitteredLinear(1) = %d, expected [94, 100]", v)
			}
		}
		// At tick 100, base=0, jitter in [-5,5], clamped to [0, 5]
		for range 100 {
			v := JitteredLinear(100)
			if v > 5 {
				t.Fatalf("JitteredLinear(100) = %d, expected [0, 5]", v)
			}
		}
	})
}
