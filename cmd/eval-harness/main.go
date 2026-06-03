// cmd/eval-harness evaluates trained LoRA adapters against a held-out dataset.
//
// Workflow:
//  1. Read the holdout set from datasets/<ns>/repos/<repo>/holdout.jsonl
//     (created during dataset-build when --holdout-ratio > 0).
//  2. For each example, send the prompt to the gateway and collect the
//     model's completion.
//  3. Compute token-overlap (F1), exact-match, and a simple BLEU-like
//     unigram precision score.
//  4. Write eval/<ns>/<repo>.json with per-example results and summary stats.
//  5. Exit 1 if the repo LoRA score is below the namespace baseline by more
//     than the configured regression threshold.
//
// Usage:
//
//	eval-harness [flags]
//
// Flags:
//
//	-gateway       gateway URL (default: http://localhost:8080)
//	-namespace     pipeline namespace to evaluate (required)
//	-repo          repo label to evaluate (empty = namespace-level only)
//	-ns-model      namespace model alias in Ollama (default: <namespace>/namespace)
//	-repo-model    repo model alias in Ollama (default: <namespace>/<repo>)
//	-datasets      base dataset directory (default: datasets)
//	-out           eval output directory (default: eval)
//	-threshold     max acceptable regression vs ns baseline (0–1, default: 0.05)
//	-max-examples  max holdout examples to evaluate (0 = all, default: 100)
//	-api-key       gateway API key (empty = open access)
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// holdoutExample is one line from holdout.jsonl.
type holdoutExample struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

// evalResult captures one generation result.
type evalResult struct {
	Source      string  `json:"source"`
	Reference   string  `json:"reference"`
	Generated   string  `json:"generated"`
	ExactMatch  bool    `json:"exact_match"`
	TokenF1     float64 `json:"token_f1"`
	UnigramPrec float64 `json:"unigram_precision"`
}

// evalSummary holds aggregate metrics for one model evaluation.
type evalSummary struct {
	Model         string       `json:"model"`
	Namespace     string       `json:"namespace"`
	Repo          string       `json:"repo"`
	ExactMatchAcc float64      `json:"exact_match_accuracy"`
	AvgTokenF1    float64      `json:"avg_token_f1"`
	AvgUnigramP   float64      `json:"avg_unigram_precision"`
	N             int          `json:"n_examples"`
	CreatedAt     time.Time    `json:"created_at"`
	Results       []evalResult `json:"results"`
}

func main() {
	gatewayURL := flag.String("gateway", "http://localhost:8080", "Gateway URL")
	nsName := flag.String("namespace", "", "Pipeline namespace (required)")
	repoName := flag.String("repo", "", "Repo label (empty = namespace-only)")
	nsModel := flag.String("ns-model", "", "Namespace model alias")
	repoModel := flag.String("repo-model", "", "Repo model alias")
	datasetsDir := flag.String("datasets", "datasets", "Base dataset directory")
	outDir := flag.String("out", "eval", "Eval output directory")
	threshold := flag.Float64("threshold", 0.05, "Max acceptable regression vs ns baseline (0–1)")
	maxExamples := flag.Int("max-examples", 100, "Max holdout examples (0 = all)")
	apiKey := flag.String("api-key", "", "Gateway API key")
	flag.Parse()

	if *nsName == "" {
		flag.Usage()
		log.Fatal("-namespace is required")
	}

	if *nsModel == "" {
		*nsModel = *nsName + "/namespace"
	}
	if *repoModel == "" && *repoName != "" {
		*repoModel = *nsName + "/" + *repoName
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	ctx := context.Background()

	// Evaluate namespace model.
	nsHoldout := filepath.Join(*datasetsDir, *nsName, "holdout.jsonl")
	nsSummary, err := evaluate(ctx, client, *gatewayURL, *apiKey, *nsModel, *nsName, "",
		nsHoldout, *maxExamples)
	if err != nil {
		log.Fatalf("evaluate namespace model: %v", err)
	}

	if err := writeSummary(*outDir, *nsName, "", nsSummary); err != nil {
		log.Fatalf("write ns summary: %v", err)
	}
	log.Printf("namespace model %s: exact_match=%.3f token_f1=%.3f (n=%d)",
		*nsModel, nsSummary.ExactMatchAcc, nsSummary.AvgTokenF1, nsSummary.N)

	if *repoName == "" {
		return
	}

	// Evaluate repo model.
	repoHoldout := filepath.Join(*datasetsDir, *nsName, "repos", *repoName, "holdout.jsonl")
	repoSummary, err := evaluate(ctx, client, *gatewayURL, *apiKey, *repoModel, *nsName, *repoName,
		repoHoldout, *maxExamples)
	if err != nil {
		log.Fatalf("evaluate repo model: %v", err)
	}

	if err := writeSummary(*outDir, *nsName, *repoName, repoSummary); err != nil {
		log.Fatalf("write repo summary: %v", err)
	}
	log.Printf("repo model %s: exact_match=%.3f token_f1=%.3f (n=%d)",
		*repoModel, repoSummary.ExactMatchAcc, repoSummary.AvgTokenF1, repoSummary.N)

	// Regression check.
	drop := nsSummary.AvgTokenF1 - repoSummary.AvgTokenF1
	if drop > *threshold {
		log.Fatalf("REGRESSION: repo model token_f1 %.3f is %.3f below namespace baseline %.3f (threshold %.3f)",
			repoSummary.AvgTokenF1, drop, nsSummary.AvgTokenF1, *threshold)
	}
	log.Printf("No regression detected (drop=%.3f ≤ threshold=%.3f)", drop, *threshold)
}

// evaluate runs inference for each holdout example and computes metrics.
func evaluate(ctx context.Context, client *http.Client, gatewayURL, apiKey, model, ns, repo,
	holdoutPath string, maxExamples int) (*evalSummary, error) {
	examples, err := loadHoldout(holdoutPath, maxExamples)
	if err != nil {
		return nil, fmt.Errorf("load holdout %s: %w", holdoutPath, err)
	}
	if len(examples) == 0 {
		return &evalSummary{Model: model, Namespace: ns, Repo: repo, CreatedAt: time.Now()}, nil
	}

	summary := &evalSummary{
		Model:     model,
		Namespace: ns,
		Repo:      repo,
		CreatedAt: time.Now(),
	}

	for _, ex := range examples {
		// Use first half of text as prompt, second half as reference.
		mid := len(ex.Text) / 2
		prompt := ex.Text[:mid]
		reference := strings.TrimSpace(ex.Text[mid:])

		generated, err := generate(ctx, client, gatewayURL, apiKey, model, prompt)
		if err != nil {
			log.Printf("generate error for %s: %v", ex.Source, err)
			continue
		}
		generated = strings.TrimSpace(generated)

		result := evalResult{
			Source:      ex.Source,
			Reference:   reference,
			Generated:   generated,
			ExactMatch:  generated == reference,
			TokenF1:     tokenF1(reference, generated),
			UnigramPrec: unigramPrecision(reference, generated),
		}
		summary.Results = append(summary.Results, result)
		summary.N++
	}

	if summary.N > 0 {
		var em, tf1, up float64
		for _, r := range summary.Results {
			if r.ExactMatch {
				em++
			}
			tf1 += r.TokenF1
			up += r.UnigramPrec
		}
		n := float64(summary.N)
		summary.ExactMatchAcc = em / n
		summary.AvgTokenF1 = tf1 / n
		summary.AvgUnigramP = up / n
	}

	return summary, nil
}

// generate sends a completion request to the gateway and returns the text.
func generate(ctx context.Context, client *http.Client, gatewayURL, apiKey, model, prompt string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", gatewayURL+"/v1/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Text string `json:"text"`
		} `json:"choices"`
		Response string `json:"response"` // Ollama native format
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Choices) > 0 {
		return result.Choices[0].Text, nil
	}
	return result.Response, nil
}

// loadHoldout reads up to maxExamples lines from a JSONL holdout file.
func loadHoldout(path string, max int) ([]holdoutExample, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var examples []holdoutExample
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if max > 0 && len(examples) >= max {
			break
		}
		var ex holdoutExample
		if err := json.Unmarshal(scanner.Bytes(), &ex); err != nil {
			continue
		}
		examples = append(examples, ex)
	}
	return examples, scanner.Err()
}

// writeSummary writes the eval summary JSON to eval/<ns>/<repo>.json.
func writeSummary(outDir, ns, repo string, summary *evalSummary) error {
	dir := filepath.Join(outDir, ns)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := "namespace.json"
	if repo != "" {
		name = repo + ".json"
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

// -------------------------------------------------------------------------
// Metric helpers
// -------------------------------------------------------------------------

// tokenF1 computes token-level F1 between reference and generated strings.
func tokenF1(ref, gen string) float64 {
	refTokens := tokenize(ref)
	genTokens := tokenize(gen)
	if len(refTokens) == 0 && len(genTokens) == 0 {
		return 1.0
	}
	if len(refTokens) == 0 || len(genTokens) == 0 {
		return 0.0
	}
	common := countCommon(refTokens, genTokens)
	precision := float64(common) / float64(len(genTokens))
	recall := float64(common) / float64(len(refTokens))
	if precision+recall == 0 {
		return 0.0
	}
	return 2 * precision * recall / (precision + recall)
}

// unigramPrecision computes the fraction of generated tokens found in reference.
func unigramPrecision(ref, gen string) float64 {
	refTokens := tokenize(ref)
	genTokens := tokenize(gen)
	if len(genTokens) == 0 {
		return 0.0
	}
	common := countCommon(refTokens, genTokens)
	return float64(common) / float64(len(genTokens))
}

// tokenize lowercases and splits on whitespace.
func tokenize(s string) []string {
	return strings.Fields(strings.ToLower(s))
}

// countCommon counts tokens in gen that also appear in ref (bag intersection).
func countCommon(ref, gen []string) int {
	counts := make(map[string]int, len(ref))
	for _, t := range ref {
		counts[t]++
	}
	common := 0
	for _, t := range gen {
		if counts[t] > 0 {
			common++
			counts[t]--
		}
	}
	return common
}
