package lb

import (
	"testing"

	"github.com/opd-ai/cluster/internal/inventory"
)

// TestLeastQueueLoadTest simulates 3 backends with different queue depths
// and verifies that the least-queue strategy routes away from high-queue backends.
func TestLeastQueueLoadTest(t *testing.T) {
	// Create 3 backends with different queue depths
	backend1 := &BackendRecord{
		Address:    "192.168.1.10",
		Roles:      []string{"chat"},
		Models:     []string{"llama2", "neural-chat"},
		Healthy:    true,
		QueueDepth: 1, // Low queue
		Services: []inventory.ServiceBinding{
			{Role: "chat", Port: "11434"},
		},
	}

	backend2 := &BackendRecord{
		Address:    "192.168.1.11",
		Roles:      []string{"chat"},
		Models:     []string{"llama2", "neural-chat"},
		Healthy:    true,
		QueueDepth: 10, // High queue — should be avoided
		Services: []inventory.ServiceBinding{
			{Role: "chat", Port: "11434"},
		},
	}

	backend3 := &BackendRecord{
		Address:    "192.168.1.12",
		Roles:      []string{"chat"},
		Models:     []string{"llama2", "neural-chat"},
		Healthy:    true,
		QueueDepth: 2, // Medium queue
		Services: []inventory.ServiceBinding{
			{Role: "chat", Port: "11434"},
		},
	}

	backends := []*BackendRecord{backend1, backend2, backend3}

	// Create a LeastQueue picker
	picker := NewLeastQueue(backends)

	// Test: Pick should select backend1 (queue depth = 1) when all are healthy
	selected := picker.Pick("chat", "llama2", "")
	if selected == nil {
		t.Fatal("picker returned nil; expected to select a backend")
	}
	if selected.Address != "192.168.1.10" {
		t.Errorf("expected backend1 (queue=1), got %s (queue=%d)", selected.Address, selected.QueueDepth)
	}

	// Test: Simulate increased queue depth for backend1
	// Now backend2 is still avoided (queue=10), but backend3 should be selected (queue=2)
	backend1.QueueDepth = 5
	picker.Update(backends) // refresh picker
	selected = picker.Pick("chat", "llama2", "")
	if selected == nil {
		t.Fatal("picker returned nil after queue update")
	}
	if selected.Address != "192.168.1.12" {
		t.Errorf("expected backend3 (queue=2), got %s (queue=%d)", selected.Address, selected.QueueDepth)
	}

	// Test: Backend with queue=10 should never be selected when alternatives exist
	for i := 0; i < 100; i++ {
		selected = picker.Pick("chat", "llama2", "")
		if selected == nil {
			t.Fatal("picker returned nil in loop")
		}
		if selected.Address == "192.168.1.11" {
			t.Errorf("iteration %d: picker selected high-queue backend (queue=10), should avoid it", i)
		}
	}

	// Test: If only high-queue backend is healthy, it should be selected
	backend1.Healthy = false
	backend3.Healthy = false
	picker.Update(backends)
	selected = picker.Pick("chat", "llama2", "")
	if selected == nil {
		t.Fatal("picker returned nil when only high-queue backend is healthy")
	}
	if selected.Address != "192.168.1.11" {
		t.Errorf("expected backend2 (only healthy), got %v", selected.Address)
	}

	// Test: Model filtering should work correctly
	backend1.Healthy = true
	backend3.Healthy = true
	backend2.Models = []string{"mistral"} // backend2 doesn't support llama2
	picker.Update(backends)
	for i := 0; i < 50; i++ {
		selected = picker.Pick("chat", "llama2", "")
		if selected == nil {
			t.Fatal("picker returned nil when filtering by model")
		}
		if selected.Address == "192.168.1.11" {
			t.Errorf("iteration %d: picker selected backend that doesn't support the model", i)
		}
	}

	t.Log("✓ LeastQueue load test passed: high-queue backends avoided when alternatives exist")
}

// TestLeastQueueRoleFiltering verifies that role filtering works correctly.
func TestLeastQueueRoleFiltering(t *testing.T) {
	backend1 := &BackendRecord{
		Address:    "192.168.1.10",
		Roles:      []string{"chat"},
		Models:     []string{"llama2"},
		Healthy:    true,
		QueueDepth: 1,
		Services: []inventory.ServiceBinding{
			{Role: "chat", Port: "11434"},
		},
	}

	backend2 := &BackendRecord{
		Address:    "192.168.1.11",
		Roles:      []string{"image-generation"},
		Models:     []string{"stable-diffusion"},
		Healthy:    true,
		QueueDepth: 1,
		Services: []inventory.ServiceBinding{
			{Role: "image-generation", Port: "7860"},
		},
	}

	backends := []*BackendRecord{backend1, backend2}
	picker := NewLeastQueue(backends)

	// Request for "chat" role should only get backend1
	selected := picker.Pick("chat", "", "")
	if selected == nil {
		t.Fatal("picker returned nil for chat role")
	}
	if selected.Address != "192.168.1.10" {
		t.Errorf("expected chat backend, got %s", selected.Address)
	}

	// Request for "image-generation" role should only get backend2
	selected = picker.Pick("image-generation", "", "")
	if selected == nil {
		t.Fatal("picker returned nil for image-generation role")
	}
	if selected.Address != "192.168.1.11" {
		t.Errorf("expected image-generation backend, got %s", selected.Address)
	}

	// Request for unknown role should get nil
	selected = picker.Pick("unknown-role", "", "")
	if selected != nil {
		t.Errorf("expected nil for unknown role, got %s", selected.Address)
	}

	t.Log("✓ LeastQueue role filtering test passed")
}
