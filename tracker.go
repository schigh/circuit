package circuit

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	evictInterval = 500 * time.Millisecond
)

type errTracker struct {
	mx     *sync.Mutex
	events map[int64]uint32
	window int64
	pipe   chan struct{}
	clock  *time.Ticker
	sz     *uint32
}

func newErrTracker(dur time.Duration) errTracker {
	e := errTracker{}
	e.mx = &sync.Mutex{}
	e.events = make(map[int64]uint32)
	e.window = int64(dur)
	e.pipe = make(chan struct{})
	e.clock = time.NewTicker(evictInterval)
	var sz uint32
	e.sz = &sz

	e.poll()

	return e
}

func (e errTracker) poll() {
	go func(e *errTracker) {
		for {
			select {
			case <-e.clock.C:
				e.evict()
			case <-e.pipe:
				e.record()
			}
		}
	}(&e)
}

//  send signal to record an error instance now
func (e errTracker) incr() {
	e.pipe <- struct{}{}
}

// record an error instance
func (e errTracker) record() {
	n := time.Now().UnixNano()
	e.mx.Lock()
	e.events[n]++
	atomic.AddUint32(e.sz, 1)
	e.mx.Unlock()
}

func (e errTracker) evict() {
	// bail if there are no events to track
	ml := len(e.events)
	if ml == 0 {
		return
	}

	e.mx.Lock()
	defer e.mx.Unlock()

	// make a slice of entries to be evicted
	evictions := make([]int64, 0, ml)
	evictTime := time.Now().UnixNano() - e.window
	var diff uint32
	for k, v := range e.events {
		if k < evictTime {
			diff += v
			evictions = append(evictions, k)
		}
	}
	// bail if there are no evictions
	if len(evictions) == 0 {
		return
	}

	// remove the evicted timestamps
	for i := range evictions {
		delete(e.events, evictions[i])
	}

	// set error size
	sz := atomic.LoadUint32(e.sz)
	// casting these larger to avoid loss of resolution
	newSz := int64(sz) - int64(diff)
	if newSz < 0 {
		newSz = 0
	}
	atomic.StoreUint32(e.sz, uint32(newSz))
}

func (e errTracker) size() uint32 {
	return atomic.LoadUint32(e.sz)
}
