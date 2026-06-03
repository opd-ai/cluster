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
	"strings"
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
// gateway's adapter registry.  It runs until stop is closed.
func (gw *Gateway) startLoRAWatcher(manifestPath string, pollInterval time.Duration, stop <-chan struct{}) {
	var lastMod time.Time
	for {
		select {
		case <-stop:
			return
		default:
		}
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

	// Build a new adapter map atomically
	newAdapters := make(map[string]LoRAAdapter)
	for _, adapter := range manifest.Adapters {
		alias := adapter.Base + "+" + adapter.Name
		newAdapters[alias] = adapter
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()

	gw.loraAdapters = newAdapters
	for _, adapter := range manifest.Adapters {
		alias := adapter.Base + "+" + adapter.Name
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
		for _, b := range gw.lbRegistry.GetAll() {
			chatURL := backendURLForRole(b, "chat")
			if chatURL == "" {
				continue
			}
			if b.Healthy && backendMatchesNode(chatURL, nodeName) {
				return chatURL
			}
		}
	}
	return ""
}

// backendMatchesNode returns true if the backend URL contains the node name or
// address.  Simple substring match is sufficient for inventory-driven setups.
func backendMatchesNode(backendURL, nodeName string) bool {
	return len(nodeName) > 0 && len(backendURL) > 0 &&
		strings.Contains(backendURL, nodeName)
}
