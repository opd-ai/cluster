// cmd/cache-gc performs LRU eviction on the per-node hot cache at
// /var/lib/aicluster/cache (or an overridden path).
//
// It walks the cache directory, collects file access times, and removes the
// least-recently-used files until the directory is below the configured
// high-water mark (default: 85 % of the partition's total capacity).
//
// Usage:
//
//	cache-gc [flags]
//
// Flags:
//
//	-cache-dir      root of the hot cache (default: /var/lib/aicluster/cache)
//	-high-water     evict when usage exceeds this percentage (default: 85)
//	-low-water      stop evicting when usage drops below this percentage (default: 70)
//	-dry-run        log what would be removed without deleting anything
//	-min-age        minimum file age in minutes before eligible for eviction (default: 60)
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/sys/unix"
)

// CacheEntry records an eviction candidate.
type CacheEntry struct {
	Path    string
	Size    int64
	LastUse time.Time
}

// GCConfig holds runtime configuration.
type GCConfig struct {
	CacheDir   string
	HighWater  int
	LowWater   int
	MinAgeMins int
	DryRun     bool
}

func main() {
	cfg := GCConfig{}
	flag.StringVar(&cfg.CacheDir, "cache-dir", "/var/lib/aicluster/cache", "Hot cache root directory")
	flag.IntVar(&cfg.HighWater, "high-water", 85, "Evict when usage % exceeds this value")
	flag.IntVar(&cfg.LowWater, "low-water", 70, "Stop evicting when usage % drops below this value")
	flag.IntVar(&cfg.MinAgeMins, "min-age", 60, "Minimum file age (minutes) before eviction eligibility")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Log removals without deleting")
	flag.Parse()

	if cfg.HighWater <= cfg.LowWater {
		log.Fatalf("-high-water (%d) must be greater than -low-water (%d)", cfg.HighWater, cfg.LowWater)
	}

	if err := runGC(cfg); err != nil {
		log.Fatalf("cache-gc: %v", err)
	}
}

func runGC(cfg GCConfig) error {
	usagePct, err := diskUsagePct(cfg.CacheDir)
	if err != nil {
		return fmt.Errorf("disk usage: %w", err)
	}

	if usagePct < cfg.HighWater {
		fmt.Printf("cache usage %d%% — below high-water mark (%d%%), nothing to do\n",
			usagePct, cfg.HighWater)
		return nil
	}

	fmt.Printf("cache usage %d%% — evicting to reach %d%%\n", usagePct, cfg.LowWater)

	entries, err := collectEntries(cfg.CacheDir, cfg.MinAgeMins)
	if err != nil {
		return fmt.Errorf("collect entries: %w", err)
	}

	// Sort ascending by last-use time (oldest first).
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].LastUse.Before(entries[j].LastUse)
	})

	for _, entry := range entries {
		usagePct, err = diskUsagePct(cfg.CacheDir)
		if err != nil {
			return fmt.Errorf("disk usage mid-gc: %w", err)
		}
		if usagePct < cfg.LowWater {
			break
		}

		if cfg.DryRun {
			fmt.Printf("[DRY-RUN] would remove %s (%.1f MB, last used %s)\n",
				entry.Path, float64(entry.Size)/(1<<20), entry.LastUse.Format(time.RFC3339))
			continue
		}

		fmt.Printf("removing %s (%.1f MB, last used %s)\n",
			entry.Path, float64(entry.Size)/(1<<20), entry.LastUse.Format(time.RFC3339))
		if err := os.Remove(entry.Path); err != nil {
			log.Printf("remove %s: %v", entry.Path, err)
		}
	}

	return nil
}

// collectEntries walks cacheDir and returns regular files whose last-access
// time is older than minAgeMins.
func collectEntries(cacheDir string, minAgeMins int) ([]CacheEntry, error) {
	cutoff := time.Now().Add(-time.Duration(minAgeMins) * time.Minute)
	var entries []CacheEntry

	err := filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		atime := accessTime(info)
		if atime.After(cutoff) {
			return nil // too recently accessed
		}

		entries = append(entries, CacheEntry{
			Path:    path,
			Size:    info.Size(),
			LastUse: atime,
		})
		return nil
	})

	return entries, err
}

// diskUsagePct returns the used-disk percentage for the filesystem containing
// path.
func diskUsagePct(path string) (int, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	if stat.Blocks == 0 {
		return 0, nil
	}
	used := stat.Blocks - stat.Bfree
	return int(used * 100 / stat.Blocks), nil
}
