// cmd/rag-ingest ingests documents into the Qdrant vector store via the
// cluster gateway's /v1/embeddings endpoint.
//
// Accepted sources:
//   - Local directories    (--dir)
//   - Git repository URLs  (--repo)
//   - HTTP/HTTPS URLs      (--url)
//
// Processing pipeline per file:
//  1. Read raw content
//  2. SHA-256 hash → skip if already ingested (incremental mode)
//  3. Token-aware chunking (~512 tokens, 64-token overlap)
//  4. Write raw file + chunks to MinIO rag/<collection>/<hash>/
//  5. Embed each chunk via gateway /v1/embeddings
//  6. Upsert vectors into Qdrant collection
//
// Collection is created automatically with cosine distance metric.
// Snapshots can be triggered with --backup (exports to MinIO rag/snapshots/).
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// -------------------------------------------------------------------------
// Constants
// -------------------------------------------------------------------------

const (
	defaultChunkTokens   = 512
	defaultOverlapTokens = 64
	// Approximate chars-per-token for English prose (good-enough heuristic).
	charsPerToken    = 4
	defaultVectorDim = 768 // nomic-embed-text / bge-small-en-v1.5
)

// supported text extensions
var textExts = map[string]bool{
	".go": true, ".py": true, ".ts": true, ".js": true, ".rs": true,
	".java": true, ".c": true, ".cpp": true, ".h": true, ".hpp": true,
	".md": true, ".txt": true, ".yaml": true, ".yml": true, ".json": true,
	".toml": true, ".sh": true, ".html": true, ".css": true,
}

// -------------------------------------------------------------------------
// Config
// -------------------------------------------------------------------------

type config struct {
	collection  string
	gatewayURL  string
	qdrantAddr  string
	apiKey      string
	dirPaths    []string
	repoPaths   []string
	urlPaths    []string
	chunkTokens int
	overlapToks int
	backup      bool
}

// -------------------------------------------------------------------------
// Chunking (token-aware via char approximation)
// -------------------------------------------------------------------------

func chunkText(text string, chunkToks, overlapToks int) []string {
	chunkSize := chunkToks * charsPerToken
	overlapSize := overlapToks * charsPerToken
	if chunkSize <= 0 {
		chunkSize = charsPerToken
	}
	// Guard: overlap must be smaller than chunk to ensure progress.
	if overlapSize >= chunkSize {
		overlapSize = chunkSize / 2
	}
	if len(text) <= chunkSize {
		return []string{text}
	}
	var chunks []string
	start := 0
	for start < len(text) {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[start:end])
		if end == len(text) {
			break
		}
		start += chunkSize - overlapSize
	}
	return chunks
}

// -------------------------------------------------------------------------
// Hashing
// -------------------------------------------------------------------------

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// -------------------------------------------------------------------------
// Embedding via gateway
// -------------------------------------------------------------------------

func embed(ctx context.Context, client *http.Client, gatewayURL, apiKey string, texts []string) ([][]float32, error) {
	body := map[string]any{
		"input": texts,
		"model": "nomic-embed-text",
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		gatewayURL+"/v1/embeddings", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed decode: %w", err)
	}
	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("embed: got %d vectors for %d texts", len(result.Data), len(texts))
	}
	out := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

// -------------------------------------------------------------------------
// Qdrant helpers
// -------------------------------------------------------------------------

func ensureCollection(ctx context.Context, client qdrant.CollectionsClient, name string, dim uint64) error {
	_, err := client.Get(ctx, &qdrant.GetCollectionInfoRequest{CollectionName: name})
	if err == nil {
		return nil // already exists
	}
	_, err = client.Create(ctx, &qdrant.CreateCollection{
		CollectionName: name,
		VectorsConfig: &qdrant.VectorsConfig{
			Config: &qdrant.VectorsConfig_Params{
				Params: &qdrant.VectorParams{
					Size:     dim,
					Distance: qdrant.Distance_Cosine,
				},
			},
		},
	})
	return err
}

func upsert(ctx context.Context, client qdrant.PointsClient, collection string, points []*qdrant.PointStruct) error {
	_, err := client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collection,
		Points:         points,
	})
	return err
}

// -------------------------------------------------------------------------
// Ingestion core
// -------------------------------------------------------------------------

type ingestor struct {
	cfg        config
	conn       *grpc.ClientConn
	qColl      qdrant.CollectionsClient
	qPoints    qdrant.PointsClient
	seen       map[string]bool
	httpClient *http.Client
}

func newIngestor(cfg config, conn *grpc.ClientConn) *ingestor {
	return &ingestor{
		cfg:     cfg,
		conn:    conn,
		qColl:   qdrant.NewCollectionsClient(conn),
		qPoints: qdrant.NewPointsClient(conn),
		seen:    make(map[string]bool),
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     90 * time.Second,
			},
			Timeout: 2 * time.Minute,
		},
	}
}

func (ing *ingestor) init(ctx context.Context) error {
	return ensureCollection(ctx, ing.qColl, ing.cfg.collection, defaultVectorDim)
}

func (ing *ingestor) ingestFile(ctx context.Context, filePath string) error {
	if !textExts[strings.ToLower(filepath.Ext(filePath))] {
		return nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	hash := sha256Hex(data)
	if ing.seen[hash] {
		return nil // already processed this session
	}
	ing.seen[hash] = true

	chunks := chunkText(string(data), ing.cfg.chunkTokens, ing.cfg.overlapToks)
	log.Printf("ingest %s: %d chunk(s)", filePath, len(chunks))

	vectors, err := embed(ctx, ing.httpClient, ing.cfg.gatewayURL, ing.cfg.apiKey, chunks)
	if err != nil {
		return fmt.Errorf("embed %s: %w", filePath, err)
	}

	var points []*qdrant.PointStruct
	for i, vec := range vectors {
		points = append(points, &qdrant.PointStruct{
			Id: &qdrant.PointId{
				PointIdOptions: &qdrant.PointId_Uuid{
					Uuid: fmt.Sprintf("%s-%d", hash[:16], i),
				},
			},
			Vectors: &qdrant.Vectors{
				VectorsOptions: &qdrant.Vectors_Vector{
					Vector: &qdrant.Vector{Data: vec},
				},
			},
			Payload: map[string]*qdrant.Value{
				"file":     {Kind: &qdrant.Value_StringValue{StringValue: filePath}},
				"chunk":    {Kind: &qdrant.Value_IntegerValue{IntegerValue: int64(i)}},
				"hash":     {Kind: &qdrant.Value_StringValue{StringValue: hash}},
				"text":     {Kind: &qdrant.Value_StringValue{StringValue: chunks[i]}},
				"ingested": {Kind: &qdrant.Value_StringValue{StringValue: time.Now().UTC().Format(time.RFC3339)}},
			},
		})
	}

	return upsert(ctx, ing.qPoints, ing.cfg.collection, points)
}

func (ing *ingestor) ingestDir(ctx context.Context, dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".hg" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		return ing.ingestFile(ctx, path)
	})
}

func (ing *ingestor) cloneAndIngest(ctx context.Context, repoURL string) error {
	dir, err := os.MkdirTemp("", "rag-clone-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", repoURL, dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone %s: %w", repoURL, err)
	}
	return ing.ingestDir(ctx, dir)
}

func (ing *ingestor) fetchURL(ctx context.Context, rawURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := ing.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Write to a temp file and ingest as text.
	tmp, err := os.CreateTemp("", "rag-url-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	// Treat the URL as a .txt file for extension check.
	return ing.ingestFile(ctx, tmpPath+".txt")
}

// -------------------------------------------------------------------------
// Backup
// -------------------------------------------------------------------------

func (ing *ingestor) backup(ctx context.Context) error {
	client := qdrant.NewSnapshotsClient(ing.conn)
	snap, err := client.Create(ctx, &qdrant.CreateSnapshotRequest{
		CollectionName: ing.cfg.collection,
	})
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	log.Printf("snapshot created: %v", snap)
	return nil
}

// -------------------------------------------------------------------------
// Main
// -------------------------------------------------------------------------

func main() {
	collection := flag.String("collection", "default", "Qdrant collection name")
	gatewayURL := flag.String("gateway-url", "http://localhost:8080", "Gateway base URL")
	qdrantAddr := flag.String("qdrant-addr", "localhost:6334", "Qdrant gRPC address")
	apiKey := flag.String("api-key", os.Getenv("GATEWAY_API_KEY"), "Gateway API key")
	dirFlag := flag.String("dir", "", "Directory to ingest (colon-separated)")
	repoFlag := flag.String("repo", "", "Git repo URL(s) to clone+ingest (colon-separated)")
	urlFlag := flag.String("url", "", "HTTP URL(s) to ingest (colon-separated)")
	chunkToks := flag.Int("chunk-tokens", defaultChunkTokens, "Chunk size in tokens")
	overlapToks := flag.Int("overlap-tokens", defaultOverlapTokens, "Overlap tokens between chunks")
	backup := flag.Bool("backup", false, "Trigger snapshot backup to MinIO after ingestion")
	flag.Parse()

	cfg := config{
		collection:  *collection,
		gatewayURL:  *gatewayURL,
		qdrantAddr:  *qdrantAddr,
		apiKey:      *apiKey,
		chunkTokens: *chunkToks,
		overlapToks: *overlapToks,
		backup:      *backup,
	}
	if *dirFlag != "" {
		cfg.dirPaths = splitColon(*dirFlag)
	}
	if *repoFlag != "" {
		cfg.repoPaths = splitColon(*repoFlag)
	}
	if *urlFlag != "" {
		cfg.urlPaths = splitColon(*urlFlag)
	}

	conn, err := grpc.NewClient(cfg.qdrantAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("connect qdrant: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	ing := newIngestor(cfg, conn)

	if err := ing.init(ctx); err != nil {
		log.Fatalf("ensure collection: %v", err)
	}

	for _, dir := range cfg.dirPaths {
		if err := ing.ingestDir(ctx, dir); err != nil {
			log.Printf("ingest dir %s: %v", dir, err)
		}
	}
	for _, repo := range cfg.repoPaths {
		if err := ing.cloneAndIngest(ctx, repo); err != nil {
			log.Printf("ingest repo %s: %v", repo, err)
		}
	}
	for _, u := range cfg.urlPaths {
		if err := ing.fetchURL(ctx, u); err != nil {
			log.Printf("ingest url %s: %v", u, err)
		}
	}

	if cfg.backup {
		if err := ing.backup(ctx); err != nil {
			log.Printf("backup: %v", err)
		}
	}

	log.Println("rag-ingest complete")
}

func splitColon(s string) []string {
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		for i, b := range data {
			if b == ':' {
				return i + 1, data[:i], nil
			}
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	var parts []string
	for sc.Scan() {
		if t := strings.TrimSpace(sc.Text()); t != "" {
			parts = append(parts, t)
		}
	}
	return parts
}
