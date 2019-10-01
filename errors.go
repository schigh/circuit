package circuit

type Error struct {
	msg string
}

func (e Error) Error() string {
	return e.msg
}

var (
	NotInitializedError = Error{"circuit: breaker must be instantiated with NewBreaker"}
	TimeoutError        = Error{"circuit: breaker timed out"}
	StateUnknownError   = Error{"circuit: unknown state"}
	StateOpenError      = Error{"circuit: the circuit breaker is open"}
	StateThrottledError = Error{"circuit: breaker is throttled"}
	UnnamedBreakerError = Error{"circuit: breakers used in a breaker box must have a name"}
)
