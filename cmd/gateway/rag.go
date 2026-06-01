// rag.go adds server-side RAG tool execution to the gateway.
//
// When a /v1/chat/completions request contains a tool of type "rag",
// the gateway calls the RAG service to retrieve context and prepends
// a system message with citations before forwarding to the LLM backend.
//
// Example request:
//
//	{
//	  "model": "llama3",
//	  "messages": [{"role":"user","content":"What is the k3s control plane?"}],
//	  "tools": [{"type":"rag","collection":"cluster-docs"}]
//	}
//
// The tool is removed from the forwarded request; clients only see
// standard OpenAI-compatible chat completion responses.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// ragTool is the "rag" entry in a chat completion tools array.
type ragTool struct {
	Type       string `json:"type"`
	Collection string `json:"collection"`
	TopK       int    `json:"top_k"`
}

// ragHit is one retrieval result returned by the RAG service.
type ragHit struct {
	Text  string  `json:"text"`
	File  string  `json:"file"`
	Chunk int64   `json:"chunk"`
	Score float64 `json:"score"`
}

// ragQueryResponse mirrors cmd/rag's /rag/query response.
type ragQueryResponse struct {
	Results []ragHit `json:"results"`
}

// extractRAGTools removes any {type:"rag"} entries from the tools array and
// returns them separately.
func extractRAGTools(tools []json.RawMessage) (ragTools []ragTool, rest []json.RawMessage) {
	for _, t := range tools {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(t, &probe); err != nil {
			rest = append(rest, t)
			continue
		}
		if probe.Type == "rag" {
			var rt ragTool
			if err := json.Unmarshal(t, &rt); err == nil {
				ragTools = append(ragTools, rt)
				continue
			}
		}
		rest = append(rest, t)
	}
	return
}

// retrieveRAGContext calls the RAG service and returns a formatted context
// string with citations.  ragURL is the base URL of cmd/rag (e.g.
// http://rag:8081).
func retrieveRAGContext(ctx context.Context, ragURL, query, collection string, topK int, authHeader string) (string, error) {
	if topK <= 0 {
		topK = 5
	}
	body := map[string]any{
		"query":      query,
		"collection": collection,
		"top_k":      topK,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		ragURL+"/rag/query", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var qr ragQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return "", fmt.Errorf("decode rag response: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("The following context was retrieved from the knowledge base:\n\n")
	for i, h := range qr.Results {
		fmt.Fprintf(&sb, "[%d] (file: %s, score: %.3f)\n%s\n\n", i+1, h.File, h.Score, h.Text)
	}
	return sb.String(), nil
}

// injectRAGContextRaw is the gateway-facing variant of injectRAGContext
// that operates directly on the map[string]json.RawMessage request body.
func (gw *Gateway) injectRAGContextRaw(ctx context.Context, req map[string]json.RawMessage, authHeader string) error {
	toolsRaw, ok := req["tools"]
	if !ok {
		return nil
	}

	var tools []json.RawMessage
	if err := json.Unmarshal(toolsRaw, &tools); err != nil {
		return nil
	}

	ragTools, rest := extractRAGTools(tools)
	if len(ragTools) == 0 {
		return nil
	}

	// Extract user query
	var messages []map[string]json.RawMessage
	if msgsRaw, ok := req["messages"]; ok {
		_ = json.Unmarshal(msgsRaw, &messages)
	}
	query := extractLastUserMessageRaw(messages)
	if query == "" {
		return nil
	}

	var contextParts []string
	for _, rt := range ragTools {
		ctxStr, err := retrieveRAGContext(ctx, gw.ragURL, query, rt.Collection, rt.TopK, authHeader)
		if err != nil {
			log.Printf("RAG retrieve %s: %v", rt.Collection, err)
			continue
		}
		contextParts = append(contextParts, ctxStr)
	}

	if len(contextParts) == 0 {
		return nil
	}

	// Prepend system message.
	systemContent := strings.Join(contextParts, "\n---\n")
	systemMsg := map[string]any{
		"role":    "system",
		"content": systemContent,
	}
	sysRaw, err := json.Marshal(systemMsg)
	if err != nil {
		return err
	}
	var sysRawMsg map[string]json.RawMessage
	if err := json.Unmarshal(sysRaw, &sysRawMsg); err != nil {
		return err
	}

	// Re-encode messages with system message prepended.
	newMessages := append([]map[string]json.RawMessage{sysRawMsg}, messages...)
	newMsgsJSON, err := json.Marshal(newMessages)
	if err != nil {
		return err
	}
	req["messages"] = newMsgsJSON

	// Update tools.
	if len(rest) == 0 {
		delete(req, "tools")
	} else {
		restJSON, err := json.Marshal(rest)
		if err == nil {
			req["tools"] = restJSON
		}
	}
	return nil
}

// extractLastUserMessageRaw extracts the last user message content from
// a raw JSON messages array.
func extractLastUserMessageRaw(messages []map[string]json.RawMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		var role, content string
		if r, ok := messages[i]["role"]; ok {
			_ = json.Unmarshal(r, &role)
		}
		if c, ok := messages[i]["content"]; ok {
			_ = json.Unmarshal(c, &content)
		}
		if role == "user" && content != "" {
			return content
		}
	}
	return ""
}
//  1. Extracts "rag" tools from the tools array.
//  2. Calls the RAG service for each unique collection.
//  3. Prepends a system message with the retrieved context.
//  4. Removes "rag" tools from the forwarded request.
func (gw *Gateway) injectRAGContext(ctx context.Context, req map[string]any, authHeader string) error {
	if gw.ragURL == "" {
		return nil
	}

	rawTools, _ := req["tools"].([]any)
	if len(rawTools) == 0 {
		return nil
	}

	// Re-marshal tools to []json.RawMessage for extraction.
	var toolsJSON []json.RawMessage
	for _, t := range rawTools {
		b, err := json.Marshal(t)
		if err != nil {
			continue
		}
		toolsJSON = append(toolsJSON, b)
	}

	ragTools, rest := extractRAGTools(toolsJSON)
	if len(ragTools) == 0 {
		return nil
	}

	// Extract the user query from the last user message.
	query := extractLastUserMessage(req)
	if query == "" {
		return nil
	}

	// Retrieve context for each RAG tool.
	var contextParts []string
	for _, rt := range ragTools {
		ctxStr, err := retrieveRAGContext(ctx, gw.ragURL, query, rt.Collection, rt.TopK, authHeader)
		if err != nil {
			log.Printf("RAG retrieve %s: %v", rt.Collection, err)
			continue
		}
		contextParts = append(contextParts, ctxStr)
	}

	if len(contextParts) == 0 {
		return nil
	}

	// Prepend system message.
	systemMsg := map[string]any{
		"role":    "system",
		"content": strings.Join(contextParts, "\n---\n"),
	}
	messages, _ := req["messages"].([]any)
	req["messages"] = append([]any{systemMsg}, messages...)

	// Replace tools with non-RAG tools (or remove key if empty).
	if len(rest) == 0 {
		delete(req, "tools")
	} else {
		req["tools"] = rest
	}
	return nil
}

// extractLastUserMessage returns the content of the last "user" message.
func extractLastUserMessage(req map[string]any) string {
	messages, _ := req["messages"].([]any)
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role == "user" {
			if content, _ := msg["content"].(string); content != "" {
				return content
			}
		}
	}
	return ""
}
