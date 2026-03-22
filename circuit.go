package circuit

import (
	"fmt"
	"strings"
	"time"
)

const (
	DefaultTimeout = 10 * time.Second
	DefaultBackOff = time.Minute
	DefaultWindow  = 5 * time.Minute

	minimumWindow  = 10 * time.Millisecond
	minimumBackoff = 10 * time.Millisecond
)

// State represents the circuit breaker's state
type State uint32

const (
	// Closed indicates that the circuit is functioning optimally
	Closed State = iota
	// Throttled indicates that the circuit breaker is recovering from an open state
	Throttled
	// Open indicates that the circuit breaker is currently open and rejecting all requests
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
		return []byte(`"closed"`), nil
	case Throttled:
		return []byte(`"throttled"`), nil
	case Open:
		return []byte(`"open"`), nil
	}
	return []byte(`"unknown"`), nil
}

func (s *State) UnmarshalJSON(data []byte) error {
	str := strings.Trim(string(data), `"`)
	switch str {
	case "closed":
		*s = Closed
	case "throttled":
		*s = Throttled
	case "open":
		*s = Open
	default:
		return fmt.Errorf("circuit: unknown state %q", str)
	}
	return nil
}
