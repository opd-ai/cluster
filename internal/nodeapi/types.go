// Package nodeapi defines wire types for the node-agent HTTP API.
// These types are shared between cmd/node-agent (server) and clients
// (cmd/gateway, cmd/console, peer node-agents).
package nodeapi

import (
	"github.com/opd-ai/cluster/internal/inventory"
	"time"
)

// =========================================================================
// Node Discovery and Status
// =========================================================================

// NodeInfo is returned by GET /api/v1/info and contains static node metadata.
type NodeInfo struct {
	Hostname     string                   `json:"hostname"`
	Address      string                   `json:"address"`
	Roles        []string                 `json:"roles"`
	Services     []inventory.ServiceBinding `json:"services"` // per-role port bindings
	Arch         string                   `json:"arch"`
	OS           string                   `json:"os"`
	Accelerator  string                   `json:"accelerator"`
	VRAMGB       int                      `json:"vram_gb"`
	RamGB        int                      `json:"ram_gb"`
	DiskGB       int                      `json:"disk_gb"`
	VRAMBudget   map[string]int           `json:"vram_budget"` // VRAM allocation per role in MB
}

// HealthReport is returned by GET /api/v1/health and contains per-role liveness.
type HealthReport struct {
	Timestamp time.Time             `json:"timestamp"`
	PerRole   map[string]RoleHealth `json:"per_role"` // role -> health status
	Healthy   bool                  `json:"healthy"`
}

// RoleHealth represents the health of a single role on the node.
type RoleHealth struct {
	Role       string    `json:"role"`
	ProcessUp  bool      `json:"process_up"`  // daemon process is running
	ModelReady bool      `json:"model_ready"` // model/service is loaded
	Error      string    `json:"error,omitempty"`
	LastProbed time.Time `json:"last_probed"`
}

// NodeMetricsExt is returned by GET /api/v1/metrics and extends metrics with per-role breakdown.
type NodeMetricsExt struct {
	Timestamp   time.Time                  `json:"timestamp"`
	CPUPct      float64                    `json:"cpu_pct"`
	MemPct      float64                    `json:"mem_pct"`
	PerRole     map[string]RoleMetrics     `json:"per_role"` // role -> metrics
}

// RoleMetrics represents metrics for a single role.
type RoleMetrics struct {
	Role           string  `json:"role"`
	VRAMUsedMB     int64   `json:"vram_used_mb"`
	VRAMTotalMB    int64   `json:"vram_total_mb"`
	VRAMPct        float64 `json:"vram_pct"`
	QueueDepth     int     `json:"queue_depth"`
	RequestsPerSec float64 `json:"requests_per_sec,omitempty"`
}

// =========================================================================
// Peer Discovery
// =========================================================================

// PeerRecord represents a peer node known to this agent (from discovery).
type PeerRecord struct {
	Hostname   string    `json:"hostname"`
	Address    string    `json:"address"`
	Roles      []string  `json:"roles"`
	Services   []inventory.ServiceBinding `json:"services"`
	Healthy    bool      `json:"healthy"`
	LastSeen   time.Time `json:"last_seen"`
	SeqNum     int       `json:"seq_num"` // sequence number for beacon ordering
}

// BeaconMessage is the JSON payload sent over UDP multicast (239.77.0.1:9977).
// Keep payload <= 512 bytes to fit in standard UDP datagrams.
type BeaconMessage struct {
	Version    int                      `json:"v"`
	Hostname   string                   `json:"hostname"`
	Address    string                   `json:"address"`
	Roles      []string                 `json:"roles"`
	Services   []inventory.ServiceBinding `json:"services"`
	Arch       string                   `json:"arch"`
	OS         string                   `json:"os"`
	VRAMGB     int                      `json:"vram_gb"`
	RamGB      int                      `json:"ram_gb"`
	SeqNum     int                      `json:"seq"` // beacon sequence for deduplication
}

// =========================================================================
// Pipeline Execution
// =========================================================================

// PipelineAck is returned by POST /api/v1/pipeline/submit; acknowledges stage acceptance.
type PipelineAck struct {
	JobID     string    `json:"job_id"`
	Stage     string    `json:"stage"`
	Timestamp time.Time `json:"timestamp"`
}

// PipelineResult is returned by GET /api/v1/pipeline/result/{id}.
type PipelineResult struct {
	JobID      string                 `json:"job_id"`
	Stage      string                 `json:"stage"`
	Status     string                 `json:"status"` // pending, running, completed, failed
	Output     any                    `json:"output,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Progress   float64                `json:"progress"` // 0-1
}
