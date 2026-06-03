// Package lb provides load balancing strategies for routing requests to backend nodes.
package lb

import (
	"sync"
)

// LeastQueue selects backends based on the fewest pending requests (queue depth).
type LeastQueue struct {
	mu       sync.RWMutex
	backends []*BackendRecord
}

// NewLeastQueue creates a new least-queue picker.
func NewLeastQueue(backends []*BackendRecord) *LeastQueue {
	return &LeastQueue{
		backends: backends,
	}
}

// Pick selects the backend with the lowest queue depth among healthy backends supporting the role.
func (l *LeastQueue) Pick(role, model, hint string) *BackendRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if len(l.backends) == 0 {
		return nil
	}

	var best *BackendRecord
	minQueue := int(^uint(0) >> 1) // max int

	for _, b := range l.backends {
		if !b.Healthy || !hasRole(b, role) {
			continue
		}

		if b.QueueDepth < minQueue {
			best = b
			minQueue = b.QueueDepth
		}
	}

	return best
}

// Update refreshes the backend list.
func (l *LeastQueue) Update(backends []*BackendRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.backends = backends
}
