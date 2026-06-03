// Command console is the server-side component of the cluster management
// console.  It:
//   - Serves the embedded WASM client (cmd/console-wasm compiled to main.wasm)
//     together with the necessary wasm_exec.js shim.
//   - Exposes a WebSocket endpoint at /api/ws that proxies cluster state from
//     the gateway and node agents, pushing uiapi.Message values to clients.
//   - Provides REST endpoints:
//     POST /api/login            — exchange an API key for a session token
//     GET  /api/cluster          — current ClusterState snapshot
//     GET  /api/jobs             — recent JobState list
//     GET  /api/logs?source=…    — last N log lines
//
// Usage:
//
//	console --addr :8080 --gateway http://localhost:8000 \
//	        --key-file /etc/cluster/keys.yaml \
//	        --wasm-dir /path/to/wasm
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/opd-ai/cluster/internal/nodeapi"
	"github.com/opd-ai/cluster/internal/uiapi"
)

// sessionTTL is how long a session token remains valid after issuance.
const sessionTTL = 24 * time.Hour

// -------------------------------------------------------------------------
// Server
// -------------------------------------------------------------------------

// sessionEntry stores a session token with its owner role and expiry.
type sessionEntry struct {
	role    uiapi.Role
	expires time.Time
}

// Server is the console HTTP server.
type Server struct {
	addr       string
	gatewayURL string
	wasmDir    string
	apiKeys    map[string]uiapi.Role

	mu             sync.RWMutex
	state          uiapi.ClusterState
	jobs           []uiapi.JobState
	logBuf         []uiapi.LogLine
	clients        map[chan uiapi.Message]struct{}
	audit          *auditLog
	sessions       map[string]sessionEntry
	nodeAgentURLs  []string // derived from gateway /status backends
}

func newServer(addr, gatewayURL, wasmDir, keyFile string) (*Server, error) {
	keys, err := loadKeyFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("load key file: %w", err)
	}
	return &Server{
		addr:       addr,
		gatewayURL: gatewayURL,
		wasmDir:    wasmDir,
		apiKeys:    keys,
		jobs:       make([]uiapi.JobState, 0),
		logBuf:     make([]uiapi.LogLine, 0),
		clients:    make(map[chan uiapi.Message]struct{}),
		sessions:   make(map[string]sessionEntry),
		audit:      newAuditLog(0),
	}, nil
}

// -------------------------------------------------------------------------
// Key file loader
// -------------------------------------------------------------------------

// loadKeyFile reads a newline-separated key:role file.
// Lines starting with '#' are ignored.
func loadKeyFile(path string) (map[string]uiapi.Role, error) {
	if path == "" {
		return make(map[string]uiapi.Role), nil
	}
	data, err := os.ReadFile(path) // #nosec G304 — operator-supplied path
	if err != nil {
		return nil, err
	}

	result := make(map[string]uiapi.Role)
	for _, line := range splitLines(string(data)) {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		key, role := parseKeyLine(line)
		if key != "" {
			result[key] = role
		}
	}
	return result, nil
}

// -------------------------------------------------------------------------
// HTTP handlers
// -------------------------------------------------------------------------

// routes registers all HTTP routes.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Static WASM client files.
	// The root index.html and WASM bootstrap assets are public (needed for the
	// login page to load); all other static assets require a valid session to
	// prevent directory traversal exposing operator-misconfigured files.
	fileServer := http.FileServer(http.Dir(s.wasmDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html", "/main.wasm", "/wasm_exec.js":
			fileServer.ServeHTTP(w, r)
			return
		}
		s.withAuth(fileServer.ServeHTTP)(w, r)
	})

	// REST API.
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/cluster", s.withAuth(s.handleCluster))
	mux.HandleFunc("/api/jobs", s.withAuth(s.handleJobs))
	mux.HandleFunc("/api/logs", s.withAuth(s.handleLogs))
	mux.HandleFunc("/api/audit", s.withAuth(s.handleAuditLog))

	// WebSocket.
	mux.HandleFunc("/api/ws", s.handleWS)

	// Plain-HTML accessibility fallback.
	s.registerHTMLRoutes(mux)

	return mux
}

// handleLogin exchanges an API key for a session token.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req uiapi.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	role, ok := s.apiKeys[req.APIKey]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Generate a random session token so the API key is not exposed to the
	// browser or network (M8).
	token, err := newSessionToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	now := time.Now()
	for existingToken, entry := range s.sessions {
		if now.After(entry.expires) {
			delete(s.sessions, existingToken)
		}
	}
	s.sessions[token] = sessionEntry{role: role, expires: time.Now().Add(sessionTTL)}
	s.mu.Unlock()

	resp := uiapi.LoginResponse{Token: token, Role: string(role)}
	s.audit.Record(auditEntry{
		Actor:  req.APIKey[:min(8, len(req.APIKey))] + "…",
		Role:   role,
		Action: "login",
		OK:     true,
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// newSessionToken generates a cryptographically random 32-byte hex token.
func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// lookupSession returns the role for a session token if it exists and has not
// expired. Expired sessions are pruned lazily on lookup.
func (s *Server) lookupSession(token string) (uiapi.Role, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.sessions[token]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expires) {
		delete(s.sessions, token)
		return "", false
	}
	return entry.role, true
}

// handleCluster returns the current ClusterState.
func (s *Server) handleCluster(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	state := s.state
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}

// handleJobs returns recent jobs.
func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	jobs := s.jobs
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobs)
}

// handleLogs returns buffered log lines.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	lines := s.logBuf
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(lines)
}

// handleWS upgrades to a WebSocket and pushes cluster updates.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	// Validate session token from query param.
	token := r.URL.Query().Get("token")
	if _, ok := s.lookupSession(token); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ch := make(chan uiapi.Message, 64)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	// Use nhooyr.io/websocket for WASM-compatible WS.
	upgradeAndServe(w, r, ch)
}

// withAuth wraps a handler with bearer-token auth.
func (s *Server) withAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if _, ok := s.lookupSession(token); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

// -------------------------------------------------------------------------
// Broadcast
// -------------------------------------------------------------------------

// broadcast sends a message to all connected WebSocket clients.
func (s *Server) broadcast(msg uiapi.Message) {
	s.mu.RLock()
	for ch := range s.clients {
		select {
		case ch <- msg:
		default:
		}
	}
	s.mu.RUnlock()
}

// -------------------------------------------------------------------------
// Polling loop — pull cluster state from gateway /status endpoint
// -------------------------------------------------------------------------

// pollNodeAgents periodically fetches metrics from all known node-agents
// and broadcasts an AggregateMetrics message to connected WebSocket clients.
func (s *Server) pollNodeAgents(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	client := &http.Client{Timeout: 5 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.fetchAndBroadcastAggMetrics(client)
		}
	}
}

func (s *Server) fetchAndBroadcastAggMetrics(client *http.Client) {
	s.mu.RLock()
	agentURLs := make([]string, len(s.nodeAgentURLs))
	copy(agentURLs, s.nodeAgentURLs)
	s.mu.RUnlock()

	if len(agentURLs) == 0 {
		return
	}

	agg := uiapi.AggregateMetrics{
		Timestamp:      time.Now(),
		PerRoleMetrics: make(map[string]uiapi.AggRoleMetrics),
	}

	for _, baseURL := range agentURLs {
		url := baseURL + "/api/v1/metrics"
		resp, err := client.Get(url) // #nosec G107 — operator-supplied URL
		if err != nil {
			continue
		}
		var m nodeapi.NodeMetricsExt
		decErr := json.NewDecoder(resp.Body).Decode(&m)
		resp.Body.Close()
		if decErr != nil {
			continue
		}
		agg.TotalCPUPct += m.CPUPct
		agg.TotalMemPct += m.MemPct
		for role, rm := range m.PerRole {
			cur := agg.PerRoleMetrics[role]
			cur.Role = role
			cur.NodesActive++
			cur.TotalQueueDepth += rm.QueueDepth
			cur.TotalVRAMUsedMB += rm.VRAMUsedMB
			cur.TotalVRAMBudgetMB += rm.VRAMTotalMB
			agg.TotalVRAMUsedMB += rm.VRAMUsedMB
			available := rm.VRAMTotalMB - rm.VRAMUsedMB
			if available < 0 {
				available = 0
			}
			agg.TotalVRAMAvailableMB += available
			agg.PerRoleMetrics[role] = cur
		}
	}

	s.broadcast(uiapi.Message{Type: uiapi.MsgAggregateMetrics, Payload: agg})
}

// pollGateway periodically fetches cluster state from the gateway.
func (s *Server) pollGateway(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	client := &http.Client{Timeout: 10 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.fetchGatewayState(client)
		}
	}
}

func (s *Server) fetchGatewayState(client *http.Client) {
	url := s.gatewayURL + "/status"
	resp, err := client.Get(url) // #nosec G107 — operator-supplied URL
	if err != nil {
		log.Printf("gateway poll error: %v", err)
		return
	}
	defer resp.Body.Close()

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		log.Printf("gateway state decode error: %v", err)
		return
	}

	// Build a minimal ClusterState from gateway /status.
	state := uiapi.ClusterState{UpdatedAt: time.Now()}
	var agentURLs []string
	if backendsRaw, ok := raw["backends"]; ok {
		var backends []struct {
			URL     string   `json:"url"`
			Healthy bool     `json:"healthy"`
			Models  []string `json:"models"`
		}
		if err := json.Unmarshal(backendsRaw, &backends); err == nil {
			for _, b := range backends {
				state.Nodes = append(state.Nodes, uiapi.NodeState{
					Name:    b.URL,
					Role:    "inference",
					Healthy: b.Healthy,
					Models:  b.Models,
				})
				// Derive node-agent URL from backend host (port 9977).
				host := backendHost(b.URL)
				if host != "" {
					agentURLs = append(agentURLs, "http://"+host+":9977")
				}
			}
		}
	}

	s.mu.Lock()
	s.state = state
	s.nodeAgentURLs = agentURLs
	s.mu.Unlock()

	s.broadcast(uiapi.Message{Type: uiapi.MsgClusterState, Payload: state})
}

// backendHost extracts just the hostname from a URL like "http://host:11434".
func backendHost(rawURL string) string {
	rawURL = strings.TrimPrefix(rawURL, "http://")
	rawURL = strings.TrimPrefix(rawURL, "https://")
	if idx := strings.Index(rawURL, "/"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	h, _, err := net.SplitHostPort(rawURL)
	if err != nil {
		return rawURL // no port present
	}
	return h
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
		return auth[len(prefix):]
	}
	return ""
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func parseKeyLine(line string) (string, uiapi.Role) {
	for i := 0; i < len(line); i++ {
		if line[i] == ':' {
			key := line[:i]
			role := uiapi.Role(line[i+1:])
			return key, role
		}
	}
	return line, uiapi.RoleUser
}

// -------------------------------------------------------------------------
// WebSocket upgrade helper (uses nhooyr.io/websocket)
// -------------------------------------------------------------------------

func upgradeAndServe(w http.ResponseWriter, r *http.Request, ch <-chan uiapi.Message) {
	// Import is in ws.go to keep this file import-clean.
	serveWebSocket(w, r, ch)
}

// -------------------------------------------------------------------------
// main
// -------------------------------------------------------------------------

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	gatewayURL := flag.String("gateway", "http://localhost:8000", "gateway base URL")
	keyFile := flag.String("key-file", "", "path to key:role file")
	wasmDir := flag.String("wasm-dir", "", "directory containing compiled WASM client (main.wasm, wasm_exec.js, index.html)")
	pollInterval := flag.Duration("poll-interval", 5*time.Second, "gateway poll interval")
	flag.Parse()

	if *wasmDir == "" {
		// Default to the wasm sub-directory next to the binary.
		exe, _ := os.Executable()
		*wasmDir = filepath.Join(filepath.Dir(exe), "wasm")
	}

	srv, err := newServer(*addr, *gatewayURL, *wasmDir, *keyFile)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.pollGateway(ctx, *pollInterval)
	go srv.pollNodeAgents(ctx, *pollInterval)

	httpSrv := &http.Server{
		Addr:         *addr,
		Handler:      srv.routes(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	log.Printf("console listening on %s", *addr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}
