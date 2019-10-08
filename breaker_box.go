package circuit

import "sync"

type BreakerBox struct {
	breakers    sync.Map
	stateChange chan BreakerState
	funnel      chan BreakerState
}

// NewBreakerBox will return a BreakerBox with all internals properly configured.
// Use this function at all times when creating a new instance.
func NewBreakerBox() *BreakerBox {
	stateChange := make(chan BreakerState, 5)
	funnel := make(chan BreakerState)

	go func(stateChange, funnel chan BreakerState) {
		for {
			select {
			case state := <-funnel:
				select {
				case stateChange <- state:
				default:
					// no one's listening, don't block
				}
			}
		}
	}(stateChange, funnel)

	return &BreakerBox{
		breakers:    sync.Map{},
		stateChange: stateChange,
		funnel:      funnel,
	}
}

// StateChange exposes the breaker breaker state channel of the box
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
// will not be funneled to the box's state change output
func (bb *BreakerBox) AddBYO(b *Breaker) {
	bb.breakers.Store(b.name, b)
}

// Create will generate a new circuit breaker with the supplied options and return it.
// If a breaker with the same name already exists in the box, it will be discarded.
func (bb *BreakerBox) Create(opts BreakerOptions) (*Breaker, error) {
	if opts.Name == "" {
		return nil, UnnamedBreakerError
	}
	b := NewBreaker(opts)
	go func(b *Breaker, bb *BreakerBox) {
		for {
			select {
			case state := <-b.stateChange:
				bb.funnel <- state
			}
		}
	}(b, bb)
	bb.breakers.Store(b.name, b)

	return b, nil
}

// LoadOrCreate will attempt to load a circuit breaker by name.  If the breaker doesnt exist, a
// new one with the supplied options will be created and returned.
func (bb *BreakerBox) LoadOrCreate(opts BreakerOptions) (*Breaker, error) {
	b, ok := bb.breakers.Load(opts.Name)
	if ok {
		return b.(*Breaker), nil
	}

	return bb.Create(opts)
}
