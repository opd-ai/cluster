// images.go adds /v1/images/generations and /v1/images/edits to the gateway.
//
// These endpoints translate OpenAI-compatible image requests to SwarmUI's
// HTTP API, then return base64-encoded or URL results.
//
// SwarmUI API reference: https://github.com/mcmonkeyprojects/SwarmUI
//
// The SwarmUI backend URL is configured via the -swarmui-url flag on the
// gateway command line (default: http://images.cluster:7801).
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// imageGenerationRequest is the OpenAI /v1/images/generations body.
type imageGenerationRequest struct {
	Prompt         string `json:"prompt"`
	Model          string `json:"model"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	ResponseFormat string `json:"response_format"` // "url" or "b64_json"
	Quality        string `json:"quality"`
	Style          string `json:"style"`
	User           string `json:"user"`
}

// imageEditRequest is the OpenAI /v1/images/edits body.
type imageEditRequest struct {
	Prompt         string `json:"prompt"`
	Image          string `json:"image"` // base64 PNG
	Mask           string `json:"mask"`  // base64 PNG mask
	N              int    `json:"n"`
	Size           string `json:"size"`
	ResponseFormat string `json:"response_format"`
}

// imageResponse is the OpenAI images response.
type imageResponse struct {
	Created int64           `json:"created"`
	Data    []imageDataItem `json:"data"`
}

// imageDataItem is one image in the response.
type imageDataItem struct {
	URL     string `json:"url,omitempty"`
	B64JSON string `json:"b64_json,omitempty"`
}

// handleImageGenerations proxies POST /v1/images/generations to SwarmUI.
func (gw *Gateway) handleImageGenerations(w http.ResponseWriter, r *http.Request) {
	if gw.swarmURL == "" {
		http.Error(w, `{"error":"image generation backend not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req imageGenerationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	req.N, req.Size = normalizeImageParams(req.N, req.Size)

	key := extractBearerToken(r)
	if !gw.checkNSFW(w, req.Prompt) {
		return
	}
	if !gw.checkImageQuota(w, key) {
		return
	}

	width, height := parseSizeStr(req.Size)

	swarmReq := map[string]any{
		"prompt":    req.Prompt,
		"width":     width,
		"height":    height,
		"images":    req.N,
		"donotsave": false,
		"model":     swarmModel(req.Model),
	}

	images, err := gw.callSwarm(r.Context(), "/API/GenerateText2Image", swarmReq)
	if err != nil {
		log.Printf("swarm generate: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	writeJSON(w, buildImageResponse(images, req.ResponseFormat, gw.swarmURL, gw.httpClient))
}

// handleImageEdits proxies POST /v1/images/edits to SwarmUI.
func (gw *Gateway) handleImageEdits(w http.ResponseWriter, r *http.Request) {
	if gw.swarmURL == "" {
		http.Error(w, `{"error":"image generation backend not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req imageEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	req.N, req.Size = normalizeImageParams(req.N, req.Size)

	key := extractBearerToken(r)
	if !gw.checkNSFW(w, req.Prompt) {
		return
	}
	if !gw.checkImageQuota(w, key) {
		return
	}

	width, height := parseSizeStr(req.Size)

	swarmReq := map[string]any{
		"prompt":     req.Prompt,
		"width":      width,
		"height":     height,
		"images":     req.N,
		"init_image": req.Image,
		"mask_image": req.Mask,
		"donotsave":  false,
	}

	images, err := gw.callSwarm(r.Context(), "/API/GenerateImage2Image", swarmReq)
	if err != nil {
		log.Printf("swarm edit: %v", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	writeJSON(w, buildImageResponse(images, req.ResponseFormat, gw.swarmURL, gw.httpClient))
}

// callSwarm sends a request to the SwarmUI API and returns image paths/URLs.
func (gw *Gateway) callSwarm(ctx context.Context, path string, body map[string]any) ([]string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gw.swarmURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := gw.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Images []string `json:"images"`
		Error  string   `json:"error"`
	}
	if err := json.Unmarshal(respData, &result); err != nil {
		return nil, fmt.Errorf("parse swarm response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("swarm error: %s", result.Error)
	}
	return result.Images, nil
}

// buildImageResponse converts SwarmUI image paths to an OpenAI-style response.
func buildImageResponse(images []string, format, baseURL string, client *http.Client) imageResponse {
	var items []imageDataItem
	for _, img := range images {
		if format == "b64_json" {
			// Fetch image and base64-encode it
			fullURL := img
			if len(img) > 0 && img[0] == '/' {
				fullURL = baseURL + img
			}
			resp, err := client.Get(fullURL)
			if err != nil {
				log.Printf("fetch image for b64_json: %v", err)
				continue // skip failed images
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				log.Printf("fetch image returned status %d", resp.StatusCode)
				continue
			}
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Printf("read image body: %v", err)
				continue
			}
			b64 := base64.StdEncoding.EncodeToString(data)
			items = append(items, imageDataItem{B64JSON: b64})
		} else {
			// SwarmUI returns relative paths; prepend the base URL.
			url := img
			if len(img) > 0 && img[0] == '/' {
				url = baseURL + img
			}
			items = append(items, imageDataItem{URL: url})
		}
	}
	return imageResponse{
		Created: time.Now().Unix(),
		Data:    items,
	}
}

// normalizeImageParams applies defaults to image request parameters.
func normalizeImageParams(n int, size string) (int, string) {
	// Cap maximum number of images per request
	if n <= 0 {
		n = 1
	} else if n > 10 {
		n = 10
	}
	
	// Allowlist valid image sizes
	allowedSizes := map[string]bool{
		"256x256":   true,
		"512x512":   true,
		"768x768":   true,
		"1024x1024": true,
	}
	
	if !allowedSizes[size] {
		size = "1024x1024" // default
	}
	return n, size
}

// parseSizeStr splits "1024x1024" into (1024, 1024).
func parseSizeStr(size string) (int, int) {
	var w, h int
	_, err := fmt.Sscanf(size, "%dx%d", &w, &h)
	if err != nil || w <= 0 || h <= 0 {
		return 1024, 1024
	}
	return w, h
}

// swarmModel maps an OpenAI model name to a SwarmUI model identifier.
func swarmModel(model string) string {
	switch model {
	case "dall-e-3", "flux", "flux-dev":
		return "flux1-dev"
	case "dall-e-2", "sdxl":
		return "sd_xl_base_1.0"
	case "flux-schnell":
		return "flux1-schnell"
	case "":
		return "" // SwarmUI uses its default
	default:
		return model
	}
}
