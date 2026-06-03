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

type repo struct {
	Label  string `yaml:"label"`
	URL    string `yaml:"url"`
	Branch string `yaml:"branch"`
}

type namespace struct {
	Name  string    `yaml:"name"`
	Repos []repo    `yaml:"repos"`
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

	// Nightly ticker: track the last day we ran to avoid firing multiple
	// times within the same hour (H7).
	nightly := time.NewTicker(1 * time.Minute)
	defer nightly.Stop()
	var lastNightlyDay int // Julian day number of last nightly run

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
			dir, coll := dirAndCollectionForPath(event.Name, repoToCollection)
			if coll == "" {
				continue
			}
			// Debounce: reset timer on each event.
			if t, exists := pending[coll]; exists {
				t.Stop()
			}
			// Copy dir to avoid closure capture issues if dir is reassigned.
			dirCopy := dir
			pending[coll] = time.AfterFunc(*debounce, func() {
				runIngest(*ragIngest, dirCopy, coll, *gatewayURL, *qdrantAddr, *apiKey)
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)

		case t := <-nightly.C:
			utc := t.UTC()
			today := int(utc.Unix() / 86400)
			if utc.Hour() == *nightlyHour && today != lastNightlyDay {
				lastNightlyDay = today
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
		for _, r := range ns.Repos {
			dir := filepath.Join(cacheDir, sanitizeLabel(r.Label))
			m[dir] = ns.RAG.Collection
		}
	}
	return m
}

// sanitizeLabel replaces path separators and spaces with underscores,
// matching the convention used by cmd/repo-sync.
func sanitizeLabel(label string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_", ":", "_")
	return replacer.Replace(label)
}

// dirAndCollectionForPath returns the repo directory and collection name for
// a changed file path, avoiding the round-trip through dirForCollection that
// could return a different directory than the one that actually changed.
func dirAndCollectionForPath(path string, m map[string]string) (string, string) {
	for dir, coll := range m {
		// Check for exact match or directory boundary (dir + "/")
		if path == dir || strings.HasPrefix(path, dir+"/") {
			return dir, coll
		}
	}
	return "", ""
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
	)
	// Pass the API key via the environment rather than a CLI argument to avoid
	// exposure in process listings readable by other local users (M13).
	cmd.Env = append(os.Environ(), "GATEWAY_API_KEY="+apiKey)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("rag-ingest %s: %v", collection, err)
	}
}
