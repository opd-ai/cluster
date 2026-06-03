// Package lb provides load balancing strategies for routing requests to backend nodes.
package lb

import (
	"sync"
)

// LatencyEWMA selects backends based on exponentially weighted moving average of latency.
// Lower latency backends are preferred.
type LatencyEWMA struct {
	mu       sync.RWMutex
	backends []*BackendRecord
	alpha    float64 // exponential weight (0-1), higher = more weight to recent samples
}

// NewLatencyEWMA creates a new latency EWMA picker with the given smoothing factor.
func NewLatencyEWMA(backends []*BackendRecord, alpha float64) *LatencyEWMA {
	if alpha < 0 || alpha > 1 {
		alpha = 0.2 // default
	}
	return &LatencyEWMA{
		backends: backends,
		alpha:    alpha,
	}
}

// Pick selects the backend with the lowest latency EMA among healthy backends supporting the role.
func (l *LatencyEWMA) Pick(role, model, hint string) *BackendRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if len(l.backends) == 0 {
		return nil
	}

	var best *BackendRecord
	minLatency := float64(^uint(0) >> 1) // max float

	for _, b := range l.backends {
		if !b.Healthy || !hasRole(b, role) {
			continue
		}

		if b.LatencyEMAms < minLatency {
			best = b
			minLatency = b.LatencyEMAms
		}
	}

	return best
}

// UpdateLatency updates the latency EMA for a backend.
// Typically called after observing a request's round-trip time.
func (l *LatencyEWMA) UpdateLatency(backend *BackendRecord, latencyMs float64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	backend.LatencyEMAms = l.alpha*latencyMs + (1-l.alpha)*backend.LatencyEMAms
}

// Update refreshes the backend list.
func (l *LatencyEWMA) Update(backends []*BackendRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.backends = backends
}
