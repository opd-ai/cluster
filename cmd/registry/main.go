// cmd/registry maintains registry/models.json: a catalog of LLM bases,
// LoRA adapters, GGUFs, and image/video checkpoints (SDXL, Flux, video
// models, VAEs, CLIP/T5 encoders, LoRAs).
//
// Each entry records SHA256, size, license tag, and source URL so the
// cluster can verify downloads and track provenance.
//
// Subcommands:
//
//	list                  print all registry entries (table or JSON)
//	push  -name -path     add/update an entry from a local file
//	pull  -name -dest     download an entry to a local path
//	verify -name          verify the SHA256 of a cached file
//
// Usage:
//
//	registry [flags] <subcommand> [subcommand-flags]
//
// Flags:
//
//	-registry   path to models.json (default: registry/models.json)
//	-json       output as JSON (list only)
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"
)

// Entry describes one artifact in the registry.
type Entry struct {
	Name       string    `json:"name"`
	Type       string    `json:"type"`        // llm-base|adapter|gguf|checkpoint|vae|clip|lora|embedding
	SHA256     string    `json:"sha256"`
	SizeBytes  int64     `json:"size_bytes"`
	LicenseTag string    `json:"license_tag"` // e.g. apache-2.0, mit, cc-by-nc-4.0
	SourceURL  string    `json:"source_url"`
	AddedAt    time.Time `json:"added_at"`
	Tags       []string  `json:"tags,omitempty"`
}

// Registry is the top-level JSON structure.
type Registry struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

func main() {
	registryPath := flag.String("registry", "registry/models.json", "Path to models.json")
	jsonOut := flag.Bool("json", false, "Output as JSON (list only)")
	flag.Parse()

	if flag.NArg() == 0 {
		log.Fatal("usage: registry [-registry path] <list|push|pull|verify> [flags]")
	}

	subcommand := flag.Arg(0)

	switch subcommand {
	case "list":
		runList(*registryPath, *jsonOut)
	case "push":
		runPush(*registryPath, flag.Args()[1:])
	case "pull":
		runPull(*registryPath, flag.Args()[1:])
	case "verify":
		runVerify(*registryPath, flag.Args()[1:])
	default:
		log.Fatalf("unknown subcommand %q (want: list, push, pull, verify)", subcommand)
	}
}

// -------------------------------------------------------------------------
// list
// -------------------------------------------------------------------------

func runList(registryPath string, jsonOut bool) {
	reg := loadRegistry(registryPath)

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reg.Entries); err != nil {
			log.Fatalf("encode: %v", err)
		}
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tSIZE\tLICENSE\tSHA256 (first 12)")
	for _, e := range reg.Entries {
		sha := e.SHA256
		if len(sha) > 12 {
			sha = sha[:12] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			e.Name, e.Type, humanBytes(e.SizeBytes), e.LicenseTag, sha)
	}
	w.Flush()
}

// -------------------------------------------------------------------------
// push
// -------------------------------------------------------------------------

func runPush(registryPath string, args []string) {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	name := fs.String("name", "", "Registry entry name (required)")
	path := fs.String("path", "", "Local file path (required)")
	entryType := fs.String("type", "gguf", "Entry type")
	license := fs.String("license", "unknown", "License tag")
	sourceURL := fs.String("source", "", "Source URL")
	_ = fs.Parse(args)

	if *name == "" || *path == "" {
		log.Fatal("push: -name and -path are required")
	}

	sha, size, err := hashFile(*path)
	if err != nil {
		log.Fatalf("hash %s: %v", *path, err)
	}

	reg := loadRegistry(registryPath)
	reg.updateEntry(Entry{
		Name:       *name,
		Type:       *entryType,
		SHA256:     sha,
		SizeBytes:  size,
		LicenseTag: *license,
		SourceURL:  *sourceURL,
		AddedAt:    time.Now().UTC(),
	})

	if err := saveRegistry(registryPath, reg); err != nil {
		log.Fatalf("save registry: %v", err)
	}
	fmt.Printf("registered %s (sha256:%s)\n", *name, sha[:16])
}

// -------------------------------------------------------------------------
// pull
// -------------------------------------------------------------------------

func runPull(registryPath string, args []string) {
	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	name := fs.String("name", "", "Registry entry name (required)")
	dest := fs.String("dest", ".", "Destination directory or file path")
	_ = fs.Parse(args)

	if *name == "" {
		log.Fatal("pull: -name is required")
	}

	reg := loadRegistry(registryPath)
	entry, ok := reg.find(*name)
	if !ok {
		log.Fatalf("entry %q not found in registry", *name)
	}
	if entry.SourceURL == "" {
		log.Fatalf("entry %q has no source URL", *name)
	}

	dst := *dest
	info, err := os.Stat(dst)
	if err == nil && info.IsDir() {
		dst = filepath.Join(dst, filepath.Base(entry.SourceURL))
	}

	fmt.Printf("pulling %s → %s\n", entry.SourceURL, dst)
	if err := downloadFile(entry.SourceURL, dst); err != nil {
		log.Fatalf("download: %v", err)
	}

	sha, _, err := hashFile(dst)
	if err != nil {
		log.Fatalf("hash after download: %v", err)
	}
	if sha != entry.SHA256 {
		log.Fatalf("SHA256 mismatch: got %s want %s", sha, entry.SHA256)
	}
	fmt.Printf("✓ verified sha256:%s\n", sha[:16])
}

// -------------------------------------------------------------------------
// verify
// -------------------------------------------------------------------------

func runVerify(registryPath string, args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	name := fs.String("name", "", "Registry entry name (required)")
	path := fs.String("path", "", "Local file to verify (required)")
	_ = fs.Parse(args)

	if *name == "" || *path == "" {
		log.Fatal("verify: -name and -path are required")
	}

	reg := loadRegistry(registryPath)
	entry, ok := reg.find(*name)
	if !ok {
		log.Fatalf("entry %q not found in registry", *name)
	}

	sha, _, err := hashFile(*path)
	if err != nil {
		log.Fatalf("hash %s: %v", *path, err)
	}

	if sha != entry.SHA256 {
		log.Fatalf("FAIL: sha256 mismatch for %s\n  got  %s\n  want %s", *name, sha, entry.SHA256)
	}
	fmt.Printf("✓ %s OK (sha256:%s)\n", *name, sha[:16])
}

// -------------------------------------------------------------------------
// Registry helpers
// -------------------------------------------------------------------------

func loadRegistry(path string) Registry {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Registry{Version: 1}
	}
	if err != nil {
		log.Fatalf("read registry: %v", err)
	}
	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		log.Fatalf("parse registry: %v", err)
	}
	return reg
}

func saveRegistry(path string, reg Registry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (r *Registry) find(name string) (Entry, bool) {
	for _, e := range r.Entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

func (r *Registry) updateEntry(e Entry) {
	for i, existing := range r.Entries {
		if existing.Name == e.Name {
			r.Entries[i] = e
			return
		}
	}
	r.Entries = append(r.Entries, e)
}

// -------------------------------------------------------------------------
// File helpers
// -------------------------------------------------------------------------

func hashFile(path string) (sha string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), n, nil
}

func downloadFile(url, dest string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{
		Transport: &http.Transport{
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
