package circuit

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"sync/atomic"
	"testing"
	"time"
)

func TestBreaker(t *testing.T) {
	t.Parallel()
	//var saneOptions = func() BreakerOptions {
	//	return BreakerOptions{
	//		Name:      "testCB",
	//		Timeout:   time.Second,
	//		BackOff:   5 * time.Second,
	//		Window:    30 * time.Second,
	//		Threshold: 2,
	//		LockOut:   2 * time.Second,
	//	}
	//}

	t.Run("NewBreaker", func(t *testing.T) {
		t.Parallel()
		t.Run("defaults", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})

			// name
			nameRx := regexp.MustCompile(`func_circuit_TestBreaker_func[\d_]+file_breaker_test_line_\d+`)
			if !nameRx.Match([]byte(breaker.name)) {
				t.Fatalf("expected regex to match for default name: %s", breaker.name)
			}

			// timeout
			if breaker.timeout != DefaultTimeout {
				t.Fatalf("expected default timeout, got %v", breaker.timeout)
			}

			// baudrate
			if breaker.baudrate != DefaultBaudRate {
				t.Fatalf("expected default baudrate, got %v", breaker.baudrate)
			}

			// backoff
			if breaker.backoff != DefaultBackOff {
				t.Fatalf("expected default backoff, got %v", breaker.backoff)
			}

			// window
			if breaker.window != DefaultWindow {
				t.Fatalf("expected default window, got %v", breaker.window)
			}

			// interpolate
			if breaker.interpolate == nil {
				t.Fatalf("interpolation func cannot be nil")
			}
			for i := 1; i < 100; i++ {
				if breaker.interpolate(i) != uint32(100-i) {
					t.Fatalf("linear interpolation was expected")
				}
			}
		})

		t.Run("illegal options", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{
				BaudRate: time.Millisecond,
				BackOff:  time.Millisecond,
				Window:   time.Millisecond,
			})

			// baudrate
			if breaker.baudrate != minimumBaudRate {
				t.Fatalf("expected default baudrate, got %v", breaker.baudrate)
			}

			// backoff
			if breaker.backoff != minimumBackoff {
				t.Fatalf("expected default backoff, got %v", breaker.backoff)
			}

			// window
			if breaker.window != minimumWindow {
				t.Fatalf("expected default window, got %v", breaker.window)
			}
		})
	})

	t.Run("locks", func(t *testing.T) {
		t.Parallel()

		t.Run("new breaker", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})

			lockedSince, isLocked := breaker.lockStatus()
			if !lockedSince.IsZero() || isLocked {
				t.Fatalf("the lock status of a new breaker is incorrect")
			}
		})

		t.Run("set locked", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{
				LockOut: time.Second,
			})
			breaker.setLocked(true)

			lockedSince, isLocked := breaker.lockStatus()
			if lockedSince.IsZero() || !isLocked {
				t.Fatalf("the circuit breaker should be locked")
			}

			time.Sleep(1100 * time.Millisecond)
			lockedSince, isLocked = breaker.lockStatus()
			if !lockedSince.IsZero() || isLocked {
				t.Fatalf("the breaker should have unlocked")
			}
		})

		t.Run("manual unlock", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{
				LockOut: time.Second,
			})

			breaker.setLocked(true)
			lockedSince, isLocked := breaker.lockStatus()
			if lockedSince.IsZero() || !isLocked {
				t.Fatalf("the circuit breaker should be locked")
			}

			breaker.setLocked(false)
			lockedSince, isLocked = breaker.lockStatus()
			if !lockedSince.IsZero() || isLocked {
				t.Fatalf("the breaker should have unlocked immediately")
			}
		})
	})

	t.Run("throttles", func(t *testing.T) {
		t.Parallel()
		t.Run("new breaker", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})

			throttledSince, isThrottled := breaker.throttledStatus()
			if !throttledSince.IsZero() || isThrottled {
				t.Fatalf("the throttle status of a new breaker is incorrect")
			}
		})

		t.Run("set throttled", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{BackOff: minimumBackoff})

			breaker.setThrottled(true)

			throttledSince, isThrottled := breaker.throttledStatus()
			if throttledSince.IsZero() || !isThrottled {
				t.Fatalf("the circuit breaker should be throttled")
			}

			time.Sleep(1100 * time.Millisecond)
			throttledSince, isThrottled = breaker.lockStatus()
			if !throttledSince.IsZero() || isThrottled {
				t.Fatalf("the breaker should not be throttled")
			}
			if breaker.State() != Closed {
				t.Fatalf("the breaker should be closed")
			}
		})

		t.Run("calls to interpolation", func(t *testing.T) {
			t.Parallel()
			var count uint32
			breaker := NewBreaker(BreakerOptions{
				BackOff: minimumBackoff,
				InterpolationFunc: func(int) uint32 {
					atomic.AddUint32(&count, 1)
					return 0
				},
			})

			breaker.setThrottled(true)
			time.Sleep(1100 * time.Millisecond)

			if count != 100 {
				t.Fatalf("expected the interpolation func to run 100 times.  It ran %d times", count)
			}
		})

		t.Run("cancelling interpolation", func(t *testing.T) {
			t.Parallel()
			var count uint32
			breaker := NewBreaker(BreakerOptions{
				BackOff: minimumBackoff,
				InterpolationFunc: func(int) uint32 {
					atomic.AddUint32(&count, 1)
					return 0
				},
			})

			breaker.setThrottled(true)
			time.Sleep(500 * time.Millisecond)
			breaker.setThrottled(false)
			time.Sleep(600 * time.Millisecond)

			t.Log("count", count)
			if count > 50 {
				t.Fatalf("expected the interpolation func to cancel half way through.  It ran %d times", count)
			}
		})
	})

	t.Run("closed", func(t *testing.T) {
		t.Parallel()
		t.Run("new breaker", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})

			closedSince, isClosed := breaker.closedStatus()
			if closedSince.IsZero() || !isClosed {
				t.Fatalf("the closed status of a new breaker is incorrect")
			}
		})

		t.Run("set manually", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})
			breaker.setClosed(false)

			closedSince, isClosed := breaker.closedStatus()
			if !closedSince.IsZero() || isClosed {
				t.Fatalf("the circuit breaker should not be closed")
			}

			breaker.setClosed(true)
			closedSince, isClosed = breaker.closedStatus()
			if closedSince.IsZero() || !isClosed {
				t.Fatalf("the circuit breaker should be closed")
			}
		})
	})

	t.Run("changeStateTo", func(t *testing.T) {
		t.Parallel()

		t.Run("closed to open", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{LockOut: time.Second})
			breaker.changeStateTo(internalOpen)

			closedSince, isClosed := breaker.closedStatus()
			throttledSince, isThrottled := breaker.throttledStatus()
			lockedSince, isLocked := breaker.lockStatus()

			if !closedSince.IsZero() || isClosed {
				t.Fatalf("the circuit breaker should not be closed")
			}
			if !throttledSince.IsZero() || isThrottled {
				t.Fatalf("the circuit breaker should not be throttled")
			}
			if lockedSince.IsZero() || !isLocked {
				t.Fatalf("the circuit breaker should be locked")
			}
		})

		t.Run("open to throttled implicitly", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{LockOut: time.Second})
			breaker.changeStateTo(internalOpen)

			// 1 second plus baudrate + cushion
			time.Sleep(1500 * time.Millisecond)

			closedSince, isClosed := breaker.closedStatus()
			throttledSince, isThrottled := breaker.throttledStatus()
			lockedSince, isLocked := breaker.lockStatus()

			if !closedSince.IsZero() || isClosed {
				t.Fatalf("the circuit breaker should not be closed")
			}
			if throttledSince.IsZero() || !isThrottled {
				t.Fatalf("the circuit breaker should be throttled")
			}
			if !lockedSince.IsZero() || isLocked {
				t.Fatalf("the circuit breaker should not be locked")
			}
		})

		t.Run("open to throttled explicitly", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{LockOut: time.Second})
			breaker.changeStateTo(internalOpen)
			breaker.changeStateTo(internalThrottled)

			closedSince, isClosed := breaker.closedStatus()
			throttledSince, isThrottled := breaker.throttledStatus()
			lockedSince, isLocked := breaker.lockStatus()

			if !closedSince.IsZero() || isClosed {
				t.Fatalf("the circuit breaker should not be closed")
			}
			if throttledSince.IsZero() || !isThrottled {
				t.Fatalf("the circuit breaker should be throttled")
			}
			if !lockedSince.IsZero() || isLocked {
				t.Fatalf("the circuit breaker should not be locked")
			}
		})

		t.Run("throttled to open", func(t *testing.T) {
			t.Parallel()
			var count uint32
			breaker := NewBreaker(BreakerOptions{
				LockOut: time.Second,
				BackOff: minimumBackoff,
				InterpolationFunc: func(int) uint32 {
					atomic.AddUint32(&count, 1)
					return 0
				},
			})
			breaker.changeStateTo(internalOpen)
			breaker.changeStateTo(internalThrottled)
			time.Sleep(500 * time.Millisecond)
			breaker.changeStateTo(internalOpen)

			closedSince, isClosed := breaker.closedStatus()
			throttledSince, isThrottled := breaker.throttledStatus()
			lockedSince, isLocked := breaker.lockStatus()

			if !closedSince.IsZero() || isClosed {
				t.Fatalf("the circuit breaker should not be closed")
			}
			if !throttledSince.IsZero() || isThrottled {
				t.Fatalf("the circuit breaker should not be throttled")
			}
			if lockedSince.IsZero() || !isLocked {
				t.Fatalf("the circuit breaker should be locked")
			}
			t.Log("count", count)
			if count > 50 {
				t.Fatalf("the throttle should have canceled half way through")
			}
		})

		t.Run("throttled to closed implicitly", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{
				LockOut: time.Second,
				BackOff: time.Second,
			})
			breaker.changeStateTo(internalOpen)
			breaker.changeStateTo(internalThrottled)

			time.Sleep(1500 * time.Millisecond)

			closedSince, isClosed := breaker.closedStatus()
			throttledSince, isThrottled := breaker.throttledStatus()
			lockedSince, isLocked := breaker.lockStatus()

			if closedSince.IsZero() || !isClosed {
				t.Fatalf("the circuit breaker should be closed")
			}
			if !throttledSince.IsZero() || isThrottled {
				t.Fatalf("the circuit breaker should not be throttled")
			}
			if !lockedSince.IsZero() || isLocked {
				t.Fatalf("the circuit breaker should not be locked")
			}
		})

		t.Run("change listener", func(t *testing.T) {
			t.Parallel()
			quit := make(chan struct{}, 1)
			states := make([]string, 0)

			breaker := NewBreaker(BreakerOptions{LockOut: time.Second, BackOff: minimumBackoff})

			go func(stateChange <-chan BreakerState, quit chan struct{}) {
				for {
					select {
					case <-quit:
						return
					case state := <-stateChange:
						states = append(states, state.String())
					}
				}
			}(breaker.StateChange(), quit)
			time.Sleep(time.Millisecond)
			breaker.changeStateTo(internalOpen)
			time.Sleep(1500 * time.Millisecond)
			breaker.changeStateTo(internalOpen)
			time.Sleep(2500 * time.Millisecond)
			quit <- struct{}{}

			if !reflect.DeepEqual(states, []string{"closed", "open", "throttled", "open", "throttled", "closed"}) {
				t.Fatalf("state changes are not registering properly")
			}
		})
	})

	t.Run("calc", func(t *testing.T) {
		t.Parallel()

		t.Run("default to open", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{LockOut: time.Second, BackOff: minimumBackoff})
			closedSince, isClosed := breaker.closedStatus()
			throttledSince, isThrottled := breaker.throttledStatus()
			lockedSince, isLocked := breaker.lockStatus()

			if closedSince.IsZero() || !isClosed {
				t.Fatalf("the circuit breaker should be closed")
			}
			if !throttledSince.IsZero() || isThrottled {
				t.Fatalf("the circuit breaker should not be throttled")
			}
			if !lockedSince.IsZero() || isLocked {
				t.Fatalf("the circuit breaker should not be locked")
			}

			breaker.tracker.incr()
			time.Sleep(DefaultBaudRate + 10*time.Millisecond)

			closedSince, isClosed = breaker.closedStatus()
			throttledSince, isThrottled = breaker.throttledStatus()
			lockedSince, isLocked = breaker.lockStatus()

			if !closedSince.IsZero() || isClosed {
				t.Fatalf("the circuit breaker should not be closed")
			}
			if !throttledSince.IsZero() || isThrottled {
				t.Fatalf("the circuit breaker should not be throttled")
			}
			if lockedSince.IsZero() || !isLocked {
				t.Fatalf("the circuit breaker should be locked")
			}
		})

		t.Run("throttled to open", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{LockOut: time.Second, BackOff: minimumBackoff})
			breaker.changeStateTo(internalThrottled)

			closedSince, isClosed := breaker.closedStatus()
			throttledSince, isThrottled := breaker.throttledStatus()
			lockedSince, isLocked := breaker.lockStatus()

			if !closedSince.IsZero() || isClosed {
				t.Fatalf("the circuit breaker should not be closed")
			}
			if throttledSince.IsZero() || !isThrottled {
				t.Fatalf("the circuit breaker should be throttled")
			}
			if !lockedSince.IsZero() || isLocked {
				t.Fatalf("the circuit breaker should not be locked")
			}

			breaker.tracker.incr()
			time.Sleep(DefaultBaudRate + 10*time.Millisecond)

			closedSince, isClosed = breaker.closedStatus()
			throttledSince, isThrottled = breaker.throttledStatus()
			lockedSince, isLocked = breaker.lockStatus()

			if !closedSince.IsZero() || isClosed {
				t.Fatalf("the circuit breaker should not be closed")
			}
			if !throttledSince.IsZero() || isThrottled {
				t.Fatalf("the circuit breaker should not be throttled")
			}
			if lockedSince.IsZero() || !isLocked {
				t.Fatalf("the circuit breaker should be locked")
			}
		})

		t.Run("open to throttled", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{LockOut: time.Second, BackOff: minimumBackoff})
			breaker.changeStateTo(internalOpen)

			closedSince, isClosed := breaker.closedStatus()
			throttledSince, isThrottled := breaker.throttledStatus()
			lockedSince, isLocked := breaker.lockStatus()

			if !closedSince.IsZero() || isClosed {
				t.Fatalf("the circuit breaker should not be closed")
			}
			if !throttledSince.IsZero() || isThrottled {
				t.Fatalf("the circuit breaker should not be throttled")
			}
			if lockedSince.IsZero() || !isLocked {
				t.Fatalf("the circuit breaker should be locked")
			}

			time.Sleep(DefaultBaudRate + 10*time.Millisecond)
			time.Sleep(minimumBackoff)

			closedSince, isClosed = breaker.closedStatus()
			throttledSince, isThrottled = breaker.throttledStatus()
			lockedSince, isLocked = breaker.lockStatus()

			if !closedSince.IsZero() || isClosed {
				t.Fatalf("the circuit breaker should not be closed")
			}
			if throttledSince.IsZero() || !isThrottled {
				t.Fatalf("the circuit breaker should be throttled")
			}
			if !lockedSince.IsZero() || isLocked {
				t.Fatalf("the circuit breaker should not be locked")
			}
		})
	})

	t.Run("apply throttle", func(t *testing.T) {
		t.Parallel()

		t.Run("100% throttle chance", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})
			breaker.throttleChance = 100

			if err := breaker.applyThrottle(); err == nil {
				t.Fatal("applying the throttle with 100% throttle chance should always return an error")
			}
		})

		t.Run("0% throttle chance", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})
			breaker.throttleChance = 0

			if err := breaker.applyThrottle(); err != nil {
				t.Fatal("applying the throttle with 0% throttle chance should never return an error")
			}
		})
	})

	t.Run("preprocessors", func(t *testing.T) {
		t.Parallel()
		t.Run("block run", func(t *testing.T) {
			t.Parallel()
			theError := errors.New("you shall not pass")
			breaker := NewBreaker(BreakerOptions{
				PreProcessors: []PreProcessor{
					func(ctx context.Context, runner Runner) (context.Context, Runner, error) {
						return ctx, nil, theError
					},
				},
			})

			_, err := breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
				return true, nil
			})

			if !errors.Is(err, theError) {
				t.Fatal("the preprocessor should have blocked execution and returned a specific error")
			}
		})

		t.Run("replace run", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{
				PreProcessors: []PreProcessor{
					func(ctx context.Context, runner Runner) (context.Context, Runner, error) {
						return ctx, func(ctx context.Context) (interface{}, error) {
							return true, nil
						}, nil
					},
				},
			})

			_result, err := breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
				return nil, errors.New("i should be overridden")
			})

			if err != nil {
				t.Fatal("the error should have been overridden by the preprocessor")
			}

			result, _ := _result.(bool)
			if !result {
				t.Fatal("the result should have been true")
			}
		})
	})

	t.Run("postprocessors", func(t *testing.T) {
		t.Parallel()
		t.Run("override output", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{
				PostProcessors: []PostProcessor{
					func(ctx context.Context, i interface{}, err error) (interface{}, error) {
						return true, nil
					},
				},
			})
			_result, err := breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
				return nil, errors.New("i should be overridden")
			})
			if err != nil {
				t.Fatal("the error should have been overridden by the postprocessor")
			}

			result, _ := _result.(bool)
			if !result {
				t.Fatal("the result should have been true")
			}
		})

		t.Run("override output with error", func(t *testing.T) {
			t.Parallel()
			theError := errors.New("you shall not pass")
			breaker := NewBreaker(BreakerOptions{
				PostProcessors: []PostProcessor{
					func(ctx context.Context, i interface{}, err error) (interface{}, error) {
						return nil, theError
					},
				},
			})
			_, err := breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
				return true, nil
			})
			if !errors.Is(err, theError) {
				t.Fatal("postprocessor should have returned an error")
			}
		})
	})

	t.Run("check fitness", func(t *testing.T) {
		t.Parallel()
		t.Run("with a canceled context", func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			breaker := NewBreaker(BreakerOptions{})

			err := breaker.checkFitness(ctx)

			if !errors.Is(err, context.Canceled) {
				t.Fatal("expected a context.Canceled error")
			}
		})

		t.Run("open breaker", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{LockOut: time.Second})
			breaker.changeStateTo(internalOpen)
			err := breaker.checkFitness(context.Background())

			if !errors.Is(err, StateOpenError) {
				t.Fatal("expected StateOpenError")
			}
		})

		t.Run("throttled breaker", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{
				BackOff: minimumBackoff,
				InterpolationFunc: func(int) uint32 {
					return 100
				},
			})
			breaker.changeStateTo(internalThrottled)
			err := breaker.checkFitness(context.Background())

			if !errors.Is(err, StateThrottledError) {
				t.Fatal("expected StateThrottledError")
			}
		})

		t.Run("closed breaker", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})

			if err := breaker.checkFitness(context.Background()); err != nil {
				t.Fatal("the error should have been nil")
			}
		})

		t.Run("unknown state (PEBKAC edge case)", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})
			breaker.state = uint32(100)
			err := breaker.checkFitness(context.Background())

			if !errors.Is(err, StateUnknownError) {
				t.Fatal("expected StateUnknownError")
			}
		})
	})

	t.Run("snapshot", func(t *testing.T) {
		t.Parallel()
		t.Run("new breaker", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{Name: "foo"})
			snap := breaker.Snapshot()
			if snap.Name != "foo" {
				t.Fatal("snapshot is not capturing breaker name")
			}
			if snap.String() != "closed" {
				t.Fatalf("the stringer for snap returned wrong value: %s", snap.String())
			}
			if snap.State != Closed {
				t.Fatal("state should be Closed")
			}
			if snap.ClosedSince == nil {
				t.Fatal("the ClosedSince property should not be nil")
			}
			if snap.Throttled != nil {
				t.Fatal("the Throttled property should not be set")
			}
			if snap.BackOffEnds != nil {
				t.Fatal("the BackOffEnds property should not be set")
			}
			if snap.Opened != nil {
				t.Fatal("the Opened property should not be set")
			}
			if snap.LockoutEnds != nil {
				t.Fatal("the LockoutEnds property should not be set")
			}
		})

		t.Run("throttled breaker", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{Name: "foo"})
			breaker.changeStateTo(internalThrottled)
			snap := breaker.Snapshot()
			if snap.Name != "foo" {
				t.Fatal("snapshot is not capturing breaker name")
			}
			if snap.String() != "throttled" {
				t.Fatalf("the stringer for snap returned wrong value: %s", snap.String())
			}
			if snap.State != Throttled {
				t.Fatal("state should be Throttled")
			}
			if snap.ClosedSince != nil {
				t.Fatal("the ClosedSince property should not be set")
			}
			if snap.Throttled == nil {
				t.Fatal("the Throttled property should not be nil")
			}
			if snap.BackOffEnds == nil {
				t.Fatal("the BackOffEnds property should not be nil")
			}
			if snap.Opened != nil {
				t.Fatal("the Opened property should not be set")
			}
			if snap.LockoutEnds != nil {
				t.Fatal("the LockoutEnds property should not be set")
			}
		})

		t.Run("open breaker", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{Name: "foo", LockOut: time.Second})
			breaker.changeStateTo(internalOpen)
			snap := breaker.Snapshot()
			if snap.Name != "foo" {
				t.Fatal("snapshot is not capturing breaker name")
			}
			if snap.String() != "open" {
				t.Fatalf("the stringer for snap returned wrong value: %s", snap.String())
			}
			if snap.State != Open {
				t.Fatal("state should be Open")
			}
			if snap.ClosedSince != nil {
				t.Fatal("the ClosedSince property should not be set")
			}
			if snap.Throttled != nil {
				t.Fatal("the Throttled property should not be set")
			}
			if snap.BackOffEnds != nil {
				t.Fatal("the BackOffEnds property should not be set")
			}
			if snap.Opened == nil {
				t.Fatal("the Opened property should not be nil")
			}
			if snap.LockoutEnds == nil {
				t.Fatal("the LockoutEnds property should not be nil")
			}
		})
	})

	t.Run("size", func(t *testing.T) {
		t.Parallel()
		t.Run("new breaker", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})
			if sz := breaker.Size(); sz != 0 {
				t.Fatal("a new breaker should have size 0")
			}
		})

		t.Run("three errors", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})
			breaker.tracker.incr()
			breaker.tracker.incr()
			breaker.tracker.incr()
			time.Sleep(time.Millisecond)
			if sz := breaker.Size(); sz != 3 {
				t.Fatal("breaker should be size 3")
			}
		})
	})

	t.Run("edge cases for Run", func(t *testing.T) {
		t.Parallel()
		t.Run("breaker times out", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{
				Timeout: 10 * time.Millisecond,
			})

			_, err := breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
				time.Sleep(20 * time.Millisecond)
				return true, nil
			})

			if !errors.Is(err, TimeoutError) {
				t.Fatal("the circuit breaker should have timed out")
			}
		})

		t.Run("canceled context propagation", func(t *testing.T) {
			t.Parallel()
			t.Run("default behavior", func(t *testing.T) {
				t.Parallel()
				breaker := NewBreaker(BreakerOptions{
					Timeout: 10 * time.Millisecond,
				})
				var tick uint32
				_, _ = breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
					time.Sleep(20 * time.Millisecond)
					if ctx.Err() != nil {
						atomic.AddUint32(&tick, 1)
						return true, nil
					}
					atomic.AddUint32(&tick, 2)
					return true, nil
				})

				time.Sleep(30 * time.Millisecond)
				t.Log("tick", tick)
				if tick != 1 {
					t.Fatal("context error didnt propagate")
				}
			})

			t.Run("no propagation ", func(t *testing.T) {
				t.Parallel()
				breaker := NewBreaker(BreakerOptions{
					Timeout:       10 * time.Millisecond,
					IgnoreContext: true,
				})
				var tick uint32
				_, _ = breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
					time.Sleep(20 * time.Millisecond)
					if ctx.Err() != nil {
						atomic.AddUint32(&tick, 1)
						return true, nil
					}
					atomic.AddUint32(&tick, 2)
					return true, nil
				})

				time.Sleep(30 * time.Millisecond)
				t.Log("tick", tick)
				if tick != 2 {
					t.Fatal("context error didnt propagate")
				}
			})
		})

		t.Run("improperly initialized", func(t *testing.T) {
			t.Parallel()
			breaker := Breaker{}
			_, err := breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
				return true, nil
			})
			if !errors.Is(err, NotInitializedError) {
				t.Fatal("expected the improper initialization error")
			}
		})

		t.Run("unfit because open", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{LockOut: time.Second})
			breaker.changeStateTo(internalOpen)
			_, err := breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
				return true, nil
			})

			if !errors.Is(err, StateOpenError) {
				t.Fatal("fitness chack failed")
			}
		})

		t.Run("unfit because throttled", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{
				BackOff: minimumBackoff,
				InterpolationFunc: func(int) uint32 {
					return 100
				},
			})
			breaker.changeStateTo(internalThrottled)
			_, err := breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
				return true, nil
			})

			if !errors.Is(err, StateThrottledError) {
				t.Fatal("fitness check failed")
			}
		})

		t.Run("unfit because context canceled", func(t *testing.T) {
			t.Parallel()
			breaker := NewBreaker(BreakerOptions{})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := breaker.Run(ctx, func(ctx context.Context) (interface{}, error) {
				return true, nil
			})

			if !errors.Is(err, context.Canceled) {
				t.Fatal("fitness check failed")
			}
		})

	})
}
