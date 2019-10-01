package circuit

import (
	"context"
	"fmt"
	"math/rand"
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

// Runner is the function signature for calls to Run
type Runner func(context.Context) (interface{}, error)

// BreakerOptions contains configuration options for a circuit breaker
type BreakerOptions struct {
	// OpeningWillResetErrors will cause the error count to reset
	// when the circuit breaker opens.  If this is set true, all
	// blocked calls will come from the throttled backoff, unless
	// the circuit breaker has a lockout duration.
	OpeningWillResetErrors bool

	// IgnoreContext will prevent context cancellation to
	// propagate to any in-flight Run functions.
	IgnoreContext bool

	// Threshold is the maximum number of errors that
	// can occur within the window before the circuit
	// breaker opens. By default, one error will open
	// the circuit breaker.
	Threshold uint32

	// Timeout is the maximum duration that the Run func
	// can execute before timing out.  The default Timeout
	// is 3 seconds.
	Timeout time.Duration

	// BaudRate is the duration between error calculations.
	// The default value is 250ms.  The minimum BaudRate
	// is 10ms
	BaudRate time.Duration

	// BackOff is the duration that a circuit breaker is
	// throttled.  The default BackOff is 1 minute.
	// The minimum BackOff is 1 second.
	BackOff time.Duration

	// Window is the length of time checked for error
	// calculation. The default Window is 5 minutes.
	// The minimum Window is 5 seconds.
	Window time.Duration

	// LockOut is the length of time that a circuit breaker
	// is forced open before attempting to throttle.
	// If no lockout is provided, the circuit breaker will
	// transition to a throttled state only after its error
	// count is at or below the threshold.  While a circuit
	// breaker is open, all requests are rejected and no
	// new errors are recorded.
	LockOut time.Duration

	// Name is the circuit breaker name. If name is not provided,
	// a unique name will be created based on the caller to NewBreaker.
	Name string

	// InterpolationFunc is the function used to determine
	// the chance of a request being throttled during the
	// backoff period.  By default, Linear interpolation is used.
	InterpolationFunc InterpolationFunc

	// PreProcessors are functions that execute in order before
	// the Runner function is executed.  If a preprocessor
	// returns an error, the execution is canceled and the error
	// is returned from Run.
	PreProcessors []PreProcessor

	// PostProcessors are functions that execute in order after
	// the Runner has executed.  The return values from the
	// Runner are passed along to the first postprocessor.
	// If there are subsequent postprocessors, each will take
	// the return value of its predecessor.
	PostProcessors []PostProcessor
}

// Breaker is the circuit breaker implementation for this package
type Breaker struct {

	// switches
	initialized   bool // Flag to check if the breaker was initialized via NewBreaker
	ignoreContext bool // If true, will not propagate context cancellation
	openingResets bool // If true, the circuit breaker resets its error count upon opening

	// state
	threshold      uint32 // Maximum number of errors allowed to occur in window
	state          uint32 // Current state
	throttleChance uint32 // Chance of a request being throttled during backoff

	// event timestamps
	lockCreated     int64 // Unix nano timestamp of lock creation time
	throttleCreated int64 // Unix nano timestamp of throttle creation time
	closedSince     int64 // Unix nano timestamp of last closed time (or creation)

	// name
	name string // Circuit Breaker name

	// timings
	timeout  time.Duration // Timeout for Run func
	baudrate time.Duration // Polling rate to recalculate error counts
	backoff  time.Duration // Length of time the breaker is throttled
	lockout  time.Duration // Length of time a breaker is locked out once it opens
	window   time.Duration // Window of time to look for errors (e.g. 5 errors in 10 mins)

	// misc
	stateMX     sync.Mutex        // Mutex around state change
	tracker     errTracker        // Error tracker
	interpolate InterpolationFunc // Function used to interpolate throttling chance

	// orchestration
	currThrottles []chan struct{} // Signaling channels to stop throttle backoff
	stateChange   chan BreakerState

	// pre/post processing
	preprocessors  []PreProcessor
	postprocessors []PostProcessor
}

// NewBreaker will create a new Breaker using the
// supplied options.  All new Breaker instances
// MUST be created with this function.  All calls
// to Run using a Breaker created elsewhere will
// fail immediately.
func NewBreaker(opts BreakerOptions) *Breaker {
	b := &Breaker{
		name:           opts.Name,
		timeout:        opts.Timeout,
		baudrate:       opts.BaudRate,
		backoff:        opts.BackOff,
		window:         opts.Window,
		threshold:      opts.Threshold,
		lockout:        opts.LockOut,
		interpolate:    opts.InterpolationFunc,
		openingResets:  opts.OpeningWillResetErrors,
		ignoreContext:  opts.IgnoreContext,
		preprocessors:  opts.PreProcessors,
		postprocessors: opts.PostProcessors,
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
	if b.baudrate == 0 {
		b.baudrate = DefaultBaudRate
	}
	if b.baudrate < minimumBaudRate {
		b.baudrate = minimumBaudRate
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
	if b.interpolate == nil {
		b.interpolate = Linear
	}

	b.stateChange = make(chan BreakerState, 1)
	b.tracker = newErrTracker(b.window)
	now := time.Now()
	b.closedSince = now.UnixNano()
	go func(b *Breaker) {
		t := time.NewTicker(b.baudrate)
		for {
			select {
			case <-t.C:
				b.calc()
			}
		}
	}(b)
	b.initialized = true

	b.stateChange <- BreakerState{
		Name:        b.name,
		State:       Closed,
		ClosedSince: &now,
	}

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

// open the circuit breaker and lock it out if enabled
func (b *Breaker) setLocked(is bool) {
	// if resetting the counter is enabled, it is done regardless of lockout
	b.tracker.reset(is && b.openingResets)

	if b.lockout == 0 {
		return
	}

	// setting open to false
	if !is {
		atomic.SwapInt64(&b.lockCreated, 0)
		return
	}

	// setting true, start a new unlocker
	nowNano := time.Now().UnixNano()
	atomic.SwapInt64(&b.lockCreated, nowNano)

	go func(b *Breaker, lockID int64) {
		t := time.NewTimer(b.lockout)
		<-t.C
		atomic.CompareAndSwapInt64(&b.lockCreated, lockID, 0)
	}(b, nowNano)
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
	if !is {
		atomic.SwapInt64(&b.throttleCreated, 0)
		atomic.SwapUint32(&b.throttleChance, 0)

		// cancelChan any existing backoff timers
		for i := range b.currThrottles {
			b.currThrottles[i] <- struct{}{}
		}

		// discard backoff
		b.currThrottles = []chan struct{}(nil)
		return
	}

	nowNano := time.Now().UnixNano()
	atomic.SwapInt64(&b.throttleCreated, nowNano)
	atomic.SwapUint32(&b.throttleChance, 100)

	cancelChan := make(chan struct{}, 1)
	b.currThrottles = append(b.currThrottles, cancelChan)

	t := time.NewTicker(b.backoff / 100)
	go func(b *Breaker, t *time.Ticker, cancel chan struct{}) {
		i := 1
		for {
			select {
			case <-t.C:
				// Here we run the interpolation function a maximum of
				// 100 times.  If the circuit breaker goes from throttled
				// to open, this ticker is stopped elsewhere.
				atomic.SwapUint32(&b.throttleChance, b.interpolate(i))
				if i >= 100 {
					// The backoff has completed without reopening the
					// circuit breaker.  Here we will close the circuit breaker.
					t.Stop()
					b.changeStateTo(internalClosed)
					return
				}
				i++
			case <-cancel:
				t.Stop()
				return
			}
		}
	}(b, t, cancelChan)
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
	//    - Throttled to Closed
	from := atomic.SwapUint32(&b.state, to)
	if from == to {
		return
	}

	newState := BreakerState{
		Name:  b.name,
		State: State(to),
	}

	switch from {
	case internalOpen:
		b.setLocked(false)
	case internalThrottled:
		b.setThrottled(false)
	case internalClosed:
		b.setClosed(false)
	}

	switch to {
	case internalOpen:
		b.setLocked(true)
		ts, locked := b.lockStatus()
		if locked {
			newState.Opened = &ts
			end := ts.Add(b.lockout)
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

	// this will prevent a block if no one is listening on the other end
	select {
	case b.stateChange <- newState:
	default:
		break
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

func (b *Breaker) applyThrottle() error {
	chance := atomic.LoadUint32(&b.throttleChance)
	if rand.New(rand.NewSource(time.Now().UnixNano())).Uint32()%100 >= chance {
		return nil
	}

	return StateThrottledError
}

func (b *Breaker) preProcess(ctx context.Context, runner Runner) (context.Context, Runner, error) {
	var err error
	for i := range b.preprocessors {
		ctx, runner, err = b.preprocessors[i](ctx, runner)
		if err != nil {
			return ctx, nil, err
		}
	}
	return ctx, runner, nil
}

func (b *Breaker) postProcess(ctx context.Context, p interface{}, err error) (interface{}, error) {
	for i := range b.postprocessors {
		p, err = b.postprocessors[i](ctx, p, err)
	}
	return p, err
}

// determine if Run can continue
func (b *Breaker) checkFitness(ctx context.Context) error {
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

// Run wraps the execution of function runner
func (b *Breaker) Run(ctx context.Context, runner Runner) (interface{}, error) {
	if !b.initialized {
		return nil, NotInitializedError
	}

	// run any preprocessors
	var preErr error
	ctx, runner, preErr = b.preProcess(ctx, runner)
	if preErr != nil {
		return nil, preErr
	}

	// checkFitness determines if this invocation is allowed to happen
	if err := b.checkFitness(ctx); err != nil {
		return nil, err
	}

	type result struct {
		value interface{}
		err   error
	}

	resultChan := make(chan result, 1)
	timeout := time.NewTimer(b.timeout)
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	// run the runner
	go func(ctx context.Context) {
		v, e := runner(ctx)
		resultChan <- result{
			value: v,
			err:   e,
		}
	}(ctx)

	// return values
	var out interface{}
	var outErr error

	select {
	// circuit breaker has timed out
	case <-timeout.C:
		if !b.ignoreContext {
			cancel()
		}
		outErr = TimeoutError
	// runner has returned values
	case r := <-resultChan:
		{
			timeout.Stop()
			if r.err != nil {
				if !b.ignoreContext {
					cancel()
				}
			}
			out = r.value
			outErr = r.err
		}
	}

	// apply post processing
	out, outErr = b.postProcess(ctx, out, outErr)
	if outErr != nil {
		b.tracker.incr()
	}
	return out, outErr
}

// State returns the current state of the circuit breaker
func (b *Breaker) State() State {
	return State(atomic.LoadUint32(&b.state))
}

func (b *Breaker) StateChange() <-chan BreakerState {
	return b.stateChange
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
