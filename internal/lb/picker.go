// Package lb provides load balancing strategies for routing requests to backend nodes.
package lb

import (
	"sync"
	"time"

	"github.com/opd-ai/cluster/internal/inventory"
)

// Picker is the interface for load balancing strategies.
type Picker interface {
	// Pick selects a backend for the given role, model, and optional hint.
	// If no suitable backend is available, returns nil.
	Pick(role, model, hint string) *BackendRecord
}

// Updater is an optional interface that Picker implementations may satisfy
// to receive backend list updates when the registry changes.
type Updater interface {
	Update(backends []*BackendRecord)
}

// BackendRecord represents a backend node capable of serving requests.
type BackendRecord struct {
	Address      string
	Roles        []string
	Services     []inventory.ServiceBinding // per-role ports
	Models       []string
	Healthy      bool
	QueueDepth   int
	LatencyEMAms float64
	LastSeen     time.Time
}

// WeightedRoundRobin is a simple round-robin picker that rotates through healthy backends.
type WeightedRoundRobin struct {
	mu       sync.Mutex
	backends []*BackendRecord
	index    int
}

// NewWeightedRoundRobin creates a new weighted round-robin picker.
func NewWeightedRoundRobin(backends []*BackendRecord) *WeightedRoundRobin {
	return &WeightedRoundRobin{
		backends: backends,
		index:    0,
	}
}

// Pick selects the next healthy backend in round-robin order.
func (w *WeightedRoundRobin) Pick(role, model, hint string) *BackendRecord {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.backends) == 0 {
		return nil
	}

	// Find healthy backends that support the requested role and model
	var candidates []*BackendRecord
	for _, b := range w.backends {
		if b.Healthy && hasRole(b, role) && supportsModel(b, model) {
			candidates = append(candidates, b)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Round-robin through candidates
	idx := w.index % len(candidates)
	w.index++
	return candidates[idx]
}

// Update refreshes the backend list.
func (w *WeightedRoundRobin) Update(backends []*BackendRecord) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.backends = backends
	if w.index >= len(backends) {
		w.index = 0
	}
}

// hasRole checks if a backend supports a given role.
func hasRole(b *BackendRecord, role string) bool {
	for _, r := range b.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// supportsModel checks if a backend supports a given model.
// An empty model string matches any backend (means any model is acceptable).
func supportsModel(b *BackendRecord, model string) bool {
	if model == "" {
		return true // any model is acceptable
	}
	for _, m := range b.Models {
		if m == model {
			return true
		}
	}
	return false
}
