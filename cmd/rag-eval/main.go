// cmd/rag-eval runs evaluation jobs against a RAG collection.
//
// For each collection it:
//  1. Loads a curated QA set from <data-dir>/<collection>/qa.jsonl
//     (fields: question, expected_answer, expected_files)
//  2. Calls /rag/query for each question and measures recall@k
//     (fraction of expected_files present in top-k results)
//  3. Calls /rag/answer for each question and measures answer
//     faithfulness using an LLM judge (yes/no) via /v1/chat/completions
//  4. Measures p50/p95 latency for both endpoints
//  5. Writes results to <output-dir>/<collection>-<date>.json and
//     prints a summary to stdout
//
// Exit code 1 if recall@k drops below --min-recall threshold.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// -------------------------------------------------------------------------
// QA set types
// -------------------------------------------------------------------------

type qaItem struct {
	Question       string   `json:"question"`
	ExpectedAnswer string   `json:"expected_answer"`
	ExpectedFiles  []string `json:"expected_files"`
}

// -------------------------------------------------------------------------
// Eval result types
// -------------------------------------------------------------------------

type collectionResult struct {
	Collection       string        `json:"collection"`
	Date             string        `json:"date"`
	RecallAtK        float64       `json:"recall_at_k"`
	Faithfulness     float64       `json:"faithfulness"`
	LatencyQueryP50  time.Duration `json:"latency_query_p50_ms"`
	LatencyQueryP95  time.Duration `json:"latency_query_p95_ms"`
	LatencyAnswerP50 time.Duration `json:"latency_answer_p50_ms"`
	LatencyAnswerP95 time.Duration `json:"latency_answer_p95_ms"`
	Total            int           `json:"total"`
	Passed           int           `json:"passed"`
}

// -------------------------------------------------------------------------
// HTTP helpers
// -------------------------------------------------------------------------

type ragClient struct {
	ragURL     string
	gatewayURL string
	authHeader string
	httpClient *http.Client
}

func newRAGClient(ragURL, gatewayURL, apiKey string) *ragClient {
	auth := ""
	if apiKey != "" {
		auth = "Bearer " + apiKey
	}
	return &ragClient{
		ragURL:     ragURL,
		gatewayURL: gatewayURL,
		authHeader: auth,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *ragClient) query(ctx context.Context, question, collection string, topK int) ([]string, time.Duration, error) {
	body := map[string]any{
		"query":      question,
		"collection": collection,
		"top_k":      topK,
	}
	start := time.Now()
	data, _ := json.Marshal(body)
	resp, err := c.post(ctx, c.ragURL+"/rag/query", data)
	elapsed := time.Since(start)
	if err != nil {
		return nil, elapsed, err
	}
	var qr struct {
		Results []struct {
			File string `json:"file"`
		} `json:"results"`
	}
	if err := json.Unmarshal(resp, &qr); err != nil {
		return nil, elapsed, err
	}
	files := make([]string, 0, len(qr.Results))
	for _, r := range qr.Results {
		files = append(files, r.File)
	}
	return files, elapsed, nil
}

func (c *ragClient) answer(ctx context.Context, question, collection string) (string, time.Duration, error) {
	body := map[string]any{
		"query":      question,
		"collection": collection,
	}
	start := time.Now()
	data, _ := json.Marshal(body)
	resp, err := c.post(ctx, c.ragURL+"/rag/answer", data)
	elapsed := time.Since(start)
	if err != nil {
		return "", elapsed, err
	}
	var ar struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(resp, &ar); err != nil {
		return "", elapsed, err
	}
	return ar.Answer, elapsed, nil
}

func (c *ragClient) judgeAnswer(ctx context.Context, question, answer, expected string) (bool, error) {
	prompt := fmt.Sprintf(
		"Question: %s\nExpected answer concept: %s\nGiven answer: %s\n\n"+
			"Does the given answer correctly address the question and match the expected concept? Reply only YES or NO.",
		question, expected, answer,
	)
	body := map[string]any{
		"model": "llama3",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	data, _ := json.Marshal(body)
	resp, err := c.post(ctx, c.gatewayURL+"/v1/chat/completions", data)
	if err != nil {
		return false, err
	}
	var cr struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(resp, &cr); err != nil {
		return false, err
	}
	if len(cr.Choices) == 0 {
		return false, nil
	}
	return strings.Contains(strings.ToUpper(cr.Choices[0].Message.Content), "YES"), nil
}

func (c *ragClient) post(ctx context.Context, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authHeader != "" {
		req.Header.Set("Authorization", c.authHeader)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, err = buf.ReadFrom(resp.Body)
	return buf.Bytes(), err
}

// -------------------------------------------------------------------------
// Evaluation logic
// -------------------------------------------------------------------------

func evalCollection(ctx context.Context, client *ragClient, collection, qaFile string, topK int) (*collectionResult, error) {
	data, err := os.ReadFile(qaFile)
	if err != nil {
		return nil, fmt.Errorf("read qa file: %w", err)
	}

	var items []qaItem
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var item qaItem
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			log.Printf("skip bad QA line: %v", err)
			continue
		}
		items = append(items, item)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("no QA items in %s", qaFile)
	}

	result := &collectionResult{
		Collection: collection,
		Date:       time.Now().UTC().Format(time.DateOnly),
		Total:      len(items),
	}

	var queryLatencies, answerLatencies []time.Duration
	recallHits := 0
	faithHits := 0

	for _, item := range items {
		files, ql, err := client.query(ctx, item.Question, collection, topK)
		queryLatencies = append(queryLatencies, ql)
		if err != nil {
			log.Printf("query error: %v", err)
			continue
		}
		if recallHit(files, item.ExpectedFiles) {
			recallHits++
		}

		answer, al, err := client.answer(ctx, item.Question, collection)
		answerLatencies = append(answerLatencies, al)
		if err != nil {
			log.Printf("answer error: %v", err)
			continue
		}
		faithful, err := client.judgeAnswer(ctx, item.Question, answer, item.ExpectedAnswer)
		if err != nil {
			log.Printf("judge error: %v", err)
			continue
		}
		if faithful {
			faithHits++
			result.Passed++
		}
	}

	result.RecallAtK = float64(recallHits) / float64(len(items))
	result.Faithfulness = float64(faithHits) / float64(len(items))
	result.LatencyQueryP50 = percentile(queryLatencies, 50)
	result.LatencyQueryP95 = percentile(queryLatencies, 95)
	result.LatencyAnswerP50 = percentile(answerLatencies, 50)
	result.LatencyAnswerP95 = percentile(answerLatencies, 95)

	return result, nil
}

func recallHit(retrieved, expected []string) bool {
	for _, e := range expected {
		for _, r := range retrieved {
			if strings.HasSuffix(r, e) || strings.HasSuffix(e, r) {
				return true
			}
		}
	}
	return len(expected) == 0
}

func percentile(durations []time.Duration, p int) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// -------------------------------------------------------------------------
// Main
// -------------------------------------------------------------------------

func main() {
	ragURL := flag.String("rag-url", "http://localhost:8081", "RAG service base URL")
	gatewayURL := flag.String("gateway-url", "http://localhost:8080", "Gateway base URL")
	dataDir := flag.String("data-dir", "eval/rag", "Directory containing per-collection QA sets")
	outputDir := flag.String("output-dir", "eval/rag/results", "Directory for result JSON files")
	apiKey := flag.String("api-key", os.Getenv("GATEWAY_API_KEY"), "Gateway API key")
	topK := flag.Int("top-k", 5, "Top-k for recall evaluation")
	minRecall := flag.Float64("min-recall", 0.5, "Minimum recall@k; exit 1 if below")
	flag.Parse()

	collections := flag.Args()
	if len(collections) == 0 {
		entries, err := os.ReadDir(*dataDir)
		if err != nil {
			log.Fatalf("read data dir: %v", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				collections = append(collections, e.Name())
			}
		}
	}

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	client := newRAGClient(*ragURL, *gatewayURL, *apiKey)
	ctx := context.Background()

	allPassed := true
	for _, coll := range collections {
		qaFile := filepath.Join(*dataDir, coll, "qa.jsonl")
		if _, err := os.Stat(qaFile); os.IsNotExist(err) {
			log.Printf("skip %s: no qa.jsonl", coll)
			continue
		}

		result, err := evalCollection(ctx, client, coll, qaFile, *topK)
		if err != nil {
			log.Printf("eval %s: %v", coll, err)
			continue
		}

		fmt.Printf("[%s] recall@%d=%.2f faithfulness=%.2f query_p50=%s answer_p50=%s\n",
			coll, *topK, result.RecallAtK, result.Faithfulness,
			result.LatencyQueryP50.Round(time.Millisecond),
			result.LatencyAnswerP50.Round(time.Millisecond))

		if result.RecallAtK < *minRecall {
			fmt.Printf("[%s] FAIL: recall@%d %.2f < %.2f\n",
				coll, *topK, result.RecallAtK, *minRecall)
			allPassed = false
		}

		outFile := filepath.Join(*outputDir, fmt.Sprintf("%s-%s.json", coll, result.Date))
		b, _ := json.MarshalIndent(result, "", "  ")
		if err := os.WriteFile(outFile, b, 0o644); err != nil {
			log.Printf("write result: %v", err)
		}
	}

	if !allPassed {
		os.Exit(1)
	}
}
