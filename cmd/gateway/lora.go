// lora.go implements LoRA adapter hot-swap support for cmd/gateway.
//
// An adapter manifest file (JSON) is watched for changes. When new adapters
// appear the gateway registers them as available model aliases of the form
// "base+adapter". Requests for "base+adapter" are routed to the vLLM/llama.cpp
// backend that has both the base model and the adapter loaded.
//
// Manifest format (configs/lora-adapters.json):
//
//	{
//	  "adapters": [
//	    {
//	      "name":   "llama3-legal",
//	      "base":   "llama3",
//	      "path":   "/var/lib/aicluster/adapters/llama3-legal.gguf",
//	      "nodes":  ["gpu-01", "gpu-02"]
//	    }
//	  ]
//	}
package main

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

// LoRAAdapter describes a single LoRA adapter.
type LoRAAdapter struct {
	Name  string   `json:"name"`
	Base  string   `json:"base"`
	Path  string   `json:"path"`
	Nodes []string `json:"nodes"`
}

// LoRAManifest is the top-level manifest file.
type LoRAManifest struct {
	Adapters []LoRAAdapter `json:"adapters"`
}

// startLoRAWatcher polls the manifest file for changes and updates the
// gateway's adapter registry.  It runs until the process exits.
func (gw *Gateway) startLoRAWatcher(manifestPath string, pollInterval time.Duration) {
	var lastMod time.Time
	for {
		info, err := os.Stat(manifestPath)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		if info.ModTime().After(lastMod) {
			lastMod = info.ModTime()
			if err := gw.reloadAdapters(manifestPath); err != nil {
				log.Printf("lora watcher: reload error: %v", err)
			}
		}
		time.Sleep(pollInterval)
	}
}

// reloadAdapters reads the manifest and registers adapters as model aliases.
func (gw *Gateway) reloadAdapters(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var manifest LoRAManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()

	for _, adapter := range manifest.Adapters {
		alias := adapter.Base + "+" + adapter.Name
		gw.loraAdapters[alias] = adapter
		log.Printf("lora watcher: registered adapter %q (base=%s, nodes=%v)",
			alias, adapter.Base, adapter.Nodes)
	}
	return nil
}

// resolveLoRAModel checks if model is a "base+adapter" alias and returns the
// backend URL that serves it. Returns "" if not a LoRA alias.
func (gw *Gateway) resolveLoRAModel(model string) string {
	gw.mu.Lock()
	adapter, ok := gw.loraAdapters[model]
	gw.mu.Unlock()
	if !ok {
		return ""
	}

	// Find a backend that has the base model and is in the adapter's node list.
	for _, nodeName := range adapter.Nodes {
		for _, b := range gw.backends {
			if b.URL == "" {
				continue
			}
			b.mu.RLock()
			healthy := b.Healthy
			b.mu.RUnlock()
			if healthy && backendMatchesNode(b.URL, nodeName) {
				return b.URL
			}
		}
	}
	return ""
}

// backendMatchesNode returns true if the backend URL contains the node name or
// address.  Simple substring match is sufficient for inventory-driven setups.
func backendMatchesNode(backendURL, nodeName string) bool {
	return len(nodeName) > 0 && len(backendURL) > 0 &&
		(contains(backendURL, nodeName))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(substr) == 0 ||
		indexStr(s, substr) >= 0)
}

func indexStr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
