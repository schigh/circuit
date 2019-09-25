package circuit

import (
	"strings"
	"sync/atomic"
	"time"
)

const (
	evictInterval = 500 * time.Millisecond
)

type errTracker struct {
	events map[int64]uint32
	window int64
	pipe   chan struct{}
	sz     *uint32
}

func newErrTracker(dur time.Duration) errTracker {
	e := errTracker{}
	e.events = make(map[int64]uint32)
	e.window = int64(dur)
	e.pipe = make(chan struct{})
	var sz uint32
	e.sz = &sz

	e.poll()

	return e
}

func (e errTracker) poll() {
	go func(e *errTracker) {
		t := time.NewTicker(evictInterval)
		window := e.window
		for {
			select {
			case <-t.C:
				e.evict(window)
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
	e.events[n]++
	atomic.AddUint32(e.sz, 1)
}

func (e errTracker) evict(window int64) {
	sz := atomic.LoadUint32(e.sz)
	if sz == 0 {
		return
	}

	// make a slice of entries to be evicted
	evictions := make([]int64, 0, sz)
	evictTime := time.Now().UnixNano() - window
	var diff uint32
	for k, v := range e.events {
		if k < evictTime {
			diff += v
			evictions = append(evictions, k)
		}
	}
	// bail if there are no evictions
	if diff == 0 {
		return
	}
	dumpf("\n\n%s", strings.Repeat("🦶", len(evictions)))

	// remove the evicted timestamps
	for i := range evictions {
		delete(e.events, evictions[i])
	}

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

func (e errTracker) reset(do bool) {
	if !do {
		return
	}
	keys := make([]int64, 0, len(e.events))
	for k := range e.events {
		keys = append(keys, k)
	}
	for i := range keys {
		delete(e.events, keys[i])
	}
	atomic.StoreUint32(e.sz, 0)
}
