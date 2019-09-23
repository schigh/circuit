package circuit

import (
	"math"
	"sync"
	"testing"
	"time"
)

func newTracker(t *testing.T, dur, tick time.Duration) errTracker {
	t.Helper()

	if dur == 0 {
		// set it to a high value so the evictions dont trigger
		dur = time.Minute
	}
	if tick == 0 {
		// set it to a high value so the calcTicker doesnt fire
		tick = time.Minute
	}

	var sz uint32
	e := errTracker{mx: &sync.Mutex{}, sz: &sz}
	e.events = make(map[int64]uint32)
	e.window = int64(dur)
	e.pipe = make(chan struct{})

	e.clock = time.NewTicker(tick)
	e.poll()

	return e
}

func TestTracker(t *testing.T) {
	t.Run("size tests", func(t *testing.T) {
		t.Parallel()
		t.Run("empty tracker should be size zero", func(t *testing.T) {
			t.Parallel()
			var tracker = newTracker(t, 0, 0)
			size := tracker.size()
			if size != 0 {
				t.Errorf("size(): expected 0, got %d", size)
			}
		})
		t.Run("tracker should have one item after increment", func(t *testing.T) {
			t.Parallel()
			var tracker = newTracker(t, 0, 0)
			tracker.incr()
			// let goroutines get all caught up
			time.Sleep(10 * time.Millisecond)
			size := tracker.size()
			if size != 1 {
				t.Errorf("size(): expected 1, got %d", size)
			}
		})
		t.Run("calling incr 20 times concurrently", func(t *testing.T) {
			t.Parallel()
			var tracker = newTracker(t, 0, 0)
			for i := 0; i < 20; i++ {
				tracker.incr()
			}
			// let goroutines get all caught up
			time.Sleep(10 * time.Millisecond)
			size := tracker.size()
			if size != 20 {
				t.Errorf("size(): expected 20, got %d", size)
			}
		})
		t.Run("calling incr 1000 times concurrently", func(t *testing.T) {
			t.Parallel()
			var tracker = newTracker(t, 0, 0)
			for i := 0; i < 1000; i++ {
				tracker.incr()
			}
			// let goroutines get all caught up
			time.Sleep(10 * time.Millisecond)
			size := tracker.size()
			if size != 1000 {
				t.Errorf("size(): expected 1000, got %d", size)
			}
		})
	})

	t.Run("eviction tests", func(t *testing.T) {
		t.Parallel()
		const chanSize = 111

		initializer := func(t *testing.T) (chan int, chan int, chan int, errTracker, errTracker, errTracker) {
			t.Helper()
			return make(chan int, chanSize),
				make(chan int, chanSize),
				make(chan int, chanSize),
				newTracker(t, 10*time.Second, 10*time.Millisecond),
				newTracker(t, 5*time.Second, 10*time.Millisecond),
				newTracker(t, time.Second, 10*time.Millisecond)
		}

		getSizes := func(t *testing.T, name string, c chan int) []int {
			t.Helper()
			i := 0
			ret := make([]int, 0, chanSize)
			for size := range c {
				if size == -1 {
					break
				}
				ret = append(ret, size)
				i++
			}

			return ret
		}

		relativelyEqual := func(t *testing.T, i1, i2 []int) bool {
			t.Helper()
			l1 := len(i1)
			l2 := len(i2)
			if l1 != l2 {
				return false
			}
			for i := 0; i < l2; i++ {
				offset := int(math.Abs(float64(i1[i] - i2[i])))
				switch offset {
				case 0, 1, 2:
					continue
				default:
					return false
				}
			}

			return true
		}

		t.Run("with front pressure", func(t *testing.T) {
			t.Parallel()

			c1, c2, c3, t1, t2, t3 := initializer(t)
			wg := &sync.WaitGroup{}
			wg.Add(2)

			go func() {
				for i := 0; i < 10; i++ {
					time.Sleep(time.Duration(i*100) * time.Millisecond)
					t1.incr()
					t2.incr()
					t3.incr()
				}
				wg.Done()
			}()

			go func() {
				for i := 0; i < 100; i++ {
					time.Sleep(100 * time.Millisecond)
					c1 <- int(t1.size())
					c2 <- int(t2.size())
					c3 <- int(t3.size())
				}
				wg.Done()
			}()

			wg.Wait()
			c1 <- -1
			c2 <- -1
			c3 <- -1

			s1 := getSizes(t, "t1", c1)
			s2 := getSizes(t, "t2", c2)
			s3 := getSizes(t, "t3", c3)

			expected1 := []int{1, 2, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 5, 6, 6, 6, 6, 6, 6, 7, 7, 7, 7, 7, 7, 7, 8, 8, 8, 8, 8, 8, 8, 8, 9, 9, 9, 9, 9, 9, 9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 9, 8, 8}
			expected2 := []int{1, 2, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 5, 6, 6, 6, 6, 6, 6, 7, 7, 7, 7, 7, 7, 7, 8, 8, 8, 8, 8, 8, 8, 8, 9, 9, 9, 9, 9, 9, 9, 9, 9, 10, 10, 10, 10, 9, 8, 8, 7, 7, 7, 6, 6, 6, 6, 5, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 3, 3, 3, 3, 3, 3, 3, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0}
			expected3 := []int{1, 2, 3, 3, 3, 4, 4, 4, 4, 4, 3, 3, 2, 2, 3, 2, 2, 2, 2, 1, 2, 2, 2, 2, 1, 1, 1, 2, 2, 2, 1, 1, 1, 1, 1, 2, 2, 1, 1, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

			if !relativelyEqual(t, s1, expected1) {
				t.Errorf("t1 failure:\nexpected: %#v\n     got: %#v\n", expected1, s1)
			}
			if !relativelyEqual(t, s2, expected2) {
				t.Errorf("t2 failure:\nexpected: %#v\n     got: %#v\n", expected2, s2)
			}
			if !relativelyEqual(t, s3, expected3) {
				t.Errorf("t3 failure:\nexpected: %#v\n     got: %#v\n", expected3, s3)
			}
		})

		t.Run("with back pressure", func(t *testing.T) {
			t.Parallel()

			c1, c2, c3, t1, t2, t3 := initializer(t)
			wg := &sync.WaitGroup{}
			wg.Add(2)

			go func() {
				for i := 0; i < 10; i++ {
					time.Sleep(time.Duration((9-i)*100) * time.Millisecond)
					t1.incr()
					t2.incr()
					t3.incr()
				}
				wg.Done()
			}()

			go func() {
				for i := 0; i < 100; i++ {
					time.Sleep(100 * time.Millisecond)
					c1 <- int(t1.size())
					c2 <- int(t2.size())
					c3 <- int(t3.size())
				}
				wg.Done()
			}()

			wg.Wait()
			c1 <- -1
			c2 <- -1
			c3 <- -1

			s1 := getSizes(t, "t1", c1)
			s2 := getSizes(t, "t2", c2)
			s3 := getSizes(t, "t3", c3)

			expected1 := []int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 7, 7, 8, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10}
			expected2 := []int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 3, 3, 3, 3, 3, 3, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 7, 7, 8, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 9, 9, 9, 9, 9, 9, 9, 9, 8, 8, 8, 8, 8, 8, 8, 7, 7, 7, 7, 7, 7, 6, 6, 6, 6, 6, 5, 5, 5, 5, 4, 4, 4, 3, 3, 2, 0, 0, 0, 0, 0, 0, 0}
			expected3 := []int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 1, 1, 1, 1, 1, 2, 2, 2, 1, 1, 1, 2, 2, 2, 2, 1, 2, 2, 2, 2, 3, 2, 2, 3, 3, 4, 5, 5, 5, 5, 4, 4, 4, 3, 3, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

			if !relativelyEqual(t, s1, expected1) {
				t.Errorf("t1 failure:\nexpected: %#v\n     got: %#v\n", expected1, s1)
			}
			if !relativelyEqual(t, s2, expected2) {
				t.Errorf("t2 failure:\nexpected: %#v\n     got: %#v\n", expected2, s2)
			}
			if !relativelyEqual(t, s3, expected3) {
				t.Errorf("t3 failure:\nexpected: %#v\n     got: %#v\n", expected3, s3)
			}
		})

		t.Run("with middle pressure", func(t *testing.T) {
			t.Parallel()

			c1, c2, c3, t1, t2, t3 := initializer(t)
			wg := &sync.WaitGroup{}
			wg.Add(2)

			go func() {
				for i := 0; i < 10; i++ {
					p := math.Abs(float64(5 - i))
					time.Sleep(time.Duration((p)*180) * time.Millisecond)
					t1.incr()
					t2.incr()
					t3.incr()
				}
				wg.Done()
			}()

			go func() {
				for i := 0; i < 100; i++ {
					time.Sleep(100 * time.Millisecond)
					c1 <- int(t1.size())
					c2 <- int(t2.size())
					c3 <- int(t3.size())
				}
				wg.Done()
			}()

			wg.Wait()
			c1 <- -1
			c2 <- -1
			c3 <- -1

			s1 := getSizes(t, "t1", c1)
			s2 := getSizes(t, "t2", c2)
			s3 := getSizes(t, "t3", c3)

			expected1 := []int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4, 6, 6, 7, 7, 7, 7, 8, 8, 8, 8, 8, 9, 9, 9, 9, 9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10}
			expected2 := []int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4, 6, 6, 7, 7, 7, 7, 8, 8, 8, 8, 8, 9, 9, 9, 9, 9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 9, 9, 9, 9, 9, 9, 9, 8, 8, 8, 8, 8, 8, 7, 7, 7, 6, 6, 4, 4, 3, 3, 3, 3, 2, 2, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0}
			expected3 := []int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 1, 1, 1, 2, 2, 2, 3, 2, 4, 4, 5, 5, 5, 4, 5, 5, 4, 4, 2, 3, 2, 2, 2, 1, 1, 1, 2, 2, 2, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

			if !relativelyEqual(t, s1, expected1) {
				t.Errorf("t1 failure:\nexpected: %#v\n     got: %#v\n", expected1, s1)
			}
			if !relativelyEqual(t, s2, expected2) {
				t.Errorf("t2 failure:\nexpected: %#v\n     got: %#v\n", expected2, s2)
			}
			if !relativelyEqual(t, s3, expected3) {
				t.Errorf("t3 failure:\nexpected: %#v\n     got: %#v\n", expected3, s3)
			}
		})

		t.Run("with uniform distribution", func(t *testing.T) {
			t.Parallel()

			c1, c2, c3, t1, t2, t3 := initializer(t)
			wg := &sync.WaitGroup{}
			wg.Add(2)

			go func() {
				for i := 0; i < 10; i++ {
					time.Sleep(450 * time.Millisecond)
					t1.incr()
					t2.incr()
					t3.incr()
				}
				wg.Done()
			}()

			go func() {
				for i := 0; i < 100; i++ {
					time.Sleep(100 * time.Millisecond)
					c1 <- int(t1.size())
					c2 <- int(t2.size())
					c3 <- int(t3.size())
				}
				wg.Done()
			}()

			wg.Wait()
			c1 <- -1
			c2 <- -1
			c3 <- -1

			s1 := getSizes(t, "t1", c1)
			s2 := getSizes(t, "t2", c2)
			s3 := getSizes(t, "t3", c3)

			expected1 := []int{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8, 8, 9, 9, 9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10}
			expected2 := []int{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8, 8, 9, 9, 9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 9, 9, 9, 9, 8, 8, 8, 8, 8, 7, 7, 7, 7, 6, 6, 6, 6, 6, 5, 5, 5, 5, 4, 4, 4, 4, 4, 3, 3, 3, 3, 2, 2, 2, 2, 2, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0}
			expected3 := []int{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 2, 3, 2, 2, 2, 3, 2, 2, 2, 2, 3, 2, 2, 2, 3, 2, 2, 2, 2, 3, 2, 2, 2, 3, 2, 2, 2, 3, 2, 2, 2, 2, 3, 2, 2, 2, 2, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

			if !relativelyEqual(t, s1, expected1) {
				t.Errorf("t1 failure:\nexpected: %#v\n     got: %#v\n", expected1, s1)
			}
			if !relativelyEqual(t, s2, expected2) {
				t.Errorf("t2 failure:\nexpected: %#v\n     got: %#v\n", expected2, s2)
			}
			if !relativelyEqual(t, s3, expected3) {
				t.Errorf("t3 failure:\nexpected: %#v\n     got: %#v\n", expected3, s3)
			}
		})
	})
}
