package circuit

import (
	"context"
	"fmt"
	"math/rand"
	"os"
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

// BreakerOptions contains configuration options for a circuit breaker
type BreakerOptions struct {
	// Name is the circuit breaker name. If name is not provided,
	// a unique name will be created based on the caller to NewBreaker
	Name string

	// Timeout is the maximum duration that the Run func
	// can execute before timing out.  The default is 3 seconds.
	Timeout time.Duration

	// BaudRate is the duration between error calculations.
	// The default value is 250ms
	BaudRate time.Duration

	// Backoff is the duration that a circuit breaker is
	// throttled.  The default Backoff is 1 minute.
	// The minimum Backoff is 1 second
	BackOff time.Duration

	// Window is the length of time checked for error
	// calculation. The default window is 10 minutes.
	// The minimum window is 10 seconds
	Window time.Duration

	// Threshold is the maximum number of errors that
	// can occur within the window before the circuit
	// breaker opens.  The default threshold is 5 errors.
	// The minimum value is 1 error.
	Threshold uint32

	// LockOut is the length of time that a circuit breaker
	// is forced open before attempting to throttle.
	// If no lockout is provided, the circuit breaker will
	// transition to a throttled state only after its error
	// count is at or below the threshold.  When a circuit
	// breaker is open, all requests are rejected and no
	// new errors are recorded.
	LockOut time.Duration

	// IgnoreContext will prevent context cancellation to
	// propagate to any in-flight Run functions
	IgnoreContext bool

	// InterpolationFunc is the function used to determine
	// the chance of a request being throttled during the
	// backoff period.  By default, Linear interpolation is used.
	InterpolationFunc InterpolationFunc
}

// Breaker is the circuit breaker implementation for this package
type Breaker struct {
	name            string            // Circuit Breaker name
	timeout         time.Duration     // Timeout for Run func
	baudrate        time.Duration     // Polling rate to recalculate error counts
	backoff         time.Duration     // Length of time the breaker is throttled
	lockout         time.Duration     // Length of time a breaker is locked out once it opens
	window          time.Duration     // Window of time to look for errors (e.g. 5 errors in 10 mins)
	threshold       uint32            // Maximum number of errors allowed to occur in window
	state           uint32            // Current state
	lockCreated     int64             // Unix nano timestamp of lock creation time
	lockCancel      []chan struct{}   // Signaling channel to stop lock timer
	throttleCreated int64             // Unix nano timestamp of throttle creation time
	throttleChance  uint32            // Chance of a request being throttled during backoff
	throttleCancel  []chan struct{}   // Signaling channels to stop throttle backoff
	closedSince     int64             // Unix nano timestamp of last closed time (or creation)
	initialized     bool              // Flag to check if the breaker was initialized via NewBreaker
	ignoreContext   bool              // If true, will not propagate context cancellation
	stateMX         sync.Mutex        // Mutex around state change
	calcTicker      *time.Ticker      // Internal ticker for recalculations, controlled by baud rate
	unlocker        *time.Timer       // Timer for unlocking the circuit breaker when lockout is set
	tracker         errTracker        // Error tracker
	interpolate     InterpolationFunc // Function used to interpolate throttling chance
	rando           *rand.Rand        // Internal randomizer
}

// NewBreaker will create a new Breaker using the
// supplied options.  All new Breaker instances
// MUST be created with this function.  All calls
// to Run using a Breaker created elsewhere will
// fail immediately.
func NewBreaker(opts BreakerOptions) *Breaker {
	b := &Breaker{
		name:        opts.Name,
		timeout:     opts.Timeout,
		baudrate:    opts.BaudRate,
		backoff:     opts.BackOff,
		window:      opts.Window,
		threshold:   opts.Threshold,
		lockout:     opts.LockOut,
		interpolate: opts.InterpolationFunc,
	}
	if b.name == "" {
		function, file, line, _ := runtime.Caller(1)
		b.name = strings.ReplaceAll(
			strings.ReplaceAll(
				fmt.Sprintf(
					"func_%s_file_%s_line_%d",
					runtime.FuncForPC(function).Name(),
					path.Base(file),
					line,
				), ".go", ""),
			".", "_",
		)
	}
	if b.timeout == 0 {
		b.timeout = DefaultTimeout
	}
	if b.baudrate == 0 {
		b.baudrate = DefaultBaudRate
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
	if b.threshold == 0 {
		b.threshold = DefaultThreshold
	}
	if b.threshold < minimumThreshold {
		b.threshold = minimumThreshold
	}
	if b.interpolate == nil {
		b.interpolate = Linear
	}

	nowNano := time.Now().UnixNano()
	b.tracker = newErrTracker(b.window)
	b.calcTicker = time.NewTicker(b.baudrate)
	b.closedSince = nowNano
	b.rando = rand.New(rand.NewSource(nowNano))
	go func(b *Breaker) {
		for {
			select {
			case <-b.calcTicker.C:
				b.calc()
			}
		}
	}(b)
	b.initialized = true
	return b
}

// get the lock status
func (b *Breaker) lockStatus() (time.Time, bool) {
	l := atomic.LoadInt64(&b.lockCreated)
	if l == 0 {
		return time.Time{}, false
	}
	return timeFromNS(l), true
}

// make lock
func (b *Breaker) setLocked(is bool) {
	if b.lockout == 0 {
		return
	}

	dumpf("\nsetting locked: %t", is)
	// if there is a current unlock timer, this will clear it.
	// this is a noop if the lockout is false
	// if setting false, just swap and exit

	if !is {
		atomic.SwapInt64(&b.lockCreated, 0)
		for i := range b.lockCancel {
			b.lockCancel[i] <- struct{}{}
		}
		b.lockCancel = []chan struct{}(nil)
		return
	}

	// setting true, start a new unlocker
	nowNano := time.Now().UnixNano()
	atomic.SwapInt64(&b.lockCreated, nowNano)
	cancel := make(chan struct{}, 1)
	b.lockCancel = append(b.lockCancel, cancel)

	dumpf("\nlocking out for %v", b.lockout)
	go func(b *Breaker, lockID int64, cancel chan struct{}) {
		t := time.NewTimer(b.lockout)
		select {
		case <-t.C:
			// only swap if we are unlocking from our lock
			if atomic.CompareAndSwapInt64(&b.lockCreated, lockID, 0) {
				dumps("\nlock released")
			}
			return
		case <-cancel:
			dumps("unlocker canceled")
			return
		}
	}(b, nowNano, cancel)
}

// get the throttle status
func (b *Breaker) throttledStatus() (time.Time, bool) {
	l := atomic.LoadInt64(&b.throttleCreated)
	if l == 0 {
		return time.Time{}, false
	}
	return timeFromNS(l), true
}

// make throttled
func (b *Breaker) setThrottled(is bool) {
	dumpf("\nsetting throttled: %t", is)

	if !is {
		atomic.SwapInt64(&b.throttleCreated, 0)
		atomic.SwapUint32(&b.throttleChance, 0)
		for i := range b.throttleCancel {
			b.throttleCancel[i] <- struct{}{}
		}
		b.throttleCancel = []chan struct{}(nil)
		return
	}

	nowNano := time.Now().UnixNano()
	atomic.SwapInt64(&b.throttleCreated, nowNano)
	atomic.SwapUint32(&b.throttleChance, 100)

	t := time.NewTicker(b.backoff / 100)
	cancel := make(chan struct{}, 1)
	b.throttleCancel = append(b.throttleCancel, cancel)

	dumpf("\nthrottling for %v", b.backoff)
	go func(b *Breaker, t *time.Ticker, cancel chan struct{}) {
		i := 1
		for {
			select {
			case <-t.C:
				// Here we run the interpolation function a maximum of
				// 100 times.  If the circuit breaker goes from throttled
				// to open, this ticker is stopped elsewhere.
				atomic.SwapUint32(&b.throttleChance, b.interpolate(i))
				i++
				if i >= 100 {
					// The backoff has completed without reopening the
					// circuit breaker.  Here we will close the circuit breaker.
					t.Stop()
					b.changeStateTo(internalClosed)
					return
				}
			case <-cancel:
				dumps("throttle canceled")
				return
			}
		}
	}(b, t, cancel)
}

// get the closed status
func (b *Breaker) closedStatus() (time.Time, bool) {
	l := atomic.LoadInt64(&b.closedSince)
	if l == 0 {
		return time.Time{}, false
	}
	return timeFromNS(l), true
}

// make closed
func (b *Breaker) setClosed(is bool) {
	dumpf("\nsetting closed: %t", is)
	if is {
		atomic.SwapInt64(&b.closedSince, time.Now().UnixNano())
		return
	}
	atomic.SwapInt64(&b.closedSince, 0)
}

// record state transition
func (b *Breaker) changeStateTo(to uint32) {
	b.stateMX.Lock()
	defer b.stateMX.Unlock()
	// Possible state transitions:
	//    - Closed to Open
	//    - Open to Throttled
	//    - Throttled to Open
	from := atomic.SwapUint32(&b.state, to)
	if from == to {
		return
	}
	switch to {
	case internalOpen:
		b.setLocked(true)
	case internalThrottled:
		b.setThrottled(true)
	case internalClosed:
		b.setClosed(true)
	}

	switch from {
	case internalThrottled:
		b.setThrottled(false)
	case internalClosed:
		b.setClosed(false)
	}
}

// calculate error frame
func (b *Breaker) calc() {
	state := atomic.LoadUint32(&b.state)
	switch state {
	case internalClosed:
		if b.tracker.size() > b.threshold {
			b.changeStateTo(internalOpen)
		}
	case internalThrottled:
		if b.tracker.size() > b.threshold {
			b.changeStateTo(internalOpen)
		}
	case internalOpen:
		// we're locked, nothing to do
		if _, locked := b.lockStatus(); locked {
			return
		}
		// error density needs to decay a bit more
		if b.tracker.size() > b.threshold {
			return
		}
		b.changeStateTo(internalThrottled)
	}
}

// record successful requests
func (b *Breaker) recordNonFailure() {
	if atomic.LoadUint32(&b.state) != internalThrottled {
		return
	}
}

func (b *Breaker) applyThrottle() error {
	chance := atomic.LoadUint32(&b.throttleChance)
	if rand.New(rand.NewSource(time.Now().UnixNano())).Uint32()%100 >= chance {
		return nil
	}
	//if b.rando.Uint32()%100 >= chance {
	//	return nil
	//}

	return StateThrottledError
}

// determine if Run can continue
func (b *Breaker) preFlight(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	state := atomic.LoadUint32(&b.state)
	switch state {
	case internalOpen:
		return StateOpenError
	case internalThrottled:
		// this may return an error immediately, or allow the runner to
		// continue, depending on the error rate and the back off timing
		return b.applyThrottle()
	case internalClosed:
		return nil
	default:
		return StateUnknownError
	}
}

// Run wraps the execution of function f
// - if the circuit breaker is open, this function will return
//   nil immediately with a circuit breaker open error
// - if the circuit breaker is throttled, only a subset of requests
//   will be attempted. Rejected requests will return nil and a circuit
//   breaker throttled error
func (b *Breaker) Run(ctx context.Context, f func(context.Context) (interface{}, error)) (interface{}, error) {
	if !b.initialized {
		return nil, NotInitializedError
	}

	if err := b.preFlight(ctx); err != nil {
		return nil, err
	}

	type result struct {
		value interface{}
		err   error
	}

	rChan := make(chan result, 1)
	timeout := time.NewTimer(b.timeout)
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	go func(ctx context.Context) {
		v, e := f(ctx)
		rChan <- result{
			value: v,
			err:   e,
		}
	}(ctx)

	for {
		select {
		// circuit breaker has timed out
		case <-timeout.C:
			if !b.ignoreContext {
				cancel()
			}
			b.tracker.incr()
			return nil, TimeoutError
		// f() has returned values
		case r := <-rChan:
			timeout.Stop()
			if r.err != nil {
				if !b.ignoreContext {
					cancel()
				}
				b.tracker.incr()
			} else {
				b.recordNonFailure()
			}
			return r.value, r.err
		}
	}
}

// State returns the current state of the circuit breaker
func (b *Breaker) State() State {
	return State(atomic.LoadUint32(&b.state))
}

// Size gets the number of errors present in the
// current tracking window
func (b *Breaker) Size() int {
	return int(b.tracker.size())
}

// Snapshot will get a current snapshot of the circuit breaker
func (b *Breaker) Snapshot() BreakerState {
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
		if since, ok := b.lockStatus(); ok {
			bs.Opened = &since
			ends := since.Add(b.lockout)
			bs.LockoutEnds = &ends
		}
	}

	return bs
}

func timeFromNS(ns int64) time.Time {
	u := ns / 1e9
	return time.Unix(u, ns-u*1e9)
}

func dump(i interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, "%#v\n", i)
}

func dumpf(f string, i ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, f+"\n", i...)
}

func dumps(s string) {
	_, _ = fmt.Fprintln(os.Stderr, s)
}
