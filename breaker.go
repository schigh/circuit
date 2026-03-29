package circuit

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"path"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	internalClosed uint32 = iota
	internalThrottled
	internalOpen
)

// Breaker is the circuit breaker implementation for this package
type Breaker struct {
	// switches
	openingResets bool // If true, the circuit breaker resets its error count upon opening

	// state
	threshold uint32 // Maximum number of errors allowed to occur in window
	state     uint32 // Current state

	// event timestamps
	lockedSince    int64 // Unix nano timestamp of current lock creation
	openSince      int64 // Unix nano timestamp of current open state creation
	throttledSince int64 // Unix nano timestamp of current throttled state creation
	closedSince    int64 // Unix nano timestamp of last closed time (or creation)

	// name
	name string // Circuit Breaker name

	// timings
	timeout time.Duration // Timeout for Run func
	backoff time.Duration // Length of time the breaker is throttled
	lockout time.Duration // Length of time a breaker is locked out once it opens
	window  time.Duration // Window of time to look for errors (e.g. 5 errors in 10 mins)

	// misc
	stateMX  sync.Mutex       // Protects state transitions in evaluateState/changeStateTo
	tracker  *errTracker      // Error tracker
	estimate EstimationFunc   // Function used to estimate throttling chance
	metrics  MetricsCollector // Optional metrics collector

	// orchestration
	stateChange    chan BreakerState
	boxStateChange chan BreakerState // set by BreakerBox.Create for forwarding

	// hooks
	onStateChange func(breakerName string, from, to State)

	// error classification
	isSuccessful func(error) bool
	isExcluded   func(error) bool
}

// NewBreaker creates a new Breaker using functional options.
// All new Breaker instances MUST be created with this function.
func NewBreaker(opts ...Option) (*Breaker, error) {
	b := &Breaker{}

	for _, opt := range opts {
		opt(b)
	}

	// if there is no name, just make a signature from the caller
	if b.name == "" {
		function, file, line, _ := runtime.Caller(1)
		b.name = strings.ReplaceAll(
			strings.ReplaceAll(
				fmt.Sprintf(
					"func_%s_file_%s_line_%d",
					path.Base(runtime.FuncForPC(function).Name()),
					path.Base(file),
					line,
				), ".go", ""),
			".", "_",
		)
	}
	if b.timeout == 0 {
		b.timeout = DefaultTimeout
	}
	if b.backoff == 0 {
		b.backoff = DefaultBackOff
	}
	if b.backoff < minimumBackoff {
		b.backoff = minimumBackoff
	}
	if b.window == 0 {
		b.window = DefaultWindow
	}
	if b.window < minimumWindow {
		b.window = minimumWindow
	}
	if b.estimate == nil {
		b.estimate = Linear
	}

	b.stateChange = make(chan BreakerState, 16)
	b.tracker = newErrTracker(b.window)
	now := time.Now()
	b.closedSince = now.UnixNano()

	b.stateChange <- BreakerState{
		Name:        b.name,
		State:       Closed,
		ClosedSince: &now,
	}

	return b, nil
}

// get the lock status
func (b *Breaker) openAndLockedStatus() (openedAt time.Time, lockedAt time.Time, isOpen bool, isLocked bool) {
	o := atomic.LoadInt64(&b.openSince)
	if o != 0 {
		openedAt = timeFromNS(o)
		isOpen = true
	}
	l := atomic.LoadInt64(&b.lockedSince)
	if l != 0 {
		lockedAt = timeFromNS(l)
		isLocked = true
	}
	return
}

// open the circuit breaker and set lockout timestamp if enabled
func (b *Breaker) setOpen(is bool) {
	b.tracker.reset(is && b.openingResets)

	if !is {
		atomic.SwapInt64(&b.openSince, 0)
		return
	}

	nowNano := time.Now().UnixNano()
	atomic.SwapInt64(&b.openSince, nowNano)

	if b.lockout == 0 {
		return
	}
	atomic.SwapInt64(&b.lockedSince, nowNano)
}

// get the throttle status
func (b *Breaker) throttledStatus() (time.Time, bool) {
	l := atomic.LoadInt64(&b.throttledSince)
	if l == 0 {
		return time.Time{}, false
	}
	return timeFromNS(l), true
}

// set throttled state (just timestamps, no goroutines)
func (b *Breaker) setThrottled(is bool) {
	if !is {
		atomic.SwapInt64(&b.throttledSince, 0)
		return
	}
	atomic.SwapInt64(&b.throttledSince, time.Now().UnixNano())
}

// get the closed status
func (b *Breaker) closedStatus() (time.Time, bool) {
	l := atomic.LoadInt64(&b.closedSince)
	if l == 0 {
		return time.Time{}, false
	}
	return timeFromNS(l), true
}

func (b *Breaker) setClosed(is bool) {
	if is {
		atomic.SwapInt64(&b.closedSince, time.Now().UnixNano())
		return
	}
	atomic.SwapInt64(&b.closedSince, 0)
}

// changeStateTo records a state transition.
// Must be called with stateMX write-locked.
// The onStateChange callback is invoked AFTER releasing the lock
// to avoid blocking requests on user code.
// Returns the BreakerState and whether a transition occurred,
// so the caller can invoke hooks after releasing the lock.
func (b *Breaker) changeStateTo(to uint32) (BreakerState, bool) {
	from := atomic.SwapUint32(&b.state, to)
	if from == to {
		return BreakerState{}, false
	}

	newState := BreakerState{
		Name:  b.name,
		State: State(to),
	}

	switch from {
	case internalOpen:
		b.setOpen(false)
	case internalThrottled:
		b.setThrottled(false)
	case internalClosed:
		b.setClosed(false)
	}

	switch to {
	case internalOpen:
		b.setOpen(true)
		openedAt, lockedAt, isOpen, isLocked := b.openAndLockedStatus()
		if isOpen {
			newState.Opened = &openedAt
		}
		if isLocked {
			end := lockedAt.Add(b.lockout)
			newState.LockoutEnds = &end
		}
	case internalThrottled:
		b.setThrottled(true)
		ts, throttled := b.throttledStatus()
		if throttled {
			newState.Throttled = &ts
			end := ts.Add(b.backoff)
			newState.BackOffEnds = &end
		}
	case internalClosed:
		b.setClosed(true)
		ts, closed := b.closedStatus()
		if closed {
			newState.ClosedSince = &ts
		}
	}

	if b.metrics != nil {
		b.metrics.RecordStateChange(b.name, State(from), State(to))
	}

	select {
	case b.stateChange <- newState:
	default:
	}

	if b.boxStateChange != nil {
		select {
		case b.boxStateChange <- newState:
		default:
		}
	}

	return newState, true
}

// evaluateState lazily evaluates and transitions state.
// Must be called with stateMX held.
// Returns the pending onStateChange callback info (if any)
// so the caller can invoke it after releasing the lock.
func (b *Breaker) evaluateState() (from, to State, transitioned bool) {
	state := atomic.LoadUint32(&b.state)
	var target uint32
	switch state {
	case internalClosed:
		if b.tracker.size() > b.threshold {
			target = internalOpen
		} else {
			return
		}
	case internalOpen:
		lockedAt := atomic.LoadInt64(&b.lockedSince)
		if lockedAt != 0 {
			if time.Since(timeFromNS(lockedAt)) >= b.lockout {
				atomic.StoreInt64(&b.lockedSince, 0)
			} else {
				return // still locked
			}
		}
		if b.tracker.size() <= b.threshold {
			target = internalThrottled
		} else {
			return
		}
	case internalThrottled:
		if b.tracker.size() > b.threshold {
			target = internalOpen
		} else {
			ts := atomic.LoadInt64(&b.throttledSince)
			if ts != 0 && time.Since(timeFromNS(ts)) >= b.backoff {
				target = internalClosed
			} else {
				return
			}
		}
	default:
		return
	}

	_, changed := b.changeStateTo(target)
	if changed {
		return State(state), State(target), true
	}
	return
}

// applyThrottle calculates throttle chance lazily from elapsed time.
func (b *Breaker) applyThrottle() error {
	ts := atomic.LoadInt64(&b.throttledSince)
	if ts == 0 {
		return nil
	}
	elapsed := time.Since(timeFromNS(ts))
	tick := int(elapsed * 100 / b.backoff)
	if tick < 1 {
		tick = 1
	}
	if tick > 100 {
		tick = 100
	}
	chance := b.estimate(tick)
	if rand.Uint32N(100) >= chance {
		return nil
	}
	return ErrStateThrottled.withContext(b.name, Throttled)
}

// checkFitness determines if a request is allowed to proceed.
func (b *Breaker) checkFitness(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Fast path: if closed and no errors exceed threshold, skip the lock entirely.
	// This is the overwhelmingly common case in production.
	state := atomic.LoadUint32(&b.state)
	if state == internalClosed && b.tracker.size() <= b.threshold {
		return nil
	}

	// Slow path: state may need to transition. Acquire lock.
	b.stateMX.Lock()
	from, to, transitioned := b.evaluateState()
	b.stateMX.Unlock()

	if transitioned && b.onStateChange != nil {
		b.onStateChange(b.name, from, to)
	}

	state = atomic.LoadUint32(&b.state)
	switch state {
	case internalOpen:
		if b.metrics != nil {
			b.metrics.RecordRejected(b.name, Open)
		}
		return ErrStateOpen.withContext(b.name, Open)
	case internalThrottled:
		err := b.applyThrottle()
		if err != nil && b.metrics != nil {
			b.metrics.RecordRejected(b.name, Throttled)
		}
		return err
	case internalClosed:
		return nil
	default:
		return ErrStateUnknown.withContext(b.name, State(state))
	}
}

// recordOutcome classifies and records the result of a call.
func (b *Breaker) recordOutcome(err error, elapsed time.Duration) {
	if err == nil {
		if b.metrics != nil {
			b.metrics.RecordSuccess(b.name, elapsed)
		}
		return
	}

	// excluded errors are not tracked at all
	if b.isExcluded != nil && b.isExcluded(err) {
		if b.metrics != nil {
			b.metrics.RecordExcluded(b.name, err)
		}
		return
	}

	// errors classified as successful don't count as failures
	if b.isSuccessful != nil && b.isSuccessful(err) {
		if b.metrics != nil {
			b.metrics.RecordSuccess(b.name, elapsed)
		}
		return
	}

	// classify deadline exceeded as timeout
	if errors.Is(err, context.DeadlineExceeded) {
		if b.metrics != nil {
			b.metrics.RecordTimeout(b.name)
		}
	}

	// failure
	b.tracker.incr()
	if b.metrics != nil {
		b.metrics.RecordError(b.name, elapsed, err)
	}
}

// Allow checks if the breaker allows a request (two-step mode).
// If allowed, it returns a done callback. The caller invokes done(err)
// after the operation completes to report the outcome.
// No timeout is applied — the caller controls timing.
func (b *Breaker) Allow(ctx context.Context) (func(error), error) {
	if b.tracker == nil {
		return nil, ErrNotInitialized
	}
	if err := b.checkFitness(ctx); err != nil {
		return nil, err
	}
	start := time.Now()
	done := func(err error) {
		b.recordOutcome(err, time.Since(start))
	}
	return done, nil
}

// Name returns the circuit breaker's name.
func (b *Breaker) Name() string {
	return b.name
}

// State returns the current state of the circuit breaker.
// This triggers lazy state evaluation.
func (b *Breaker) State() State {
	b.stateMX.Lock()
	from, to, transitioned := b.evaluateState()
	b.stateMX.Unlock()

	if transitioned && b.onStateChange != nil {
		b.onStateChange(b.name, from, to)
	}

	return State(atomic.LoadUint32(&b.state))
}

// StateChange returns the channel for state change notifications.
func (b *Breaker) StateChange() <-chan BreakerState {
	return b.stateChange
}

// Size gets the number of errors present in the current tracking window.
func (b *Breaker) Size() int {
	return int(b.tracker.size())
}

// Snapshot returns a current snapshot of the circuit breaker.
// This triggers lazy state evaluation.
func (b *Breaker) Snapshot() BreakerState {
	b.stateMX.Lock()
	from, to, transitioned := b.evaluateState()
	b.stateMX.Unlock()

	if transitioned && b.onStateChange != nil {
		b.onStateChange(b.name, from, to)
	}

	state := State(atomic.LoadUint32(&b.state))

	bs := BreakerState{
		Name:  b.name,
		State: state,
	}

	switch state {
	case Closed:
		if since, ok := b.closedStatus(); ok {
			bs.ClosedSince = &since
		}
	case Throttled:
		if since, ok := b.throttledStatus(); ok {
			bs.Throttled = &since
			ends := since.Add(b.backoff)
			bs.BackOffEnds = &ends
		}
	case Open:
		openedAt, lockedAt, isOpen, isLocked := b.openAndLockedStatus()
		if isOpen {
			bs.Opened = &openedAt
		}
		if isLocked {
			ends := lockedAt.Add(b.lockout)
			bs.LockoutEnds = &ends
		}
	}

	return bs
}

func timeFromNS(ns int64) time.Time {
	u := ns / 1e9
	return time.Unix(u, ns-u*1e9)
}
