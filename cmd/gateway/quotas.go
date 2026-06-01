// quotas.go implements per-key daily image/video budget and NSFW filtering
// for the gateway.
//
// Quota state is in-memory and resets at midnight UTC.  For production use,
// back this with Redis or a lightweight SQLite file by replacing the counters
// map with the appropriate client.
//
// NSFW filtering is off by default (self-hosted) and can be enabled per-key
// via the key manifest or globally via --nsfw-filter.  When enabled, the
// gateway rejects requests whose prompts match a blocklist.
package main

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// quotaState holds usage counters per API key for the current UTC day.
type quotaState struct {
	mu       sync.Mutex
	day      int // unix day number (unix / 86400)
	images   map[string]int
	videos   map[string]int
}

var globalQuota = &quotaState{
	images: make(map[string]int),
	videos: make(map[string]int),
}

// quotaConfig holds per-gateway quota limits.
type quotaConfig struct {
	MaxImagesPerKeyPerDay int
	MaxVideosPerKeyPerDay int
	NSFWFilter            bool
	// NSFWBlocklist is a set of lower-cased substrings to block.
	NSFWBlocklist map[string]struct{}
}

// defaultNSFWBlocklist contains placeholder entries.  In production, replace
// with a curated list appropriate for the deployment context.
var defaultNSFWBlocklist = map[string]struct{}{
	// Add blocked terms here.
}

// checkImageQuota returns an HTTP error if the key has exceeded its daily limit.
func (gw *Gateway) checkImageQuota(w http.ResponseWriter, key string) bool {
	if gw.quotaCfg == nil || gw.quotaCfg.MaxImagesPerKeyPerDay <= 0 {
		return true // no limit
	}
	if globalQuota.increment("image", key, gw.quotaCfg.MaxImagesPerKeyPerDay) {
		return true
	}
	http.Error(w, `{"error":"daily image quota exceeded"}`, http.StatusTooManyRequests)
	return false
}

// checkVideoQuota returns an HTTP error if the key has exceeded its daily limit.
func (gw *Gateway) checkVideoQuota(w http.ResponseWriter, key string) bool {
	if gw.quotaCfg == nil || gw.quotaCfg.MaxVideosPerKeyPerDay <= 0 {
		return true // no limit
	}
	if globalQuota.increment("video", key, gw.quotaCfg.MaxVideosPerKeyPerDay) {
		return true
	}
	http.Error(w, `{"error":"daily video quota exceeded"}`, http.StatusTooManyRequests)
	return false
}

// checkNSFW returns false (blocked) if the prompt contains a blocklisted term.
func (gw *Gateway) checkNSFW(w http.ResponseWriter, prompt string) bool {
	if gw.quotaCfg == nil || !gw.quotaCfg.NSFWFilter {
		return true // filter disabled
	}
	lower := strings.ToLower(prompt)
	for term := range gw.quotaCfg.NSFWBlocklist {
		if strings.Contains(lower, term) {
			http.Error(w, `{"error":"prompt blocked by content filter"}`, http.StatusBadRequest)
			return false
		}
	}
	return true
}

// increment increments the counter for (mediaType, key) and returns true
// if the new count is ≤ limit.  Counters reset daily.
func (qs *quotaState) increment(mediaType, key string, limit int) bool {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	today := int(time.Now().UTC().Unix() / 86400)
	if today != qs.day {
		qs.day = today
		qs.images = make(map[string]int)
		qs.videos = make(map[string]int)
	}

	var counter map[string]int
	switch mediaType {
	case "video":
		counter = qs.videos
	default:
		counter = qs.images
	}

	counter[key]++
	return counter[key] <= limit
}
