package circuit

import "fmt"

// Error represents a circuit breaker error with optional context.
type Error struct {
	msg         string
	BreakerName string
	State       State
}

func (e Error) Error() string {
	if e.BreakerName != "" {
		return fmt.Sprintf("%s [breaker=%s]", e.msg, e.BreakerName)
	}
	return e.msg
}

// Is supports errors.Is by comparing the base message only,
// ignoring contextual fields like BreakerName and State.
func (e Error) Is(target error) bool {
	t, ok := target.(Error)
	if !ok {
		return false
	}
	return e.msg == t.msg
}

// withContext returns a copy of the error with breaker context attached.
func (e Error) withContext(name string, state State) Error {
	return Error{msg: e.msg, BreakerName: name, State: state}
}

var (
	ErrNotInitialized = Error{msg: "circuit: breaker must be instantiated with NewBreaker"}
	ErrTimeout        = Error{msg: "circuit: breaker timed out"}
	ErrStateUnknown   = Error{msg: "circuit: unknown state"}
	ErrStateOpen      = Error{msg: "circuit: the circuit breaker is open"}
	ErrStateThrottled = Error{msg: "circuit: breaker is throttled"}
	ErrUnnamedBreaker = Error{msg: "circuit: breakers used in a breaker box must have a name"}
)
