// cmd/rag-reindex watches repo-cache/ for changed repositories and
// re-ingests them into the Qdrant RAG collection nightly (or on demand).
//
// It reads namespaces.yaml to determine which repos have RAG configured
// (optional `rag.collection` field per namespace) and passes them to
// cmd/rag-ingest via exec.
//
// Change detection uses fsnotify; a debounce window coalesces rapid
// changes before triggering re-ingest.  A nightly cron ticker provides
// a full re-ingest sweep regardless of file events.
package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// -------------------------------------------------------------------------
// Config structures (subset of namespaces.yaml relevant to RAG)
// -------------------------------------------------------------------------

type ragBlock struct {
	Collection string   `yaml:"collection"`
	Include    []string `yaml:"include"`
	Exclude    []string `yaml:"exclude"`
}

type namespace struct {
	Name  string   `yaml:"name"`
	Repos []string `yaml:"repos"`
	RAG   *ragBlock `yaml:"rag,omitempty"`
}

type namespacesConfig struct {
	Namespaces []namespace `yaml:"namespaces"`
}

// -------------------------------------------------------------------------
// Main
// -------------------------------------------------------------------------

func main() {
	namespacesFile := flag.String("namespaces", "configs/namespaces.yaml", "Path to namespaces.yaml")
	cacheDir := flag.String("cache-dir", "repo-cache", "Path to repo-cache directory to watch")
	ragIngest := flag.String("rag-ingest", "rag-ingest", "Path or name of the rag-ingest binary")
	gatewayURL := flag.String("gateway-url", "http://localhost:8080", "Gateway base URL")
	qdrantAddr := flag.String("qdrant-addr", "localhost:6334", "Qdrant gRPC address")
	apiKey := flag.String("api-key", os.Getenv("GATEWAY_API_KEY"), "Gateway API key")
	debounce := flag.Duration("debounce", 10*time.Second, "Debounce window for file events")
	nightlyHour := flag.Int("nightly-hour", 2, "UTC hour for nightly full re-ingest (0-23)")
	flag.Parse()

	cfg, err := loadNamespaces(*namespacesFile)
	if err != nil {
		log.Fatalf("load namespaces: %v", err)
	}

	// Build map: repoDir → collection
	repoToCollection := buildRepoMap(cfg, *cacheDir)
	if len(repoToCollection) == 0 {
		log.Println("no namespaces have rag configuration; nothing to watch")
	}

	// fsnotify watcher on repo-cache
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("create watcher: %v", err)
	}
	defer watcher.Close()

	if err := addWatchRecursive(watcher, *cacheDir); err != nil {
		log.Printf("warn: could not watch %s: %v", *cacheDir, err)
	}

	// Debounce map: collection → timer
	pending := make(map[string]*time.Timer)

	// Nightly ticker
	nightly := time.NewTicker(1 * time.Minute)
	defer nightly.Stop()

	log.Printf("rag-reindex watching %s (debounce=%s, nightly=%02d:00 UTC)",
		*cacheDir, *debounce, *nightlyHour)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			coll := collectionForPath(event.Name, repoToCollection)
			if coll == "" {
				continue
			}
			// Debounce: reset timer on each event.
			if t, exists := pending[coll]; exists {
				t.Stop()
			}
			dir := dirForCollection(coll, repoToCollection)
			pending[coll] = time.AfterFunc(*debounce, func() {
				runIngest(*ragIngest, dir, coll, *gatewayURL, *qdrantAddr, *apiKey)
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)

		case t := <-nightly.C:
			if t.UTC().Hour() == *nightlyHour {
				log.Println("nightly full re-ingest triggered")
				for dir, coll := range repoToCollection {
					runIngest(*ragIngest, dir, coll, *gatewayURL, *qdrantAddr, *apiKey)
				}
			}
		}
	}
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

func loadNamespaces(path string) (*namespacesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg namespacesConfig
	return &cfg, yaml.Unmarshal(data, &cfg)
}

// buildRepoMap returns repoDir → collection for namespaces that have rag: configured.
func buildRepoMap(cfg *namespacesConfig, cacheDir string) map[string]string {
	m := make(map[string]string)
	for _, ns := range cfg.Namespaces {
		if ns.RAG == nil || ns.RAG.Collection == "" {
			continue
		}
		for _, repo := range ns.Repos {
			dir := filepath.Join(cacheDir, repoDir(repo))
			m[dir] = ns.RAG.Collection
		}
	}
	return m
}

// repoDir converts a git URL to a directory name (last path segment without .git).
func repoDir(repoURL string) string {
	base := filepath.Base(repoURL)
	return strings.TrimSuffix(base, ".git")
}

// collectionForPath returns the collection name for a changed file path.
func collectionForPath(path string, m map[string]string) string {
	for dir, coll := range m {
		if strings.HasPrefix(path, dir) {
			return coll
		}
	}
	return ""
}

// dirForCollection returns the first directory mapped to a collection.
func dirForCollection(coll string, m map[string]string) string {
	for dir, c := range m {
		if c == coll {
			return dir
		}
	}
	return ""
}

func addWatchRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return w.Add(path)
		}
		return nil
	})
}

func runIngest(binary, dir, collection, gatewayURL, qdrantAddr, apiKey string) {
	if dir == "" {
		log.Printf("ingest skip: empty dir for collection %s", collection)
		return
	}
	log.Printf("re-ingesting %s → collection %s", dir, collection)
	cmd := exec.Command(binary,
		"--dir", dir,
		"--collection", collection,
		"--gateway-url", gatewayURL,
		"--qdrant-addr", qdrantAddr,
		"--api-key", apiKey,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("rag-ingest %s: %v", collection, err)
	}
}
