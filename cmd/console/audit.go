package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/opd-ai/cluster/internal/uiapi"
)

// auditEntry is one immutable record in the append-only audit log.
type auditEntry struct {
	Time      time.Time  `json:"time"`
	Actor     string     `json:"actor"`
	Role      uiapi.Role `json:"role"`
	Action    string     `json:"action"`
	Resource  string     `json:"resource,omitempty"`
	RequestID string     `json:"request_id,omitempty"`
	OK        bool       `json:"ok"`
	Detail    string     `json:"detail,omitempty"`
}

// auditLog is an in-memory append-only audit log.
// Production deployments should persist to an append-only file or Loki.
type auditLog struct {
	mu      sync.Mutex
	entries []auditEntry
	maxSize int
}

// newAuditLog creates an audit log with a rolling window of maxSize entries.
func newAuditLog(maxSize int) *auditLog {
	if maxSize <= 0 {
		maxSize = 10_000
	}
	return &auditLog{maxSize: maxSize}
}

// Record appends a new audit entry.
func (a *auditLog) Record(e auditEntry) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, e)
	if len(a.entries) > a.maxSize {
		a.entries = a.entries[len(a.entries)-a.maxSize:]
	}
}

// Recent returns the last n entries (newest last).
func (a *auditLog) Recent(n int) []auditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n <= 0 || n > len(a.entries) {
		n = len(a.entries)
	}
	out := make([]auditEntry, n)
	copy(out, a.entries[len(a.entries)-n:])
	return out
}

// handleAuditLog serves GET /api/audit.
func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	entries := s.audit.Recent(200)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}
