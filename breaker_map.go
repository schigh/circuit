package circuit

import (
	"sync"
)

// breakerMap wraps map[string]*circuit.Breaker, and locks reads and writes with a mutex
type breakerMap struct {
	mx   *sync.RWMutex
	impl map[string]*Breaker
}

// get gets the *circuit.Breaker keyed by string.
func (b breakerMap) get(key string) (value *Breaker) {
	b.mx.RLock()
	defer b.mx.RUnlock()

	value = b.impl[key]

	return
}

// keys will return all keys in the breakerMap's internal map
func (b breakerMap) keys() (keys []string) {
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

// set will add an element to the breakerMap's internal map with the specified key
func (b breakerMap) set(key string, value *Breaker) {
	b.mx.Lock()
	defer b.mx.Unlock()

	b.impl[key] = value
}

// delete will remove a *circuit.Breaker from the map by key
func (b breakerMap) delete(key string) {
	b.mx.Lock()
	defer b.mx.Unlock()

	delete(b.impl, key)
}

// clear will remove all elements from the map
func (b breakerMap) clear() {
	b.mx.Lock()
	defer b.mx.Unlock()

	b.impl = make(map[string]*Breaker)
}
