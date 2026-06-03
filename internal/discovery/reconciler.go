// Package discovery provides peer discovery via UDP multicast beacons.
package discovery

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/opd-ai/cluster/internal/inventory"
	"github.com/opd-ai/cluster/internal/nodeapi"
	"gopkg.in/yaml.v3"
)

// Reconciler merges discovered nodes into the cluster inventory YAML file.
// It performs atomic writes (write-to-temp + rename) to avoid corruption.
type Reconciler struct {
	inventoryPath string
	mu            sync.RWMutex
	nodes         map[string]*inventory.Node // keyed by address
}

// NewReconciler creates a new reconciler for the given inventory file path.
func NewReconciler(inventoryPath string) *Reconciler {
	return &Reconciler{
		inventoryPath: inventoryPath,
		nodes:         make(map[string]*inventory.Node),
	}
}

// Merge adds or updates a discovered node in the in-memory inventory.
// If the address already exists, it merges roles and services;
// otherwise, it creates a new entry.
func (r *Reconciler) Merge(msg nodeapi.BeaconMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, exists := r.nodes[msg.Address]
	if !exists {
		// Create new node
		node = &inventory.Node{
			Hostname:    msg.Hostname,
			Address:     msg.Address,
			Roles:       msg.Roles,
			Services:    msg.Services,
			Arch:        msg.Arch,
			OS:          msg.OS,
			VramGB:      msg.VRAMGB,
			RamGB:       msg.RamGB,
			Labels:      make(map[string]string),
			VRAMBudget:  make(map[string]int),
		}
		r.nodes[msg.Address] = node
		return nil
	}

	// Merge roles (union)
	for _, role := range msg.Roles {
		node.AddRole(role)
	}

	// Update services (replace)
	node.Services = msg.Services

	// Update metadata (may have changed)
	node.Hostname = msg.Hostname
	node.Arch = msg.Arch
	node.OS = msg.OS
	node.VramGB = msg.VRAMGB
	node.RamGB = msg.RamGB

	return nil
}

// WriteInventory atomically writes the merged inventory to disk.
// Only discovered nodes are written; manually configured nodes in the existing
// inventory are preserved.
func (r *Reconciler) WriteInventory() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Read existing inventory to preserve manually configured fields
	var existingYAML struct {
		Nodes []inventory.Node `yaml:"nodes"`
	}

	data, err := ioutil.ReadFile(r.inventoryPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read inventory: %w", err)
	}

	if len(data) > 0 {
		err = yaml.Unmarshal(data, &existingYAML)
		if err != nil {
			return fmt.Errorf("unmarshal inventory: %w", err)
		}
	}

	// Merge discovered nodes into existing inventory
	nodeMap := make(map[string]*inventory.Node)
	for i := range existingYAML.Nodes {
		node := &existingYAML.Nodes[i]
		nodeMap[node.Address] = node
	}

	// Update or add discovered nodes
	for addr, discoveredNode := range r.nodes {
		if existing, ok := nodeMap[addr]; ok {
			// Merge discovered data into existing node
			existing.Hostname = discoveredNode.Hostname
			existing.Roles = discoveredNode.Roles
			existing.Services = discoveredNode.Services
			existing.Arch = discoveredNode.Arch
			existing.OS = discoveredNode.OS
			existing.VramGB = discoveredNode.VramGB
			existing.RamGB = discoveredNode.RamGB
			// Preserve manually set fields: SSHUser, Labels, VRAMBudget
		} else {
			// Add new discovered node
			nodeMap[addr] = discoveredNode
		}
	}

	// Rebuild nodes list
	var outNodes []inventory.Node
	for _, node := range nodeMap {
		outNodes = append(outNodes, *node)
	}

	outYAML := struct {
		Nodes []inventory.Node `yaml:"nodes"`
	}{
		Nodes: outNodes,
	}

	// Marshal to YAML
	outData, err := yaml.Marshal(outYAML)
	if err != nil {
		return fmt.Errorf("marshal inventory: %w", err)
	}

	// Write to temp file, then rename (atomic)
	dir := filepath.Dir(r.inventoryPath)
	tmpFile, err := ioutil.TempFile(dir, ".inventory-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	_, err = tmpFile.Write(outData)
	if err != nil {
		os.Remove(tmpFile.Name())
		return fmt.Errorf("write temp file: %w", err)
	}

	err = os.Rename(tmpFile.Name(), r.inventoryPath)
	if err != nil {
		os.Remove(tmpFile.Name())
		return fmt.Errorf("rename inventory: %w", err)
	}

	return nil
}

// GetNodes returns a snapshot of the merged nodes.
func (r *Reconciler) GetNodes() []inventory.Node {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []inventory.Node
	for _, node := range r.nodes {
		out = append(out, *node)
	}
	return out
}
