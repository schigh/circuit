package circuit

import (
	"sync"
	"time"
)

type errTracker struct {
	mu            sync.Mutex
	events        map[int64]uint32
	window        int64
	sz            uint32
	lastEvictNano int64
}

const minEvictInterval = int64(50 * time.Millisecond)

func newErrTracker(dur time.Duration) *errTracker {
	return &errTracker{
		events: make(map[int64]uint32),
		window: int64(dur),
	}
}

// incr records an error instance.
func (e *errTracker) incr() {
	e.mu.Lock()
	n := time.Now().UnixNano()
	e.events[n]++
	e.sz++
	e.mu.Unlock()
}

// size returns the number of errors in the current window.
// Eviction is performed at most once per minEvictInterval to
// avoid O(n) map scans on every call under high throughput.
func (e *errTracker) size() uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now().UnixNano()
	if e.sz > 0 && now-e.lastEvictNano >= minEvictInterval {
		e.evict(now)
		e.lastEvictNano = now
	}
	return e.sz
}

// evict removes entries outside the window. Caller must hold the lock.
func (e *errTracker) evict(now int64) {
	evictTime := now - e.window
	var diff uint32
	for k, v := range e.events {
		if k < evictTime {
			diff += v
			delete(e.events, k)
		}
	}

	if diff == 0 {
		return
	}

	if diff > e.sz {
		e.sz = 0
	} else {
		e.sz -= diff
	}
}

// reset clears all tracked errors.
func (e *errTracker) reset(do bool) {
	if !do {
		return
	}
	e.mu.Lock()
	e.events = make(map[int64]uint32)
	e.sz = 0
	e.mu.Unlock()
}
