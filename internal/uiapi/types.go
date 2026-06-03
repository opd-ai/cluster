// Package uiapi defines typed Go structs shared between cmd/console (server)
// and cmd/console-wasm (client).  All types are marshaled with encoding/json.
// The same types are used for both REST responses and WebSocket push messages.
package uiapi

import "time"

// -------------------------------------------------------------------------
// WebSocket message envelope
// -------------------------------------------------------------------------

// MessageType identifies the kind of a WebSocket push message.
type MessageType string

const (
	// MsgClusterState carries a full cluster snapshot (ClusterState).
	MsgClusterState MessageType = "cluster_state"
	// MsgNodeMetrics carries a periodic per-node metrics update (NodeMetrics).
	MsgNodeMetrics MessageType = "node_metrics"
	// MsgLogLine carries a single streamed log line.
	MsgLogLine MessageType = "log_line"
	// MsgJobProgress carries a job progress update.
	MsgJobProgress MessageType = "job_progress"
	// MsgImagePreview carries a generated image preview.
	MsgImagePreview MessageType = "image_preview"
	// MsgTrainingMetrics carries a training metrics update.
	MsgTrainingMetrics MessageType = "training_metrics"
	// MsgAggregateMetrics carries aggregated federated training metrics.
	MsgAggregateMetrics MessageType = "aggregate_metrics"
	// MsgGenerationEvent carries an image or video generation event.
	MsgGenerationEvent MessageType = "generation_event"
	// MsgPipelineState carries a pipeline state update.
	MsgPipelineState MessageType = "pipeline_state"
	// MsgError carries an error notification.
	MsgError MessageType = "error"
)

// Message is the WebSocket envelope.
type Message struct {
	Type    MessageType `json:"type"`
	Payload any         `json:"payload"`
}

// -------------------------------------------------------------------------
// Cluster state
// -------------------------------------------------------------------------

// ClusterState is the full cluster snapshot pushed to connected clients.
type ClusterState struct {
	Nodes     []NodeState `json:"nodes"`
	Jobs      []JobState  `json:"jobs"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// NodeState represents a single cluster node.
type NodeState struct {
	Name       string            `json:"name"`
	Role       string            `json:"role"`                  // deprecated: use Roles instead
	Roles      []string          `json:"roles,omitempty"`       // new: multiple roles
	Services   []NodeService     `json:"services,omitempty"`    // service bindings
	VRAMBudget map[string]int    `json:"vram_budget,omitempty"` // VRAM allocation per role
	Labels     map[string]string `json:"labels"`
	Healthy    bool              `json:"healthy"`
	Models     []string          `json:"models"`
	GPUName    string            `json:"gpu_name,omitempty"`
	VRAMTotal  int64             `json:"vram_total_mb,omitempty"`
	VRAMUsed   int64             `json:"vram_used_mb,omitempty"`
	CPUPct     float64           `json:"cpu_pct,omitempty"`
	MemPct     float64           `json:"mem_pct,omitempty"`
}

// NodeService represents a service binding on a node.
type NodeService struct {
	Role string `json:"role"`
	Port string `json:"port"`
}

// NodeMetrics is a periodic metrics update for a single node.
type NodeMetrics struct {
	NodeName  string    `json:"node_name"`
	VRAMUsed  int64     `json:"vram_used_mb"`
	CPUPct    float64   `json:"cpu_pct"`
	MemPct    float64   `json:"mem_pct"`
	Timestamp time.Time `json:"timestamp"`
}

// -------------------------------------------------------------------------
// Jobs
// -------------------------------------------------------------------------

// JobKind classifies a job type.
type JobKind string

const (
	// JobKindInference identifies an inference job.
	JobKindInference JobKind = "inference"
	// JobKindTraining identifies a model training job.
	JobKindTraining JobKind = "training"
	// JobKindImageGen identifies an image generation job.
	JobKindImageGen JobKind = "image_gen"
	// JobKindVideoGen identifies a video generation job.
	JobKindVideoGen JobKind = "video_gen"
	// JobKindRAGIngest identifies a RAG ingestion job.
	JobKindRAGIngest JobKind = "rag_ingest"
	// JobKindEval identifies an evaluation job.
	JobKindEval JobKind = "eval"
)

// JobStatus is the lifecycle state of a job.
type JobStatus string

const (
	// JobPending indicates a job that has been queued but not yet started.
	JobPending JobStatus = "pending"
	// JobRunning indicates a job that is currently executing.
	JobRunning JobStatus = "running"
	// JobCompleted indicates a job that finished successfully.
	JobCompleted JobStatus = "completed"
	// JobFailed indicates a job that terminated with an error.
	JobFailed JobStatus = "failed"
)

// JobState describes a running or recently completed job.
type JobState struct {
	ID          string    `json:"id"`
	Kind        JobKind   `json:"kind"`
	Status      JobStatus `json:"status"`
	Description string    `json:"description,omitempty"`
	Progress    float64   `json:"progress,omitempty"` // 0–1
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Error       string    `json:"error,omitempty"`
}

// JobProgress is a partial update pushed during a running job.
type JobProgress struct {
	JobID    string    `json:"job_id"`
	Progress float64   `json:"progress"`
	Message  string    `json:"message,omitempty"`
	Time     time.Time `json:"time"`
}

// -------------------------------------------------------------------------
// Logs
// -------------------------------------------------------------------------

// LogLine is a single log entry pushed to the client.
type LogLine struct {
	Source    string    `json:"source"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// -------------------------------------------------------------------------
// Training metrics
// -------------------------------------------------------------------------

// TrainingMetrics carries step-level metrics from a training run.
type TrainingMetrics struct {
	JobID     string    `json:"job_id"`
	Step      int       `json:"step"`
	Loss      float64   `json:"loss"`
	LR        float64   `json:"lr"`
	Timestamp time.Time `json:"timestamp"`
}

// -------------------------------------------------------------------------
// Image/video preview
// -------------------------------------------------------------------------

// ImagePreview carries a generated image URL or base64 data.
type ImagePreview struct {
	JobID  string `json:"job_id"`
	URL    string `json:"url,omitempty"`
	B64    string `json:"b64_json,omitempty"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// -------------------------------------------------------------------------
// Auth
// -------------------------------------------------------------------------

// LoginRequest is the body for POST /api/login.
type LoginRequest struct {
	APIKey string `json:"api_key"`
}

// LoginResponse is the response for POST /api/login.
type LoginResponse struct {
	Token string `json:"token"`
	Role  string `json:"role"`
}

// Role is a permission level.
type Role string

const (
	// RoleAdmin grants full administrative access.
	RoleAdmin Role = "admin"
	// RoleOperator grants permission to operate the cluster without admin rights.
	RoleOperator Role = "operator"
	// RoleUser grants standard read and submit access.
	RoleUser Role = "user"
)

// -------------------------------------------------------------------------
// Aggregate Metrics & Cross-Node Observability
// -------------------------------------------------------------------------

// AggregateMetrics provides cluster-wide rollup metrics from all nodes.
type AggregateMetrics struct {
	Timestamp            time.Time                 `json:"timestamp"`
	TotalCPUPct          float64                   `json:"total_cpu_pct"`
	TotalMemPct          float64                   `json:"total_mem_pct"`
	TotalVRAMUsedMB      int64                     `json:"total_vram_used_mb"`
	TotalVRAMAvailableMB int64                     `json:"total_vram_available_mb"`
	PerRoleMetrics       map[string]AggRoleMetrics `json:"per_role_metrics"`
}

// AggRoleMetrics aggregates metrics for a single role across all nodes.
type AggRoleMetrics struct {
	Role              string  `json:"role"`
	NodesActive       int     `json:"nodes_active"`
	TotalQueueDepth   int     `json:"total_queue_depth"`
	AvgLatencyEMAms   float64 `json:"avg_latency_ema_ms"`
	TotalVRAMUsedMB   int64   `json:"total_vram_used_mb"`
	TotalVRAMBudgetMB int64   `json:"total_vram_budget_mb"`
}

// GenerationEvent carries real-time events from image or video generation pipelines.
type GenerationEvent struct {
	JobID       string    `json:"job_id"`
	NodeAddress string    `json:"node_address"`
	Role        string    `json:"role"` // image-generation, video-generation, etc.
	Progress    float64   `json:"progress"`
	Status      string    `json:"status"` // pending, generating, completed, failed
	OutputURL   string    `json:"output_url,omitempty"`
	Error       string    `json:"error,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// PipelineState tracks multi-stage pipeline execution across nodes.
type PipelineState struct {
	PipelineID  string               `json:"pipeline_id"`
	Status      string               `json:"status"` // pending, running, completed, failed
	Stages      []PipelineStageState `json:"stages"`
	StartedAt   time.Time            `json:"started_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
	CompletedAt time.Time            `json:"completed_at,omitempty"`
}

// PipelineStageState tracks a single stage in a pipeline.
type PipelineStageState struct {
	StageID     string    `json:"stage_id"`
	Index       int       `json:"index"`
	Status      string    `json:"status"` // pending, running, completed, failed
	NodeAddress string    `json:"node_address"`
	Role        string    `json:"role"`
	Progress    float64   `json:"progress"`
	Input       any       `json:"input,omitempty"`
	Output      any       `json:"output,omitempty"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}
