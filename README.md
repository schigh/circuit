# circuit
--
    import "."


## Usage

```go
const (
	DefaultTimeout   = 3 * time.Second
	DefaultBaudRate  = 250 * time.Millisecond
	DefaultBackOff   = time.Minute
	DefaultWindow    = 10 * time.Minute
	DefaultThreshold = uint32(5)
)
```

```go
var (
	NotInitializedError = errors.New("circuit: breaker must be instantiated with NewBreaker")
	TimeoutError        = errors.New("circuit: breaker timed out")
	StateUnknownError   = errors.New("circuit: unknown state")
	StateOpenError      = errors.New("circuit: the circuit breaker is open")
	StateThrottledError = errors.New("circuit: breaker is throttled")
)
```

#### func  EaseInOut

```go
func EaseInOut(tick int) uint32
```
EaseInOut will block most requests initially, then pass at a steep rate,
eventually slowing down the pass rate

#### func  Exponential

```go
func Exponential(tick int) uint32
```
Exponential backoff will reduce the number of blocks drastically at first,
gradually slowing the rate

#### func  Linear

```go
func Linear(tick int) uint32
```
Linear backoff will return a probability directly proportional to the current
tick

#### func  Logarithmic

```go
func Logarithmic(tick int) uint32
```
Logarithmic backoff will block most initial requests and increase the rate of
passes at a similar rate after the middle point in the curve is reached

#### type Breaker

```go
type Breaker struct {
}
```

Breaker is the circuit breaker implementation for this package

#### func  NewBreaker

```go
func NewBreaker(opts BreakerOptions) *Breaker
```
NewBreaker will create a new Breaker using the supplied options. All new Breaker
instances MUST be created with this function. All calls to Run using a Breaker
created elsewhere will fail immediately.

#### func (*Breaker) Run

```go
func (b *Breaker) Run(ctx context.Context, f func(context.Context) (interface{}, error)) (interface{}, error)
```
Run wraps the execution of function f - if the circuit breaker is open, this
function will return

    nil immediately with a circuit breaker open error

- if the circuit breaker is throttled, only a subset of requests

    will be attempted. Rejected requests will return nil and a circuit
    breaker throttled error

#### func (*Breaker) Size

```go
func (b *Breaker) Size() int
```
Size gets the number of errors present in the current tracking window

#### func (*Breaker) Snapshot

```go
func (b *Breaker) Snapshot() BreakerState
```
Snapshot will get a current snapshot of the circuit breaker

#### func (*Breaker) State

```go
func (b *Breaker) State() State
```
State returns the current state of the circuit breaker

#### type BreakerOptions

```go
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

	// OpeningWillResetErrors will cause the error count to reset
	// when the circuit breaker opens.  If this is set true, all
	// blocked calls will come from the throttled backoff, unless
	// the circuit breaker has a lockout duration
	OpeningWillResetErrors bool

	// IgnoreContext will prevent context cancellation to
	// propagate to any in-flight Run functions
	IgnoreContext bool

	// InterpolationFunc is the function used to determine
	// the chance of a request being throttled during the
	// backoff period.  By default, Linear interpolation is used.
	InterpolationFunc InterpolationFunc
}
```

BreakerOptions contains configuration options for a circuit breaker

#### type BreakerState

```go
type BreakerState struct {
	Name        string     `json:"string"`
	State       State      `json:"state"`
	ClosedSince *time.Time `json:"closed_since,omitempty"`
	Opened      *time.Time `json:"opened,omitempty"`
	LockoutEnds *time.Time `json:"lockout_ends,omitempty"`
	Throttled   *time.Time `json:"throttled,omitempty"`
	BackOffEnds *time.Time `json:"backoff_ends,omitempty"`
}
```

BreakerState represents a snapshot of a circuit breaker's internal state,
including timings for lockouts and backoff

#### func (BreakerState) String

```go
func (bs BreakerState) String() string
```

#### type InterpolationFunc

```go
type InterpolationFunc func(int) uint32
```

InterpolationFunc takes in a number within range [1 - 100] and returns a
probability that a request should be blocked based on that number. The
periodicity of this function is directly proportional to the backoff duration of
the circuit breaker, where frequency = backoff duration / 100 The interpolation
function will be called exactly 100 times during the backoff period, unless the
circuit breaker reopens. All backoff periods start by interpolating 1 and
increasing towards 100

#### type State

```go
type State uint32
```

State represents the circuit breaker's state

```go
const (
	// Closed indicates that the circuit is
	// functioning optimally
	Closed State = iota
	// Throttled indicates that the circuit
	// breaker is recovering from an open state
	Throttled
	// Open indicates that the circuit breaker
	// is currently open and rejecting all requests
	Open
)
```

#### func (State) String

```go
func (s State) String() string
```
String returns the state's string value
