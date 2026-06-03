// cmd/pipeline orchestrates the full fine-tuning pipeline for each namespace:
//
//  1. sync         — run cmd/repo-sync to update repo-cache
//  2. dataset      — run cmd/dataset-build to produce JSONL datasets
//  3. train-ns     — invoke python/train.py --mode namespace
//  4. train-repo   — invoke python/train.py --mode repo for each repo
//  5. convert      — convert PEFT adapters to GGUF via convert_lora_to_gguf.py
//  6. modelfiles   — run cmd/modelfile-gen to emit Ollama Modelfiles
//  7. register     — run cmd/registry push for each produced model
//
// Each stage is run in order.  -skip and -only flags control which stages
// execute.  A Python training stage that exits 2 is treated as "skipped"
// (repo below min_samples threshold) rather than an error.
//
// Usage:
//
//	pipeline [flags]
//
// Flags:
//
//	-namespaces    path to namespaces.yaml (default: configs/namespaces.yaml)
//	-namespace     if set, only run this namespace
//	-skip          comma-separated list of stage names to skip
//	-only          comma-separated list of stage names to run (all others skipped)
//	-dry-run       print commands without executing them
//	-python        path to Python interpreter (default: python3)
//	-train-script  path to train.py (default: python/train.py)
//	-checkpoints   base checkpoint directory (default: checkpoints)
//	-datasets      base dataset directory (default: datasets)
//	-repo-cache    repo-cache directory (default: repo-cache)
//	-modelfiles    modelfile output directory (default: modelfiles)
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// NamespacesFile mirrors configs/namespaces.yaml.
type NamespacesFile struct {
	Global     GlobalConfig      `yaml:"global"`
	Namespaces []NamespaceConfig `yaml:"namespaces"`
}

// GlobalConfig holds top-level defaults.
type GlobalConfig struct {
	RepoMinSamples int `yaml:"repo_min_samples"`
}

// NamespaceConfig is one namespace entry.
type NamespaceConfig struct {
	Name           string      `yaml:"name"`
	BaseModel      string      `yaml:"base_model"`
	SkipRepoLoRA   []string    `yaml:"skip_repo_lora"`
	RepoMinSamples int         `yaml:"repo_min_samples"`
	Repos          []RepoEntry `yaml:"repos"`
}

// RepoEntry is one repo within a namespace.
type RepoEntry struct {
	Label  string `yaml:"label"`
	URL    string `yaml:"url"`
	Branch string `yaml:"branch"`
}

// pipelineConfig holds resolved CLI flag values.
type pipelineConfig struct {
	NamespacesPath string
	NSFilter       string
	Skip           map[string]bool
	Only           map[string]bool
	DryRun         bool
	Python         string
	TrainScript    string
	Checkpoints    string
	Datasets       string
	RepoCache      string
	Modelfiles     string
}

// All stage names in order.
var allStages = []string{"sync", "dataset", "train-ns", "train-repo", "convert", "modelfiles", "register"}

func main() {
	namespacesPath := flag.String("namespaces", "configs/namespaces.yaml", "Path to namespaces.yaml")
	nsFilter := flag.String("namespace", "", "Only run this namespace")
	skipStr := flag.String("skip", "", "Comma-separated stages to skip")
	onlyStr := flag.String("only", "", "Comma-separated stages to run")
	dryRun := flag.Bool("dry-run", false, "Print commands without executing")
	python := flag.String("python", "python3", "Python interpreter")
	trainScript := flag.String("train-script", "python/train.py", "Path to train.py")
	checkpoints := flag.String("checkpoints", "checkpoints", "Checkpoint directory")
	datasets := flag.String("datasets", "datasets", "Dataset directory")
	repoCache := flag.String("repo-cache", "repo-cache", "Repo-cache directory")
	modelfiles := flag.String("modelfiles", "modelfiles", "Modelfile output directory")
	flag.Parse()

	cfg := &pipelineConfig{
		NamespacesPath: *namespacesPath,
		NSFilter:       *nsFilter,
		Skip:           parseSet(*skipStr),
		Only:           parseSet(*onlyStr),
		DryRun:         *dryRun,
		Python:         *python,
		TrainScript:    *trainScript,
		Checkpoints:    *checkpoints,
		Datasets:       *datasets,
		RepoCache:      *repoCache,
		Modelfiles:     *modelfiles,
	}

	nsf, err := loadNamespaces(cfg.NamespacesPath)
	if err != nil {
		log.Fatalf("load namespaces: %v", err)
	}

	for _, ns := range nsf.Namespaces {
		if cfg.NSFilter != "" && ns.Name != cfg.NSFilter {
			continue
		}
		if err := runNamespace(cfg, nsf, ns); err != nil {
			log.Fatalf("namespace %s: %v", ns.Name, err)
		}
	}
}

// runNamespace executes all enabled pipeline stages for one namespace.
func runNamespace(cfg *pipelineConfig, nsf *NamespacesFile, ns NamespaceConfig) error {
	log.Printf("=== namespace: %s ===", ns.Name)

	// Stage: sync
	if cfg.stageEnabled("sync") {
		if err := runStep(cfg, "repo-sync",
			"-namespaces", cfg.NamespacesPath,
			"-cache-dir", cfg.RepoCache,
			"-namespace", ns.Name,
		); err != nil {
			return fmt.Errorf("sync: %w", err)
		}
	}

	// Stage: dataset
	if cfg.stageEnabled("dataset") {
		if err := runStep(cfg, "dataset-build",
			"-namespaces", cfg.NamespacesPath,
			"-repo-cache", cfg.RepoCache,
			"-out", cfg.Datasets,
			"-namespace", ns.Name,
		); err != nil {
			return fmt.Errorf("dataset: %w", err)
		}
	}

	// Stage: train-ns
	if cfg.stageEnabled("train-ns") {
		nsDataset := filepath.Join(cfg.Datasets, ns.Name, "dataset.jsonl")
		nsOut := filepath.Join(cfg.Checkpoints, ns.Name, "namespace")
		if err := runPython(cfg, ns.Name, "", nsDataset, nsOut); err != nil {
			return fmt.Errorf("train-ns: %w", err)
		}
	}

	// Stage: train-repo
	if cfg.stageEnabled("train-repo") {
		for _, repo := range ns.Repos {
			if skipRepo(ns, repo.Label) {
				log.Printf("  skip repo %s (in skip_repo_lora)", repo.Label)
				continue
			}
			repoDataset := filepath.Join(cfg.Datasets, ns.Name, "repos", repo.Label, "dataset.jsonl")
			repoOut := filepath.Join(cfg.Checkpoints, ns.Name, "repos", repo.Label)
			nsBase := filepath.Join(cfg.Checkpoints, ns.Name, "namespace", "merged")
			if err := runPythonRepo(cfg, ns.Name, repo.Label, repoDataset, repoOut, nsBase); err != nil {
				// Exit code 2 = skipped (below min_samples), not a fatal error.
				if isSkipCode(err) {
					log.Printf("  repo %s skipped (below min_samples)", repo.Label)
					continue
				}
				return fmt.Errorf("train-repo %s: %w", repo.Label, err)
			}
		}
	}

	// Stage: convert
	if cfg.stageEnabled("convert") {
		if err := runConvert(cfg, ns); err != nil {
			log.Printf("  convert: %v (non-fatal)", err)
		}
	}

	// Stage: modelfiles
	if cfg.stageEnabled("modelfiles") {
		if err := runStep(cfg, "modelfile-gen",
			"-namespaces", cfg.NamespacesPath,
			"-checkpoints", cfg.Checkpoints,
			"-out", cfg.Modelfiles,
			"-namespace", ns.Name,
		); err != nil {
			return fmt.Errorf("modelfiles: %w", err)
		}
	}

	// Stage: register
	if cfg.stageEnabled("register") {
		if err := registerOutputs(cfg, ns); err != nil {
			log.Printf("  register: %v (non-fatal)", err)
		}
	}

	return nil
}

// runStep runs a Go binary from PATH with the given args.
func runStep(cfg *pipelineConfig, bin string, args ...string) error {
	return runCmd(cfg, bin, args...)
}

// runPython invokes train.py in namespace mode.
func runPython(cfg *pipelineConfig, namespace, _ /* unused */, dataset, outDir string) error {
	return runCmd(cfg, cfg.Python, cfg.TrainScript,
		"--mode", "namespace",
		"--namespace", namespace,
		"--namespaces", cfg.NamespacesPath,
		"--dataset-dir", dataset,
		"--output-dir", outDir,
	)
}

// runPythonRepo invokes train.py in repo mode.
func runPythonRepo(cfg *pipelineConfig, namespace, repo, dataset, outDir, baseModel string) error {
	return runCmd(cfg, cfg.Python, cfg.TrainScript,
		"--mode", "repo",
		"--namespace", namespace,
		"--repo", repo,
		"--namespaces", cfg.NamespacesPath,
		"--dataset-dir", dataset,
		"--base-model", baseModel,
		"--output-dir", outDir,
	)
}

// runConvert runs tools/setup-llama-cpp.sh then calls convert_lora_to_gguf.py
// for any adapter that has a merged dir but no gguf yet.
func runConvert(cfg *pipelineConfig, ns NamespaceConfig) error {
	setupScript := "tools/setup-llama-cpp.sh"
	if _, err := os.Stat(setupScript); err != nil {
		return fmt.Errorf("setup-llama-cpp.sh not found: %w", err)
	}
	// The script prints CONVERT_SCRIPT=<path> as its last line.
	out, err := exec.Command("bash", setupScript).Output()
	if err != nil {
		return fmt.Errorf("setup-llama-cpp: %w", err)
	}
	convertScript := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "CONVERT_SCRIPT=") {
			convertScript = strings.TrimPrefix(line, "CONVERT_SCRIPT=")
			convertScript = strings.TrimSpace(convertScript)
		}
	}
	if convertScript == "" {
		return fmt.Errorf("CONVERT_SCRIPT not found in setup-llama-cpp.sh output")
	}

	var convErrs []error

	// Convert namespace adapter.
	nsAdapterDir := filepath.Join(cfg.Checkpoints, ns.Name, "namespace", "merged")
	nsGGUF := filepath.Join(cfg.Checkpoints, ns.Name, "namespace", "model.gguf")
	if _, err := os.Stat(nsAdapterDir); err == nil {
		if _, err := os.Stat(nsGGUF); err != nil {
			if err := runCmd(cfg, cfg.Python, convertScript, nsAdapterDir, "--outfile", nsGGUF); err != nil {
				log.Printf("  convert namespace adapter: %v", err)
				convErrs = append(convErrs, err)
			}
		}
	}

	// Convert per-repo adapters.
	for _, repo := range ns.Repos {
		repoMergedDir := filepath.Join(cfg.Checkpoints, ns.Name, "repos", repo.Label, "merged")
		repoGGUF := filepath.Join(cfg.Checkpoints, ns.Name, "repos", repo.Label, "model.gguf")
		if _, err := os.Stat(repoMergedDir); err == nil {
			if _, err := os.Stat(repoGGUF); err != nil {
				if err := runCmd(cfg, cfg.Python, convertScript, repoMergedDir, "--outfile", repoGGUF); err != nil {
					log.Printf("  convert repo %s: %v", repo.Label, err)
					convErrs = append(convErrs, fmt.Errorf("repo %s: %w", repo.Label, err))
				}
			}
		}
	}

	if len(convErrs) > 0 {
		return fmt.Errorf("conversion errors: %w", errors.Join(convErrs...))
	}
	return nil
}

// registerOutputs pushes produced GGUFs to the model registry.
func registerOutputs(cfg *pipelineConfig, ns NamespaceConfig) error {
	nsGGUF := filepath.Join(cfg.Checkpoints, ns.Name, "namespace", "model.gguf")
	if _, err := os.Stat(nsGGUF); err == nil {
		if err := runCmd(cfg, "registry", "push", nsGGUF,
			"--name", ns.Name+"/namespace",
		); err != nil {
			log.Printf("  register namespace gguf: %v", err)
		}
	}

	for _, repo := range ns.Repos {
		repoGGUF := filepath.Join(cfg.Checkpoints, ns.Name, "repos", repo.Label, "model.gguf")
		if _, err := os.Stat(repoGGUF); err == nil {
			if err := runCmd(cfg, "registry", "push", repoGGUF,
				"--name", ns.Name+"/"+repo.Label,
			); err != nil {
				log.Printf("  register repo %s: %v", repo.Label, err)
			}
		}
	}
	return nil
}

// runCmd executes a command, logging its output.  In dry-run mode it only
// prints the command.
func runCmd(cfg *pipelineConfig, name string, args ...string) error {
	all := append([]string{name}, args...)
	log.Printf("  run: %s", strings.Join(all, " "))
	if cfg.DryRun {
		return nil
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// isSkipCode returns true when the command exited with code 2.
func isSkipCode(err error) bool {
	if err == nil {
		return false
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode() == 2
	}
	return false
}

// skipRepo returns true if repo is in the namespace's skip_repo_lora list.
func skipRepo(ns NamespaceConfig, label string) bool {
	for _, s := range ns.SkipRepoLoRA {
		if s == label {
			return true
		}
	}
	return false
}

// stageEnabled returns true if stage should run given skip/only flags.
func (cfg *pipelineConfig) stageEnabled(stage string) bool {
	if len(cfg.Only) > 0 {
		return cfg.Only[stage]
	}
	return !cfg.Skip[stage]
}

// parseSet splits a comma-separated string into a set.
func parseSet(s string) map[string]bool {
	m := make(map[string]bool)
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			m[item] = true
		}
	}
	return m
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
