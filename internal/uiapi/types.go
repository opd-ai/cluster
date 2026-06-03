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
	MsgClusterState    MessageType = "cluster_state"
	MsgNodeMetrics     MessageType = "node_metrics"
	MsgLogLine         MessageType = "log_line"
	MsgJobProgress     MessageType = "job_progress"
	MsgImagePreview    MessageType = "image_preview"
	MsgTrainingMetrics MessageType = "training_metrics"
	MsgError           MessageType = "error"
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
	Name      string            `json:"name"`
	Role      string            `json:"role"`
	Labels    map[string]string `json:"labels"`
	Healthy   bool              `json:"healthy"`
	Models    []string          `json:"models"`
	GPUName   string            `json:"gpu_name,omitempty"`
	VRAMTotal int64             `json:"vram_total_mb,omitempty"`
	VRAMUsed  int64             `json:"vram_used_mb,omitempty"`
	CPUPct    float64           `json:"cpu_pct,omitempty"`
	MemPct    float64           `json:"mem_pct,omitempty"`
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
	JobKindInference JobKind = "inference"
	JobKindTraining  JobKind = "training"
	JobKindImageGen  JobKind = "image_gen"
	JobKindVideoGen  JobKind = "video_gen"
	JobKindRAGIngest JobKind = "rag_ingest"
	JobKindEval      JobKind = "eval"
)

// JobStatus is the lifecycle state of a job.
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
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
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleUser     Role = "user"
)
