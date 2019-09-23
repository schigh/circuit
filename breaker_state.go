package circuit

import "time"

// BreakerState represents a snapshot of a circuit breaker's
// internal state, including timings for lockouts and backoff
type BreakerState struct {
	Name        string     `json:"string"`
	State       State      `json:"state"`
	ClosedSince *time.Time `json:"closed_since,omitempty"`
	Opened      *time.Time `json:"opened,omitempty"`
	LockoutEnds *time.Time `json:"lockout_ends,omitempty"`
	Throttled   *time.Time `json:"throttled,omitempty"`
	BackOffEnds *time.Time `json:"backoff_ends,omitempty"`
}

func (bs BreakerState) String() string {
	return bs.State.String()
}
