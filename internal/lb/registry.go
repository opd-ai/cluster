// Package lb provides load balancing strategies for routing requests to backend nodes.
package lb

import (
	"fmt"
	"sync"
)

// BackendRegistry maintains a mapping of (role, model) -> []BackendRecord
// and provides methods to register, deregister, and pick backends.
type BackendRegistry struct {
	mu       sync.RWMutex
	backends map[string]*BackendRecord // keyed by address
	picker   Picker
}

// NewBackendRegistry creates a new backend registry with the given picker strategy.
func NewBackendRegistry(picker Picker) *BackendRegistry {
	return &BackendRegistry{
		backends: make(map[string]*BackendRecord),
		picker:   picker,
	}
}

// Register adds or updates a backend in the registry.
func (r *BackendRegistry) Register(backend *BackendRecord) error {
	if backend.Address == "" {
		return fmt.Errorf("backend address is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.backends[backend.Address] = backend
	r.updatePicker()
	return nil
}

// Deregister removes a backend from the registry.
func (r *BackendRegistry) Deregister(address string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.backends, address)
	r.updatePicker()
	return nil
}

// Pick selects a backend for the given role and model using the configured picker strategy.
func (r *BackendRegistry) Pick(role, model, hint string) *BackendRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.picker.Pick(role, model, hint)
}

// GetAll returns a snapshot of all registered backends.
func (r *BackendRegistry) GetAll() []*BackendRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []*BackendRecord
	for _, b := range r.backends {
		out = append(out, b)
	}
	return out
}

// GetByAddress returns a backend by its address.
func (r *BackendRegistry) GetByAddress(address string) *BackendRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.backends[address]
}

// updatePicker refreshes the picker with the current backend list.
// Caller must hold the mutex.
func (r *BackendRegistry) updatePicker() {
	var out []*BackendRecord
	for _, b := range r.backends {
		out = append(out, b)
	}

	// Update picker if it implements the Updater interface
	if u, ok := r.picker.(Updater); ok {
		u.Update(out)
	}
}

// SetPicker changes the load balancing strategy.
func (r *BackendRegistry) SetPicker(picker Picker) error {
	if picker == nil {
		return fmt.Errorf("picker is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.picker = picker
	r.updatePicker()
	return nil
}
