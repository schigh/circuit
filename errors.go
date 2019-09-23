package circuit

import "errors"

var (
	NotInitializedError = errors.New("circuit: breaker must be instantiated with NewBreaker")
	TimeoutError        = errors.New("circuit: breaker timed out")
	StateUnknownError   = errors.New("circuit: unknown state")
	StateOpenError      = errors.New("circuit: the circuit breaker is open")
	StateThrottledError = errors.New("circuit: breaker is throttled")
)
