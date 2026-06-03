// cmd/dataset-build produces training datasets from a repo cache.
//
// It reads configs/namespaces.yaml, walks each repo's bare clone under
// repo-cache/<label>/, and emits two JSONL files:
//
//	dataset.jsonl                  — namespace-wide (all repos merged)
//	repos/<label>/dataset.jsonl   — per-repo dataset
//
// Each JSONL line is a text/chat training example:
//
//	{"text": "<file content>", "source": "<repo>/<path>"}
//
// Deduplication is by SHA-256 of the UTF-8 content.  Files outside the
// [min_file_bytes, max_file_bytes] window are skipped.
//
// Usage:
//
//	dataset-build [flags]
//
// Flags:
//
//	-namespaces   path to namespaces.yaml (default: configs/namespaces.yaml)
//	-repo-cache   path to bare-clone cache (default: repo-cache)
//	-out          output base directory (default: datasets)
//	-namespace    if set, only process this namespace
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// NamespacesFile mirrors the top-level structure of configs/namespaces.yaml.
type NamespacesFile struct {
	Global     GlobalConfig      `yaml:"global"`
	Namespaces []NamespaceConfig `yaml:"namespaces"`
}

// GlobalConfig holds defaults applied to every namespace.
type GlobalConfig struct {
	BaseModel      string      `yaml:"base_model"`
	Quantization   string      `yaml:"quantization"`
	RepoMinSamples int         `yaml:"repo_min_samples"`
	Hyperparams    Hyperparams `yaml:"hyperparams"`
}

// NamespaceConfig is a single namespace entry.
type NamespaceConfig struct {
	Name           string      `yaml:"name"`
	BaseModel      string      `yaml:"base_model"`
	Quantization   string      `yaml:"quantization"`
	SkipRepoLoRA   []string    `yaml:"skip_repo_lora"`
	RepoMinSamples int         `yaml:"repo_min_samples"`
	Repos          []RepoEntry `yaml:"repos"`
	Hyperparams    Hyperparams `yaml:"hyperparams"`
}

// RepoEntry is a single repo within a namespace.
type RepoEntry struct {
	Label  string `yaml:"label"`
	URL    string `yaml:"url"`
	Branch string `yaml:"branch"`
}

// Hyperparams holds training hyperparameter values.
// Pointer fields (MaxSteps, LearningRate) allow a namespace to explicitly
// override a global default to zero (e.g. max_steps: 0 for early-stopping).
type Hyperparams struct {
	LoraRank     int      `yaml:"lora_rank"`
	LoraAlpha    int      `yaml:"lora_alpha"`
	BatchSize    int      `yaml:"batch_size"`
	GradAccum    int      `yaml:"grad_accum"`
	MaxSteps     *int     `yaml:"max_steps"`
	LearningRate *float64 `yaml:"learning_rate"`
	MaxSeqLength int      `yaml:"max_seq_length"`
	MinFileBytes int      `yaml:"min_file_bytes"`
	MaxFileBytes int      `yaml:"max_file_bytes"`
}

// example is a single JSONL training record.
type example struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

func main() {
	namespacesPath := flag.String("namespaces", "configs/namespaces.yaml", "Path to namespaces.yaml")
	repoCache := flag.String("repo-cache", "repo-cache", "Path to bare-clone cache")
	outDir := flag.String("out", "datasets", "Output base directory")
	nsFilter := flag.String("namespace", "", "Only process this namespace (empty = all)")
	flag.Parse()

	nsFile, err := loadNamespaces(*namespacesPath)
	if err != nil {
		log.Fatalf("load namespaces: %v", err)
	}

	for _, ns := range nsFile.Namespaces {
		if *nsFilter != "" && ns.Name != *nsFilter {
			continue
		}

		hp := mergeHyperparams(nsFile.Global.Hyperparams, ns.Hyperparams)
		nsOutDir := filepath.Join(*outDir, ns.Name)
		if err := os.MkdirAll(nsOutDir, 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", nsOutDir, err)
		}

		nsOutFile, err := os.Create(filepath.Join(nsOutDir, "dataset.jsonl"))
		if err != nil {
			log.Fatalf("create dataset: %v", err)
		}
		nsWriter := bufio.NewWriter(nsOutFile)
		nsSeen := make(map[string]struct{})

		for _, repo := range ns.Repos {
			repoDir := filepath.Join(*repoCache, repo.Label)
			if _, err := os.Stat(repoDir); err != nil {
				log.Printf("repo %s: cache dir %s not found, skipping", repo.Label, repoDir)
				continue
			}

			repoOutDir := filepath.Join(*outDir, ns.Name, "repos", repo.Label)
			if err := os.MkdirAll(repoOutDir, 0o755); err != nil {
				log.Fatalf("mkdir %s: %v", repoOutDir, err)
			}
			repoFile, err := os.Create(filepath.Join(repoOutDir, "dataset.jsonl"))
			if err != nil {
				log.Fatalf("create repo dataset: %v", err)
			}
			repoWriter := bufio.NewWriter(repoFile)
			repoSeen := make(map[string]struct{})

			count, err := walkRepo(repoDir, repo.Label, hp, repoWriter, nsWriter, repoSeen, nsSeen)
			if err != nil {
				log.Printf("walk repo %s: %v", repo.Label, err)
			}

			if err := repoWriter.Flush(); err != nil {
				log.Fatalf("flush repo writer for %s: %v", repo.Label, err)
			}
			if err := repoFile.Close(); err != nil {
				log.Fatalf("close repo file for %s: %v", repo.Label, err)
			}
			log.Printf("namespace=%s repo=%s examples=%d", ns.Name, repo.Label, count)
		}

		if err := nsWriter.Flush(); err != nil {
			log.Fatalf("flush namespace writer for %s: %v", ns.Name, err)
		}
		if err := nsOutFile.Close(); err != nil {
			log.Fatalf("close namespace file for %s: %v", ns.Name, err)
		}
		log.Printf("namespace=%s dataset written to %s", ns.Name, nsOutDir)
	}
}

// writeLine writes data followed by a newline to the writer, returning an error if either write fails.
func writeLine(w *bufio.Writer, data []byte) error {
	if _, err := w.Write(data); err != nil {
		return err
	}
	return w.WriteByte('\n')
}

// walkRepo walks a bare-clone or regular git repo directory and writes
// qualifying files as JSONL examples.
func walkRepo(repoDir, repoLabel string, hp Hyperparams, repoW, nsW *bufio.Writer, repoSeen, nsSeen map[string]struct{}) (int, error) {
	count := 0
	err := filepath.WalkDir(repoDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			// Skip .git internals and hidden dirs.
			base := d.Name()
			if base == "objects" || base == "refs" || base == "logs" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Size filter.
		sz := len(data)
		if sz < hp.MinFileBytes || (hp.MaxFileBytes > 0 && sz > hp.MaxFileBytes) {
			return nil
		}

		// Skip binary files.
		if isBinary(data) {
			return nil
		}

		text := string(data)
		hash := contentHash(text)

		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			return fmt.Errorf("filepath rel for %q: %w", path, err)
		}
		src := repoLabel + "/" + rel

		ex := example{Text: text, Source: src}
		line, err := json.Marshal(ex)
		if err != nil {
			return fmt.Errorf("marshal example for %q: %w", src, err)
		}

		if _, seen := repoSeen[hash]; !seen {
			repoSeen[hash] = struct{}{}
			if err := writeLine(repoW, line); err != nil {
				return err
			}
			count++
		}
		if _, seen := nsSeen[hash]; !seen {
			nsSeen[hash] = struct{}{}
			if err := writeLine(nsW, line); err != nil {
				return err
			}
		}
		return nil
	})
	return count, err
}

// isBinary returns true if data contains a null byte (heuristic for binary).
func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// contentHash returns a hex SHA-256 of the content.
func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// mergeHyperparams returns global defaults overridden by any non-nil/non-zero ns values.
// Pointer fields (MaxSteps, LearningRate) allow a namespace to override a global
// default to zero (e.g. max_steps: 0 for early-stopping only).
func mergeHyperparams(global, ns Hyperparams) Hyperparams {
	out := global
	if ns.LoraRank != 0 {
		out.LoraRank = ns.LoraRank
	}
	if ns.LoraAlpha != 0 {
		out.LoraAlpha = ns.LoraAlpha
	}
	if ns.BatchSize != 0 {
		out.BatchSize = ns.BatchSize
	}
	if ns.GradAccum != 0 {
		out.GradAccum = ns.GradAccum
	}
	if ns.MaxSteps != nil {
		out.MaxSteps = ns.MaxSteps
	}
	if ns.LearningRate != nil {
		out.LearningRate = ns.LearningRate
	}
	if ns.MaxSeqLength != 0 {
		out.MaxSeqLength = ns.MaxSeqLength
	}
	if ns.MinFileBytes != 0 {
		out.MinFileBytes = ns.MinFileBytes
	}
	if ns.MaxFileBytes != 0 {
		out.MaxFileBytes = ns.MaxFileBytes
	}
	return out
}

// loadNamespaces reads and parses the namespaces YAML file.
func loadNamespaces(path string) (*NamespacesFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var nsf NamespacesFile
	if err := yaml.Unmarshal(data, &nsf); err != nil {
		return nil, err
	}
	return &nsf, nil
}
