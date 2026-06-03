// cmd/gateway implements an OpenAI-compatible HTTP API gateway that routes
// inference requests to backend Ollama daemons.
//
// Supported endpoints:
//
//	GET  /v1/models                — list available models
//	POST /v1/chat/completions      — chat (streaming + non-streaming)
//	POST /v1/completions           — text completion
//	POST /v1/embeddings            — text embeddings
//	GET  /healthz                  — liveness probe
//	GET  /status                   — cluster status (JSON)
//	GET  /metrics                  — Prometheus metrics
//
// Routing:
//   - Round-robin across backend nodes that report a model in /api/tags.
//   - Sticky sessions per (api_key, model) to preserve KV-cache locality.
//   - Falls back to a model pull on cache miss.
//   - Auth: ****** keys; keys are loaded from GATEWAY_API_KEYS (colon-
//     separated) or an optional key file (--keys-file).
//
// Usage:
//
//	gateway [flags]
//
// Flags:
//
//	-addr         listen address (default: :8080)
//	-backends     comma-separated list of Ollama backend URLs
//	-inventory    path to inventory YAML for dynamic backend discovery
//	-keys-file    path to newline-delimited API keys file
//	-probe-interval  backend health probe interval in seconds (default: 15)
//	-speculative  enable speculative decoding feature flag
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
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
	"github.com/go-chi/chi/v5/middleware"
	"github.com/opd-ai/cluster/internal/tracing"
)

// Backend represents a single Ollama node.
type Backend struct {
	URL     string
	Models  []string
	Healthy bool
	mu      sync.RWMutex
}

// Gateway is the main gateway state.
type Gateway struct {
	backends     []*Backend
	apiKeys      map[string]struct{}
	sticky       map[string]int // (key+model) → backend index
	loraAdapters map[string]LoRAAdapter
	swarmURL     string
	swarmHealthy bool
	ragURL       string
	quotaCfg     *quotaConfig
	mu           sync.RWMutex
	httpClient   *http.Client  // shared client for connection pooling
	rrIdx        atomic.Uint64 // round-robin counter for pickBackend
	speculative  bool
	reqTotal     atomic.Int64 // total requests handled
	reqErrors    atomic.Int64 // total gateway request failures
}

// maxStickyEntries limits the sticky session map size to prevent unbounded growth.
const maxStickyEntries = 10_000

func main() {
	addr := flag.String("addr", ":8080", "Listen address")
	backendsStr := flag.String("backends", "", "Comma-separated backend Ollama URLs")
	inventoryPath := flag.String("inventory", "cluster/inventory.yaml", "Inventory YAML for backend discovery")
	keysFile := flag.String("keys-file", "", "Newline-delimited API keys file")
	probeInterval := flag.Int("probe-interval", 15, "Backend health probe interval (seconds)")
	speculative := flag.Bool("speculative", false, "Enable speculative decoding feature flag")
	loraManifest := flag.String("lora-manifest", "", "Path to LoRA adapter manifest JSON (enables hot-swap)")
	swarmURL := flag.String("swarmui-url", "", "SwarmUI backend URL for /v1/images/* endpoints")
	maxImages := flag.Int("max-images-per-key-per-day", 0, "Daily image quota per API key (0=unlimited)")
	maxVideos := flag.Int("max-videos-per-key-per-day", 0, "Daily video quota per API key (0=unlimited)")
	nsfwFilter := flag.Bool("nsfw-filter", false, "Enable NSFW prompt filter (off by default in self-hosted)")
	ragURL := flag.String("rag-url", "", "RAG service base URL (e.g. http://rag:8081)")
	otelEndpoint := flag.String("otel-endpoint", "", "OTLP/HTTP collector endpoint (e.g. http://otel-collector:4318); empty disables tracing")
	telemetry := flag.Bool("telemetry", false, "Send anonymous usage ping (opt-in; disabled by default)")
	flag.Parse()

	ctx := context.Background()
	if *otelEndpoint != "" {
		shutdown, err := tracing.Init(ctx, "gateway", *otelEndpoint)
		if err != nil {
			log.Printf("tracing init: %v (continuing without traces)", err)
		} else {
			defer shutdown(ctx) //nolint:errcheck
		}
	}

	gw := &Gateway{
		apiKeys:      make(map[string]struct{}),
		sticky:       make(map[string]int),
		loraAdapters: make(map[string]LoRAAdapter),
		speculative:  *speculative,
		swarmURL:     *swarmURL,
		ragURL:       *ragURL,
		quotaCfg: &quotaConfig{
			MaxImagesPerKeyPerDay: *maxImages,
			MaxVideosPerKeyPerDay: *maxVideos,
			NSFWFilter:            *nsfwFilter,
			NSFWBlocklist:         defaultNSFWBlocklist,
		},
	}

	// Shared HTTP client with connection pooling (H8).
	gw.httpClient = &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Load backends
	if *backendsStr != "" {
		for _, u := range strings.Split(*backendsStr, ",") {
			u = strings.TrimSpace(u)
			if u != "" {
				gw.backends = append(gw.backends, &Backend{URL: u, Healthy: true})
			}
		}
	} else {
		discovered := discoverBackends(*inventoryPath)
		gw.backends = append(gw.backends, discovered...)
	}

	if len(gw.backends) == 0 {
		log.Println("Warning: no backends configured; gateway will start but cannot serve requests")
	}

	// Load API keys
	loadAPIKeys(gw, *keysFile)

	// Start health probing
	stopProbe := make(chan struct{})
	go gw.probeLoop(time.Duration(*probeInterval)*time.Second, stopProbe)
	defer close(stopProbe)

	// Start LoRA adapter watcher if manifest path is set.
	if *loraManifest != "" {
		stopLora := make(chan struct{})
		go gw.startLoRAWatcher(*loraManifest, 10*time.Second, stopLora)
		defer close(stopLora)
	}

	// Start video job pruning (remove completed/failed jobs older than 24h).
	stopVideoPrune := make(chan struct{})
	go pruneVideoJobsLoop(stopVideoPrune)
	defer close(stopVideoPrune)

	// Start anonymous usage ping (opt-in, off by default).
	if *telemetry {
		go telemetryPing(ctx, len(gw.backends))
	}

	// Build router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(tracing.Middleware("gateway"))
	r.Use(gw.authMiddleware)

	r.Get("/healthz", handleHealthz)
	r.Get("/status", gw.handleStatus)
	r.Get("/metrics", gw.handleMetrics)

	// OpenAI-compatible endpoints
	r.Get("/v1/models", gw.handleListModels)
	r.Post("/v1/chat/completions", gw.handleChatCompletions)
	r.Post("/v1/completions", gw.handleCompletions)
	r.Post("/v1/embeddings", gw.handleEmbeddings)

	// OpenAI Images API (proxied to SwarmUI)
	r.Post("/v1/images/generations", gw.handleImageGenerations)
	r.Post("/v1/images/edits", gw.handleImageEdits)

	// Video generation API (long-running, job-based)
	r.Post("/v1/videos/generations", gw.handleVideoGenerations)
	r.Post("/v1/videos/edits", gw.handleVideoEdits)
	r.Get("/v1/videos/jobs/{id}", gw.handleVideoJobStatus)

	log.Printf("gateway listening on %s (backends: %d, speculative: %v)",
		*addr, len(gw.backends), *speculative)
	httpSrv := &http.Server{Addr: *addr, Handler: r}

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Println("gateway shutting down...")
	shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
}

// -------------------------------------------------------------------------
// Telemetry (opt-in; disabled by default via --telemetry=false)
// -------------------------------------------------------------------------

// telemetryPing sends an anonymous usage ping once on startup and then every
// 24 hours. The payload contains only aggregate, non-identifying information:
// number of backends and the Go runtime GOOS/GOARCH. No user data, prompts,
// API keys, or model outputs are ever included.
//
// Enabled only when the --telemetry flag is explicitly set to true.
func telemetryPing(ctx context.Context, backendCount int) {
	const endpoint = "https://telemetry.opd-ai.com/v1/ping"
	const interval = 24 * time.Hour

	payload := fmt.Sprintf(`{"product":"cluster-gateway","backends":%d}`, backendCount)

	send := func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
			strings.NewReader(payload))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		resp.Body.Close()
	}

	send()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			send()
		}
	}
}

// -------------------------------------------------------------------------
// Auth middleware
// -------------------------------------------------------------------------

func (gw *Gateway) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for public endpoints
		if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		if len(gw.apiKeys) == 0 {
			// No keys configured — open mode
			gw.reqTotal.Add(1)
			next.ServeHTTP(w, r)
			return
		}

		key := extractBearerToken(r)
		gw.mu.RLock()
		_, ok := gw.apiKeys[key]
		gw.mu.RUnlock()

		if !ok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		gw.reqTotal.Add(1)
		next.ServeHTTP(w, r)
	})
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// -------------------------------------------------------------------------
// Handler: /healthz
// -------------------------------------------------------------------------

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

// -------------------------------------------------------------------------
// Handler: /status
// -------------------------------------------------------------------------

func (gw *Gateway) handleStatus(w http.ResponseWriter, _ *http.Request) {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	type backendStatus struct {
		URL     string   `json:"url"`
		Healthy bool     `json:"healthy"`
		Models  []string `json:"models"`
	}
	var statuses []backendStatus
	for _, b := range gw.backends {
		b.mu.RLock()
		statuses = append(statuses, backendStatus{
			URL:     b.URL,
			Healthy: b.Healthy,
			Models:  b.Models,
		})
		b.mu.RUnlock()
	}

	writeJSON(w, map[string]any{
		"backends":      statuses,
		"speculative":   gw.speculative,
		"swarm_healthy": gw.swarmHealthy,
	})
}

// -------------------------------------------------------------------------
// Handler: /metrics (minimal Prometheus text)
// -------------------------------------------------------------------------

func (gw *Gateway) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	gw.mu.RLock()
	healthy := 0
	total := len(gw.backends)
	for _, b := range gw.backends {
		b.mu.RLock()
		if b.Healthy {
			healthy++
		}
		b.mu.RUnlock()
	}
	gw.mu.RUnlock()

	reqTotal := gw.reqTotal.Load()
	reqErrors := gw.reqErrors.Load()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "# HELP gateway_backends_total Total configured backends\n")
	fmt.Fprintf(w, "# TYPE gateway_backends_total gauge\n")
	fmt.Fprintf(w, "gateway_backends_total %d\n", total)
	fmt.Fprintf(w, "# HELP gateway_backends_healthy Healthy backends\n")
	fmt.Fprintf(w, "# TYPE gateway_backends_healthy gauge\n")
	fmt.Fprintf(w, "gateway_backends_healthy %d\n", healthy)
	fmt.Fprintf(w, "# HELP gateway_requests_total Total authenticated API requests\n")
	fmt.Fprintf(w, "# TYPE gateway_requests_total counter\n")
	fmt.Fprintf(w, "gateway_requests_total %d\n", reqTotal)
	fmt.Fprintf(w, "# HELP gateway_request_errors_total Total gateway transport and internal request failures\n")
	fmt.Fprintf(w, "# TYPE gateway_request_errors_total counter\n")
	fmt.Fprintf(w, "gateway_request_errors_total %d\n", reqErrors)
}

// -------------------------------------------------------------------------
// Handler: GET /v1/models
// -------------------------------------------------------------------------

func (gw *Gateway) handleListModels(w http.ResponseWriter, _ *http.Request) {
	seen := make(map[string]struct{})
	var models []map[string]any

	for _, b := range gw.backends {
		b.mu.RLock()
		for _, m := range b.Models {
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				models = append(models, map[string]any{
					"id":       m,
					"object":   "model",
					"owned_by": "local",
				})
			}
		}
		b.mu.RUnlock()
	}

	writeJSON(w, map[string]any{"object": "list", "data": models})
}

// -------------------------------------------------------------------------
// Handler: POST /v1/chat/completions
// -------------------------------------------------------------------------

func (gw *Gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	modelRaw, ok := req["model"]
	if !ok {
		http.Error(w, `{"error":"model field required"}`, http.StatusBadRequest)
		return
	}
	var model string
	_ = json.Unmarshal(modelRaw, &model)

	// Server-side RAG tool injection.
	if gw.ragURL != "" {
		if err := gw.injectRAGContextRaw(r.Context(), req, r.Header.Get("Authorization")); err != nil {
			log.Printf("RAG inject: %v", err)
		}
	}

	// Check for LoRA adapter alias first.
	if loraURL := gw.resolveLoRAModel(model); loraURL != "" {
		gw.proxyTo(loraURL+"/api/chat", w, r, req)
		return
	}

	backend := gw.pickBackend(extractBearerToken(r), model)
	if backend == nil {
		http.Error(w, `{"error":"no healthy backend for model"}`, http.StatusServiceUnavailable)
		return
	}

	gw.proxyTo(backend.URL+"/api/chat", w, r, req)
}

// -------------------------------------------------------------------------
// Handler: POST /v1/completions
// -------------------------------------------------------------------------

func (gw *Gateway) handleCompletions(w http.ResponseWriter, r *http.Request) {
	var req map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	var model string
	if raw, ok := req["model"]; ok {
		_ = json.Unmarshal(raw, &model)
	}

	backend := gw.pickBackend(extractBearerToken(r), model)
	if backend == nil {
		http.Error(w, `{"error":"no healthy backend for model"}`, http.StatusServiceUnavailable)
		return
	}

	gw.proxyTo(backend.URL+"/api/generate", w, r, req)
}

// -------------------------------------------------------------------------
// Handler: POST /v1/embeddings
// -------------------------------------------------------------------------

func (gw *Gateway) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	var model string
	if raw, ok := req["model"]; ok {
		_ = json.Unmarshal(raw, &model)
	}

	backend := gw.pickBackend(extractBearerToken(r), model)
	if backend == nil {
		http.Error(w, `{"error":"no healthy backend for model"}`, http.StatusServiceUnavailable)
		return
	}

	gw.proxyTo(backend.URL+"/api/embeddings", w, r, req)
}

// -------------------------------------------------------------------------
// Routing helpers
// -------------------------------------------------------------------------

// pickBackend returns a healthy backend for the given model using sticky
// sessions per (key+model) with round-robin fallback.
func (gw *Gateway) pickBackend(key, model string) *Backend {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	// Collect healthy backends that have the model loaded.
	var candidates []*Backend
	var candidateIdx []int
	for i, b := range gw.backends {
		b.mu.RLock()
		healthy := b.Healthy
		hasModel := model == "" || containsModel(b.Models, model)
		b.mu.RUnlock()
		if healthy && hasModel {
			candidates = append(candidates, b)
			candidateIdx = append(candidateIdx, i)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Sticky session: reuse same backend if it's still healthy.
	stickyKey := key + ":" + model
	if idx, ok := gw.sticky[stickyKey]; ok {
		for i, ci := range candidateIdx {
			if ci == idx {
				return candidates[i]
			}
		}
	}

	// Round-robin across candidates using an atomic counter (M5).
	idx := int(gw.rrIdx.Add(1)-1) % len(candidates)
	chosen := candidateIdx[idx]

	// Evict an arbitrary entry if the sticky map exceeds the size cap (M4).
	if len(gw.sticky) >= maxStickyEntries {
		for k := range gw.sticky {
			delete(gw.sticky, k)
			break
		}
	}
	gw.sticky[stickyKey] = chosen
	return candidates[idx]
}

func containsModel(models []string, model string) bool {
	for _, m := range models {
		if m == model || strings.HasPrefix(m, model) {
			return true
		}
	}
	return false
}

// proxyTo forwards the decoded request body to the backend URL and streams
// the response back to the client.
func (gw *Gateway) proxyTo(url string, w http.ResponseWriter, r *http.Request, body map[string]json.RawMessage) {
	data, err := json.Marshal(body)
	if err != nil {
		gw.reqErrors.Add(1)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		gw.reqErrors.Add(1)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := gw.httpClient.Do(req)
	if err != nil {
		gw.reqErrors.Add(1)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Forward headers and stream body.
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if err != nil {
			break
		}
	}
}

// -------------------------------------------------------------------------
// Health probing
// -------------------------------------------------------------------------

func (gw *Gateway) probeLoop(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			gw.probeAll()
		case <-stop:
			return
		}
	}
}

func (gw *Gateway) probeAll() {
	gw.mu.RLock()
	backends := make([]*Backend, len(gw.backends))
	copy(backends, gw.backends)
	swarmURL := gw.swarmURL
	gw.mu.RUnlock()

	client := &http.Client{Timeout: 5 * time.Second}
	for _, b := range backends {
		models, healthy := probeBackend(client, b.URL)
		b.mu.Lock()
		b.Healthy = healthy
		b.Models = models
		b.mu.Unlock()
	}

	if swarmURL != "" {
		healthy, queueDepth := probeSwarm(client, swarmURL)
		gw.mu.Lock()
		gw.swarmHealthy = healthy
		gw.mu.Unlock()
		if !healthy {
			log.Printf("swarmui backend unhealthy")
		} else {
			log.Printf("swarmui backend healthy, queue_depth=%d", queueDepth)
		}
	}
}

func probeBackend(client *http.Client, url string) ([]string, bool) {
	resp, err := client.Get(url + "/api/tags")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, false
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, true // healthy but couldn't parse models
	}

	var names []string
	for _, m := range result.Models {
		names = append(names, m.Name)
	}
	return names, true
}

// probeSwarm calls SwarmUI /API/ListBackends to check health and queue depth.
func probeSwarm(client *http.Client, swarmURL string) (healthy bool, queueDepth int) {
	resp, err := client.Get(swarmURL + "/API/ListBackends")
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, 0
	}
	var result struct {
		Backends []struct {
			Status     string `json:"status"`
			QueueDepth int    `json:"queue_depth"`
		} `json:"backends"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return true, 0 // reachable but non-standard response
	}
	total := 0
	for _, b := range result.Backends {
		total += b.QueueDepth
	}
	return true, total
}

// -------------------------------------------------------------------------
// Backend discovery & API key loading
// -------------------------------------------------------------------------

func discoverBackends(inventoryPath string) []*Backend {
	data, err := os.ReadFile(inventoryPath)
	if err != nil {
		return nil
	}

	var backends []*Backend
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "address:") {
			addr := strings.TrimSpace(strings.TrimPrefix(trimmed, "address:"))
			if addr != "" {
				backends = append(backends, &Backend{
					URL:     "http://" + addr + ":11434",
					Healthy: true,
				})
			}
		}
	}
	return backends
}

func loadAPIKeys(gw *Gateway, keysFile string) {
	// Environment variable takes priority.
	if envKeys := os.Getenv("GATEWAY_API_KEYS"); envKeys != "" {
		for _, k := range strings.Split(envKeys, ":") {
			k = strings.TrimSpace(k)
			if k != "" {
				gw.apiKeys[k] = struct{}{}
			}
		}
		return
	}

	if keysFile == "" {
		return
	}

	data, err := os.ReadFile(keysFile)
	if err != nil {
		log.Printf("keys-file: %v", err)
		return
	}

	for _, line := range strings.Split(string(data), "\n") {
		k := strings.TrimSpace(line)
		if k != "" && !strings.HasPrefix(k, "#") {
			gw.apiKeys[k] = struct{}{}
		}
	}
}

// -------------------------------------------------------------------------
// JSON helper
// -------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
