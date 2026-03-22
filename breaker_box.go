package circuit

import "sync"

type BreakerBox struct {
	breakers    sync.Map
	createMu    sync.Mutex // serializes Create/LoadOrCreate to prevent TOCTOU races
	stateChange chan BreakerState
}

// NewBreakerBox will return a BreakerBox with all internals properly configured.
func NewBreakerBox() *BreakerBox {
	return &BreakerBox{
		stateChange: make(chan BreakerState, 16),
	}
}

// StateChange exposes the breaker state channel of the box.
// State changes from all breakers created via Create are forwarded here
// with full timing information (Opened, LockoutEnds, Throttled, etc).
func (bb *BreakerBox) StateChange() <-chan BreakerState {
	return bb.stateChange
}

// Load will fetch a circuit breaker by name if it exists
func (bb *BreakerBox) Load(name string) *Breaker {
	b, ok := bb.breakers.Load(name)
	if !ok {
		return nil
	}
	return b.(*Breaker)
}

// AddBYO will add a Breaker to the box, but the breaker's state changes
// will not be forwarded to the box's state change output.
// The breaker must have a name.
func (bb *BreakerBox) AddBYO(b *Breaker) error {
	if b.name == "" {
		return ErrUnnamedBreaker
	}
	bb.breakers.Store(b.name, b)
	return nil
}

// Create will generate a new circuit breaker with the supplied options and return it.
// If a breaker with the same name already exists in the box, it will be discarded.
// State changes are forwarded to the box's StateChange channel with full timing info.
func (bb *BreakerBox) Create(opts ...Option) (*Breaker, error) {
	b, err := NewBreaker(opts...)
	if err != nil {
		return nil, err
	}
	if b.name == "" {
		return nil, ErrUnnamedBreaker
	}
	b.boxStateChange = bb.stateChange
	bb.breakers.Store(b.name, b)

	return b, nil
}

// LoadOrCreate will attempt to load a circuit breaker by name. If the breaker doesn't exist, a
// new one with the supplied options will be created and returned.
// This method is safe for concurrent use — only one breaker will be created per name.
func (bb *BreakerBox) LoadOrCreate(name string, opts ...Option) (*Breaker, error) {
	// Fast path: check without lock
	if b, ok := bb.breakers.Load(name); ok {
		return b.(*Breaker), nil
	}

	// Slow path: serialize creation to prevent duplicate breakers
	bb.createMu.Lock()
	defer bb.createMu.Unlock()

	// Re-check after acquiring lock
	if b, ok := bb.breakers.Load(name); ok {
		return b.(*Breaker), nil
	}

	opts = append([]Option{WithName(name)}, opts...)
	return bb.Create(opts...)
}
