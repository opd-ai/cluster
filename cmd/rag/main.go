// cmd/rag is the retrieval-augmented generation service.
//
// Endpoints:
//
//	POST /rag/query   — top-k hybrid retrieval (dense + BM25, reranked)
//	POST /rag/answer  — retrieve + chat completion + citations
//
// Per-collection access is controlled by API keys.  A key may access any
// collection unless restricted by the COLLECTION_ACL environment variable
// (comma-separated "key:collection" pairs).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// -------------------------------------------------------------------------
// Config
// -------------------------------------------------------------------------

type config struct {
	addr           string
	qdrantAddr     string
	gatewayURL     string
	embeddingModel string
	chatModel      string
	topK           int
	bm25Weight     float64
	apiKeys        map[string]struct{}
	collectionACL  map[string]string // key → collection (empty = all allowed)
}

// -------------------------------------------------------------------------
// BM25 (in-process, simplified)
// -------------------------------------------------------------------------

// bm25Score computes a BM25-like relevance score for text vs query tokens.
// This is a simplified in-process implementation; production deployments can
// use bleve or a dedicated search service.
func bm25Score(text, query string) float64 {
	const k1, b, avgdl = 1.5, 0.75, 500.0
	tokens := strings.Fields(strings.ToLower(query))
	words := strings.Fields(strings.ToLower(text))
	dl := float64(len(words))

	// Term frequency map
	tf := make(map[string]float64)
	for _, w := range words {
		tf[w]++
	}

	score := 0.0
	for _, t := range tokens {
		f := tf[t]
		score += f * (k1 + 1) / (f + k1*(1-b+b*dl/avgdl))
	}
	return score
}

// -------------------------------------------------------------------------
// Retrieval types
// -------------------------------------------------------------------------

// result is a single retrieval hit.
type result struct {
	Text       string  `json:"text"`
	File       string  `json:"file"`
	Chunk      int64   `json:"chunk"`
	Score      float64 `json:"score"`
	Collection string  `json:"collection"`
}

// -------------------------------------------------------------------------
// Server
// -------------------------------------------------------------------------

// server holds the RAG service state.
type server struct {
	cfg     config
	conn    *grpc.ClientConn
	qPoints qdrant.PointsClient
}

func newServer(cfg config, conn *grpc.ClientConn) *server {
	return &server{
		cfg:     cfg,
		conn:    conn,
		qPoints: qdrant.NewPointsClient(conn),
	}
}

// -------------------------------------------------------------------------
// /rag/query
// -------------------------------------------------------------------------

type queryRequest struct {
	Query      string `json:"query"`
	Collection string `json:"collection"`
	TopK       int    `json:"top_k"`
}

type queryResponse struct {
	Results []result `json:"results"`
}

func (s *server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}

	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if req.TopK <= 0 {
		req.TopK = s.cfg.topK
	}
	if req.Collection == "" {
		http.Error(w, `{"error":"collection required"}`, http.StatusBadRequest)
		return
	}
	if !s.collectionAllowed(r, req.Collection) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}

	results, err := s.retrieve(r.Context(), req.Query, req.Collection, req.TopK)
	if err != nil {
		log.Printf("retrieve: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	writeJSON(w, queryResponse{Results: results})
}

// -------------------------------------------------------------------------
// /rag/answer
// -------------------------------------------------------------------------

type answerRequest struct {
	Query      string `json:"query"`
	Collection string `json:"collection"`
	TopK       int    `json:"top_k"`
	Model      string `json:"model"`
}

type answerResponse struct {
	Answer    string   `json:"answer"`
	Citations []result `json:"citations"`
}

func (s *server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}

	var req answerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if req.TopK <= 0 {
		req.TopK = s.cfg.topK
	}
	if req.Collection == "" {
		http.Error(w, `{"error":"collection required"}`, http.StatusBadRequest)
		return
	}
	if !s.collectionAllowed(r, req.Collection) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}

	model := req.Model
	if model == "" {
		model = s.cfg.chatModel
	}

	hits, err := s.retrieve(r.Context(), req.Query, req.Collection, req.TopK)
	if err != nil {
		log.Printf("retrieve for answer: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	answer, err := s.chatWithContext(r.Context(), req.Query, hits, model, r.Header.Get("Authorization"))
	if err != nil {
		log.Printf("chat: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	writeJSON(w, answerResponse{Answer: answer, Citations: hits})
}

// -------------------------------------------------------------------------
// Core retrieval
// -------------------------------------------------------------------------

func (s *server) retrieve(ctx context.Context, query, collection string, topK int) ([]result, error) {
	// 1. Dense retrieval via Qdrant
	vec, err := s.embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	searchResp, err := s.qPoints.Search(ctx, &qdrant.SearchPoints{
		CollectionName: collection,
		Vector:         vec,
		Limit:          uint64(topK * 3), // over-fetch for reranking
		WithPayload:    &qdrant.WithPayloadSelector{SelectorOptions: &qdrant.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant search: %w", err)
	}

	// 2. Build initial results
	hits := make([]result, 0, len(searchResp.GetResult()))
	for _, pt := range searchResp.GetResult() {
		p := pt.GetPayload()
		text := stringPayload(p, "text")
		file := stringPayload(p, "file")
		chunk := int64Payload(p, "chunk")

		// 3. Hybrid score: dense + BM25
		bm25 := bm25Score(text, query)
		hybrid := float64(pt.GetScore())*(1-s.cfg.bm25Weight) + normalise(bm25)*s.cfg.bm25Weight

		hits = append(hits, result{
			Text:       text,
			File:       file,
			Chunk:      chunk,
			Score:      hybrid,
			Collection: collection,
		})
	}

	// 4. Sort by hybrid score, return top-k
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

// -------------------------------------------------------------------------
// Chat with context
// -------------------------------------------------------------------------

func (s *server) chatWithContext(ctx context.Context, query string, hits []result, model, authHeader string) (string, error) {
	var sb strings.Builder
	sb.WriteString("You are a helpful assistant. Answer the user's question using only the provided context. ")
	sb.WriteString("Cite sources using [n] notation where n is the citation index.\n\n")
	sb.WriteString("Context:\n")
	for i, h := range hits {
		fmt.Fprintf(&sb, "[%d] (file: %s)\n%s\n\n", i+1, h.File, h.Text)
	}

	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": sb.String()},
			{"role": "user", "content": query},
		},
		"stream": false,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.gatewayURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode chat response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return result.Choices[0].Message.Content, nil
}

// -------------------------------------------------------------------------
// Embedding
// -------------------------------------------------------------------------

func (s *server) embed(ctx context.Context, text string) ([]float32, error) {
	body := map[string]any{
		"input": []string{text},
		"model": s.cfg.embeddingModel,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.gatewayURL+"/v1/embeddings", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed decode: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("embed: empty response")
	}
	return result.Data[0].Embedding, nil
}

// -------------------------------------------------------------------------
// Auth helpers
// -------------------------------------------------------------------------

func (s *server) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if len(s.cfg.apiKeys) == 0 {
		return true
	}
	key := extractBearer(r)
	if _, ok := s.cfg.apiKeys[key]; !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *server) collectionAllowed(r *http.Request, collection string) bool {
	if len(s.cfg.collectionACL) == 0 {
		return true
	}
	key := extractBearer(r)
	allowed, ok := s.cfg.collectionACL[key]
	return !ok || allowed == "" || allowed == collection
}

func extractBearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// -------------------------------------------------------------------------
// Payload helpers
// -------------------------------------------------------------------------

func stringPayload(p map[string]*qdrant.Value, key string) string {
	v, ok := p[key]
	if !ok {
		return ""
	}
	sv, ok := v.Kind.(*qdrant.Value_StringValue)
	if !ok {
		return ""
	}
	return sv.StringValue
}

func int64Payload(p map[string]*qdrant.Value, key string) int64 {
	v, ok := p[key]
	if !ok {
		return 0
	}
	iv, ok := v.Kind.(*qdrant.Value_IntegerValue)
	if !ok {
		return 0
	}
	return iv.IntegerValue
}

// normalise maps BM25 scores to [0,1] via sigmoid.
func normalise(score float64) float64 {
	return 1.0 / (1.0 + math.Exp(-score/5.0))
}

// -------------------------------------------------------------------------
// HTTP helpers
// -------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

// -------------------------------------------------------------------------
// Main
// -------------------------------------------------------------------------

func main() {
	addr := flag.String("addr", ":8081", "Listen address")
	qdrantAddr := flag.String("qdrant-addr", "localhost:6334", "Qdrant gRPC address")
	gatewayURL := flag.String("gateway-url", "http://localhost:8080", "Gateway base URL")
	embeddingModel := flag.String("embedding-model", "nomic-embed-text", "Embedding model name")
	chatModel := flag.String("chat-model", "llama3", "Chat model for /rag/answer")
	topK := flag.Int("top-k", 5, "Default number of results to return")
	bm25Weight := flag.Float64("bm25-weight", 0.3, "BM25 weight in hybrid score (0=dense only, 1=BM25 only)")
	apiKeyEnv := flag.String("api-key-env", "GATEWAY_API_KEYS", "Env var containing comma-separated API keys")
	flag.Parse()

	cfg := config{
		addr:           *addr,
		qdrantAddr:     *qdrantAddr,
		gatewayURL:     *gatewayURL,
		embeddingModel: *embeddingModel,
		chatModel:      *chatModel,
		topK:           *topK,
		bm25Weight:     *bm25Weight,
		apiKeys:        make(map[string]struct{}),
		collectionACL:  make(map[string]string),
	}

	// Load API keys
	if raw := os.Getenv(*apiKeyEnv); raw != "" {
		for _, k := range strings.Split(raw, ",") {
			if k = strings.TrimSpace(k); k != "" {
				cfg.apiKeys[k] = struct{}{}
			}
		}
	}

	// Load collection ACL: "key:collection,..."
	if raw := os.Getenv("COLLECTION_ACL"); raw != "" {
		for _, pair := range strings.Split(raw, ",") {
			parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
			if len(parts) == 2 {
				cfg.collectionACL[parts[0]] = parts[1]
			}
		}
	}

	conn, err := grpc.NewClient(cfg.qdrantAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("connect qdrant: %v", err)
	}
	defer conn.Close()

	srv := newServer(cfg, conn)

	mux := http.NewServeMux()
	mux.HandleFunc("/rag/query", srv.handleQuery)
	mux.HandleFunc("/rag/answer", srv.handleAnswer)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("rag service listening on %s", cfg.addr)
	if err := http.ListenAndServe(cfg.addr, mux); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
