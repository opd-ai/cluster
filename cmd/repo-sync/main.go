// cmd/repo-sync clones or updates repositories listed in
// configs/namespaces.yaml into a shared bare-clone cache (repo-cache/).
//
// Nodes pull from the cache via:
//
//	git fetch --reference /path/to/repo-cache/<label>
//
// This avoids re-downloading the full history on every node.
//
// Usage:
//
//	repo-sync [flags]
//
// Flags:
//
//	-namespaces   path to namespaces.yaml (default: configs/namespaces.yaml)
//	-cache-dir    root of the bare-clone cache (default: repo-cache)
//	-depth        shallow clone depth, 0 = full history (default: 0)
//	-jobs         parallel sync workers (default: 4)
//	-dry-run      log operations without performing them
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// NamespacesFile is the top-level structure of configs/namespaces.yaml.
type NamespacesFile struct {
	Namespaces []Namespace `yaml:"namespaces"`
}

// Namespace groups a set of repositories under a shared training label.
type Namespace struct {
	Name  string `yaml:"name"`
	Repos []Repo `yaml:"repos"`
}

// Repo describes a single repository to be cloned/updated.
type Repo struct {
	Label string `yaml:"label"`
	URL   string `yaml:"url"`
}

// SyncConfig holds runtime configuration.
type SyncConfig struct {
	NamespacesPath string
	CacheDir       string
	Depth          int
	Jobs           int
	DryRun         bool
}

func main() {
	cfg := SyncConfig{}
	flag.StringVar(&cfg.NamespacesPath, "namespaces", "configs/namespaces.yaml", "Path to namespaces.yaml")
	flag.StringVar(&cfg.CacheDir, "cache-dir", "repo-cache", "Root of the bare-clone cache")
	flag.IntVar(&cfg.Depth, "depth", 0, "Shallow clone depth (0 = full history)")
	flag.IntVar(&cfg.Jobs, "jobs", 4, "Parallel sync workers")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Log without performing operations")
	flag.Parse()

	repos, err := loadRepos(cfg.NamespacesPath)
	if err != nil {
		log.Fatalf("load namespaces: %v", err)
	}

	if len(repos) == 0 {
		fmt.Println("no repositories configured in namespaces.yaml")
		return
	}

	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		log.Fatalf("create cache dir: %v", err)
	}

	work := make(chan Repo, len(repos))
	for _, r := range repos {
		work <- r
	}
	close(work)

	var wg sync.WaitGroup
	for i := 0; i < cfg.Jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range work {
				if err := syncRepo(repo, cfg); err != nil {
					log.Printf("sync %s: %v", repo.Label, err)
				}
			}
		}()
	}
	wg.Wait()
}

// syncRepo clones (or fetches) a single repository into the cache.
func syncRepo(repo Repo, cfg SyncConfig) error {
	dest := filepath.Join(cfg.CacheDir, sanitizeLabel(repo.Label))

	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return cloneRepo(repo.URL, dest, cfg)
	}
	return fetchRepo(dest, cfg)
}

func cloneRepo(url, dest string, cfg SyncConfig) error {
	args := []string{"clone", "--bare"}
	if cfg.Depth > 0 {
		args = append(args, fmt.Sprintf("--depth=%d", cfg.Depth))
	}
	args = append(args, "--filter=blob:none", url, dest)
	return gitCmd(args, cfg.DryRun)
}

func fetchRepo(dest string, cfg SyncConfig) error {
	args := []string{"-C", dest, "fetch", "--all", "--prune"}
	if cfg.Depth > 0 {
		args = append(args, fmt.Sprintf("--depth=%d", cfg.Depth))
	}
	return gitCmd(args, cfg.DryRun)
}

func gitCmd(args []string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[DRY-RUN] git %s\n", strings.Join(args, " "))
		return nil
	}
	fmt.Printf("git %s\n", strings.Join(args, " "))
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// sanitizeLabel replaces path separators and spaces with underscores.
func sanitizeLabel(label string) string {
	r := strings.NewReplacer("/", "_", " ", "_", ":", "_")
	return r.Replace(label)
}

func loadRepos(path string) ([]Repo, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil // no namespaces.yaml yet — not an error
	}
	if err != nil {
		return nil, err
	}

	var nf NamespacesFile
	if err := yaml.Unmarshal(data, &nf); err != nil {
		return nil, err
	}

	var repos []Repo
	for _, ns := range nf.Namespaces {
		repos = append(repos, ns.Repos...)
	}
	return repos, nil
}
