package circuit

import "sync"

type BreakerBox struct {
	breakers    BreakerMap
	stateChange chan BreakerState
	funnel      chan BreakerState
}

func NewBreakerBox() *BreakerBox {
	breakerMap := breakerMap{
		mx:   &sync.RWMutex{},
		impl: make(map[string]*Breaker),
	}
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
		breakers:    breakerMap,
		stateChange: stateChange,
		funnel:      funnel,
	}
}

func (bb *BreakerBox) StateChange() <-chan BreakerState {
	return bb.stateChange
}

func (bb *BreakerBox) Load(name string) *Breaker {
	return bb.breakers.get(name)
}

func (bb *BreakerBox) Add(b *Breaker) {
	bb.breakers.set(b.name, b)
}

func (bb *BreakerBox) Create(opts BreakerOptions) *Breaker {
	if opts.Name == "" {
		return nil
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
	bb.breakers.set(b.name, b)

	return b
}

func (bb *BreakerBox) LoadOrCreate(opts BreakerOptions) *Breaker {
	if breaker := bb.breakers.get(opts.Name); breaker != nil {
		return breaker
	}
	return bb.Create(opts)
}
