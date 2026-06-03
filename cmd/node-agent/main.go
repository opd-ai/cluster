// Command node-agent is a long-running supervisor and HTTP API server
// that manages local role processes, broadcasts discovery beacons,
// and serves metrics to the gateway and console.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/opd-ai/cluster/internal/discovery"
	"github.com/opd-ai/cluster/internal/inventory"
	"github.com/opd-ai/cluster/internal/nodeapi"
)

func main() {
	rolesStr := flag.String("roles", "", "Comma-separated list of roles (e.g., 'chat,image-generation')")
	noReconcile := flag.Bool("no-reconcile", false, "Do not reconcile discovered nodes into inventory YAML")
	inventoryPath := flag.String("inventory", "cluster/inventory.yaml", "Path to cluster inventory file")
	hostname := flag.String("hostname", "", "Override node hostname (default: OS hostname)")
	address := flag.String("address", "", "Node address/IP (required)")
	vramGB := flag.Int("vram-gb", 0, "Total VRAM in GB")
	ramGB := flag.Int("ram-gb", 0, "Total RAM in GB")
	flag.Parse()

	if *rolesStr == "" {
		log.Fatal("--roles is required")
	}
	if *address == "" {
		log.Fatal("--address is required")
	}

	roles := strings.Split(*rolesStr, ",")
	for i := range roles {
		roles[i] = strings.TrimSpace(roles[i])
	}

	if *hostname == "" {
		h, err := os.Hostname()
		if err != nil {
			log.Fatalf("failed to get hostname: %v", err)
		}
		*hostname = h
	}

	// Build beacon message
	beaconSeq := atomic.Int32{}
	beaconSeq.Store(0)

	msgFunc := func() nodeapi.BeaconMessage {
		seq := beaconSeq.Add(1)
		return nodeapi.BeaconMessage{
			Version:  1,
			Hostname: *hostname,
			Address:  *address,
			Roles:    roles,
			Services: buildServices(roles),
			Arch:     "amd64",
			OS:       "linux",
			VRAMGB:   *vramGB,
			RamGB:    *ramGB,
			SeqNum:   int(seq),
		}
	}

	// Start beacon sender
	beacon, err := discovery.NewBeacon(10*time.Second, msgFunc)
	if err != nil {
		log.Fatalf("failed to create beacon: %v", err)
	}
	beacon.Start()
	defer beacon.Stop()

	// Start discovery listener (optional, for peer discovery)
	listener, err := discovery.NewListener(100)
	if err != nil {
		log.Printf("warning: failed to create listener: %v", err)
	} else {
		listener.Start()
		defer listener.Stop()
	}

	// Optional: start reconciler
	var reconciler *discovery.Reconciler
	if !*noReconcile {
		reconciler = discovery.NewReconciler(*inventoryPath)
		// Process discovered beacons
		if listener != nil {
			go func() {
				for msg := range listener.MessagesCh() {
					// Only reconcile other nodes (not self)
					if msg.Address != *address {
						if err := reconciler.Merge(msg); err != nil {
							log.Printf("error merging beacon from %s: %v", msg.Hostname, err)
						}
					}
				}
			}()
		}
	}

	// Start HTTP API server
	mux := chi.NewRouter()
	handlers := &apiHandlers{
		hostname:   *hostname,
		address:    *address,
		roles:      roles,
		vramGB:     *vramGB,
		ramGB:      *ramGB,
		peers:      make([]nodeapi.PeerRecord, 0),
		peersMu:    &sync.RWMutex{},
		listener:   listener,
	}

	mux.Get("/api/v1/info", handlers.handleInfo)
	mux.Get("/api/v1/health", handlers.handleHealth)
	mux.Get("/api/v1/metrics", handlers.handleMetrics)
	mux.Get("/api/v1/peers", handlers.handlePeers)

	srv := &http.Server{
		Addr:    ":9977",
		Handler: mux,
	}

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		log.Printf("Starting node-agent on %s with roles: %v", srv.Addr, roles)
		errCh <- srv.ListenAndServe()
	}()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	case sig := <-sigCh:
		log.Printf("received signal: %v", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}
}

type apiHandlers struct {
	hostname string
	address  string
	roles    []string
	vramGB   int
	ramGB    int
	peers    []nodeapi.PeerRecord
	peersMu  *sync.RWMutex
	listener *discovery.Listener
}

func (h *apiHandlers) handleInfo(w http.ResponseWriter, r *http.Request) {
	info := nodeapi.NodeInfo{
		Hostname:    h.hostname,
		Address:     h.address,
		Roles:       h.roles,
		Services:    buildServices(h.roles),
		Arch:        "amd64",
		OS:          "linux",
		Accelerator: "nvidia",
		VRAMGB:      h.vramGB,
		RamGB:       h.ramGB,
		DiskGB:      0,
		VRAMBudget:  make(map[string]int),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (h *apiHandlers) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Return per-role health status
	perRole := make(map[string]nodeapi.RoleHealth)
	for _, role := range h.roles {
		perRole[role] = nodeapi.RoleHealth{
			Role:       role,
			ProcessUp:  true, // TODO: actually check process status
			ModelReady: true, // TODO: actually check model readiness
			LastProbed: time.Now(),
		}
	}

	health := nodeapi.HealthReport{
		Timestamp: time.Now(),
		PerRole:   perRole,
		Healthy:   true,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func (h *apiHandlers) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// Return per-role metrics
	perRole := make(map[string]nodeapi.RoleMetrics)
	for _, role := range h.roles {
		perRole[role] = nodeapi.RoleMetrics{
			Role:           role,
			VRAMUsedMB:     0, // TODO: read from nvidia-smi or similar
			VRAMTotalMB:    int64(h.vramGB * 1024),
			VRAMPct:        0,
			QueueDepth:     0, // TODO: get from role process
			RequestsPerSec: 0,
		}
	}

	metrics := nodeapi.NodeMetricsExt{
		Timestamp: time.Now(),
		CPUPct:    0, // TODO: read from /proc/stat
		MemPct:    0, // TODO: read from /proc/meminfo
		PerRole:   perRole,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

func (h *apiHandlers) handlePeers(w http.ResponseWriter, r *http.Request) {
	h.peersMu.RLock()
	peers := make([]nodeapi.PeerRecord, len(h.peers))
	copy(peers, h.peers)
	h.peersMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(peers)
}

func buildServices(roles []string) []inventory.ServiceBinding {
	// Map roles to ports based on PLAN specification
	portMap := map[string]string{
		"chat":               "11434",
		"image-generation":   "7860",
		"training":           "7861",
		"embeddings":         "11434",
		"node-agent":         "9977",
	}

	var services []inventory.ServiceBinding
	for _, role := range roles {
		if port, ok := portMap[role]; ok {
			services = append(services, inventory.ServiceBinding{
				Role: role,
				Port: port,
			})
		}
	}

	// Always add node-agent service
	services = append(services, inventory.ServiceBinding{
		Role: "node-agent",
		Port: "9977",
	})

	return services
}
