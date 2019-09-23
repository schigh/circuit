// Code generated Thu, 05 Sep 2019 15:02:33 EDT by carto.  DO NOT EDIT.
package box

import (
	"sync"

	"github.com/schigh/circuit"
)

// breakerMap wraps map[string]*circuit.Breaker, and locks reads and writes with a mutex
type breakerMap struct {
	mx   sync.RWMutex
	impl map[string]*circuit.Breaker
}

// NewbreakerMap generates a new breakerMap with a non-nil map
func NewbreakerMap() *breakerMap {
	b := &breakerMap{}
	b.impl = make(map[string]*circuit.Breaker)

	return b
}

// Get gets the *circuit.Breaker keyed by string.
func (b *breakerMap) Get(key string) (value *circuit.Breaker) {
	b.mx.RLock()
	defer b.mx.RUnlock()

	value = b.impl[key]

	return
}

// Keys will return all keys in the breakerMap's internal map
func (b *breakerMap) Keys() (keys []string) {
	b.mx.RLock()
	defer b.mx.RUnlock()

	keys = make([]string, len(b.impl))
	var i int
	for k := range b.impl {
		keys[i] = k
		i++
	}

	return
}

// Set will add an element to the breakerMap's internal map with the specified key
func (b *breakerMap) Set(key string, value *circuit.Breaker) {
	b.mx.Lock()
	defer b.mx.Unlock()

	b.impl[key] = value
}

// Absorb will take all the keys and values from another breakerMap's internal map and
// overwrite any existing keys
func (b *breakerMap) Absorb(otherMap *breakerMap) {
	b.mx.Lock()
	otherMap.mx.RLock()
	defer otherMap.mx.RUnlock()
	defer b.mx.Unlock()

	for k, v := range otherMap.impl {
		b.impl[k] = v
	}
}

// AbsorbMap will take all the keys and values from another map and overwrite any existing keys
func (b *breakerMap) AbsorbMap(regularMap map[string]*circuit.Breaker) {
	b.mx.Lock()
	defer b.mx.Unlock()

	for k, v := range regularMap {
		b.impl[k] = v
	}
}

// Delete will remove a *circuit.Breaker from the map by key
func (b *breakerMap) Delete(key string) {
	b.mx.Lock()
	defer b.mx.Unlock()

	delete(b.impl, key)
}

// Clear will remove all elements from the map
func (b *breakerMap) Clear() {
	b.mx.Lock()
	defer b.mx.Unlock()

	b.impl = make(map[string]*circuit.Breaker)
}
