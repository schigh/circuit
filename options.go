package circuit

import "time"

// Option configures a Breaker. Use the With* functions to create Options.
type Option func(*Breaker)

// WithName sets the circuit breaker's name.
// If not provided, a unique name is generated from the caller.
func WithName(name string) Option {
	return func(b *Breaker) {
		b.name = name
	}
}

// WithTimeout sets the maximum duration that the Run func
// can execute before timing out. The default is 10 seconds.
// The runner must respect context cancellation for timeouts to take effect.
func WithTimeout(d time.Duration) Option {
	return func(b *Breaker) {
		b.timeout = d
	}
}

// WithBackOff sets the duration that a circuit breaker is throttled.
// The default is 1 minute. The minimum is 10ms.
func WithBackOff(d time.Duration) Option {
	return func(b *Breaker) {
		b.backoff = d
	}
}

// WithWindow sets the length of time checked for error calculation.
// The default is 5 minutes. The minimum is 10ms.
func WithWindow(d time.Duration) Option {
	return func(b *Breaker) {
		b.window = d
	}
}

// WithThreshold sets the maximum number of errors that can occur
// within the window before the circuit breaker opens.
// By default, one error will open the circuit breaker.
func WithThreshold(n uint32) Option {
	return func(b *Breaker) {
		b.threshold = n
	}
}

// WithLockOut sets the length of time that a circuit breaker is
// forced open before attempting to throttle. If no lockout is
// provided, the circuit breaker will transition to a throttled
// state only after its error count is at or below the threshold.
func WithLockOut(d time.Duration) Option {
	return func(b *Breaker) {
		b.lockout = d
	}
}

// WithEstimationFunc sets the function used to determine the chance
// of a request being throttled during the backoff period.
// By default, Linear estimation is used.
func WithEstimationFunc(f EstimationFunc) Option {
	return func(b *Breaker) {
		b.estimate = f
	}
}

// WithOpeningResetsErrors causes the error count to reset when the
// circuit breaker opens. If set, all blocked calls will come from
// the throttled backoff, unless the circuit breaker has a lockout duration.
func WithOpeningResetsErrors(v bool) Option {
	return func(b *Breaker) {
		b.openingResets = v
	}
}

// WithMetrics sets an optional MetricsCollector for the breaker.
func WithMetrics(m MetricsCollector) Option {
	return func(b *Breaker) {
		b.metrics = m
	}
}

// WithOnStateChange sets a callback that is invoked on every state transition.
// The callback runs synchronously after the state mutex is released, so it
// does not block other requests from evaluating state. However, it is called
// inline on the goroutine that triggered the transition.
func WithOnStateChange(fn func(breakerName string, from, to State)) Option {
	return func(b *Breaker) {
		b.onStateChange = fn
	}
}

// WithIsSuccessful sets a callback to classify errors as successes.
// When this returns true for an error, the call is counted as a success
// and does not contribute to the error threshold (e.g., HTTP 404).
func WithIsSuccessful(fn func(error) bool) Option {
	return func(b *Breaker) {
		b.isSuccessful = fn
	}
}

// WithIsExcluded sets a callback to exclude errors from tracking entirely.
// When this returns true, the call is not counted as success or failure
// (e.g., context.Canceled from client disconnect).
func WithIsExcluded(fn func(error) bool) Option {
	return func(b *Breaker) {
		b.isExcluded = fn
	}
}
