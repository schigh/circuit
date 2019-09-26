package circuit

import "time"

const (
	DefaultTimeout   = 3 * time.Second
	DefaultBaudRate  = 250 * time.Millisecond
	DefaultBackOff   = time.Minute
	DefaultWindow    = 10 * time.Minute
	DefaultThreshold = uint32(5)

	minimumWindow    = 5 * time.Second
	minimumBackoff   = time.Second
	minimumThreshold = uint32(1)
)

// State represents the circuit breaker's state
type State uint32

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

// String returns the state's string value
func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Throttled:
		return "throttled"
	case Open:
		return "open"
	}
	return "unknown"
}

func (s State) MarshalJSON() ([]byte, error) {
	switch s {
	case Closed:
		return []byte{'"', 'c', 'l', 'o', 's', 'e', 'd', '"'}, nil
	case Throttled:
		return []byte{'"', 't', 'h', 'r', 'o', 't', 't', 'l', 'e', 'd', '"'}, nil
	case Open:
		return []byte{'"', 'o', 'p', 'e', 'n', '"'}, nil
	}
	return []byte{'"', 'u', 'n', 'k', 'n', 'o', 'w', 'n', '"'}, nil
}
