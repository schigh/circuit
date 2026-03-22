package circuit

import "time"

// MetricsCollector is an optional interface for collecting circuit breaker metrics.
// Implement this interface and pass it via WithMetrics to instrument a breaker.
// All methods must be safe for concurrent use.
type MetricsCollector interface {
	// RecordSuccess is called when a request completes without error,
	// or when the error is classified as successful via IsSuccessful.
	RecordSuccess(breakerName string, duration time.Duration)

	// RecordError is called when a request fails. The error is provided
	// for classification in dashboards (e.g., by error type or code).
	RecordError(breakerName string, duration time.Duration, err error)

	// RecordTimeout is called when a request fails due to context deadline exceeded.
	// This is also counted as a RecordError call.
	RecordTimeout(breakerName string)

	// RecordStateChange is called on every state transition.
	RecordStateChange(breakerName string, from, to State)

	// RecordRejected is called when a request is rejected without execution.
	RecordRejected(breakerName string, state State)

	// RecordExcluded is called when an error is classified as excluded
	// via IsExcluded. Excluded errors are not tracked as successes or failures.
	RecordExcluded(breakerName string, err error)
}
