//go:build js && wasm

package main

import (
	"bytes"
	"encoding/json"
	"image/color"
	"net/http"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/opd-ai/cluster/internal/ui"
)

// chatMessage is a single message in the chat playground.
type chatMessage struct {
	Role    string
	Content string
}

// chatScene is the chat playground for testing LLM completions.
type chatScene struct {
	mu       sync.Mutex
	onBack   func()
	backBtn  *ui.Button
	sendBtn  *ui.Button
	messages []chatMessage
	input    string
	busy     bool
}

func newChatScene(onBack func()) *chatScene {
	s := &chatScene{onBack: onBack}
	s.backBtn = ui.NewButton("← Back", onBack)
	s.backBtn.SetBounds(ui.Rect{X: 12, Y: 12, W: 90, H: 32})
	s.sendBtn = ui.NewButton("Send", func() { s.sendMessage() })
	s.sendBtn.SetBounds(ui.Rect{X: 1140, Y: 754, W: 120, H: 36})
	return s
}

func (s *chatScene) sendMessage() {
	text := strings.TrimSpace(s.input)
	if text == "" {
		return
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return
	}
	s.messages = append(s.messages, chatMessage{Role: "user", Content: text})
	s.input = ""
	s.busy = true
	s.mu.Unlock()

	go func() {
		reply := s.callChat(text)
		s.mu.Lock()
		s.messages = append(s.messages, chatMessage{Role: "assistant", Content: reply})
		s.busy = false
		s.mu.Unlock()
	}()
}

func (s *chatScene) callChat(userMsg string) string {
	payload := map[string]any{
		"model": "default",
		"messages": []map[string]string{
			{"role": "user", "content": userMsg},
		},
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post("/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		return "error: " + err.Error()
	}
	defer resp.Body.Close()
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "decode error"
	}
	if len(result.Choices) > 0 {
		return result.Choices[0].Message.Content
	}
	return "(no response)"
}

func (s *chatScene) Update(_ *SharedState) error {
	_ = s.backBtn.Update()
	_ = s.sendBtn.Update()
	return nil
}

func (s *chatScene) Draw(screen *ebiten.Image, _ *SharedState) {
	screen.Fill(color.RGBA{14, 14, 22, 255})
	vector.DrawFilledRect(screen, 0, 0, 1280, 52, color.RGBA{22, 22, 38, 255}, false)
	s.backBtn.Draw(screen)
	s.sendBtn.Draw(screen)

	// Message area background.
	vector.DrawFilledRect(screen, 16, 60, 1248, 680, color.RGBA{20, 20, 32, 255}, false)

	// Input field.
	vector.DrawFilledRect(screen, 16, 752, 1108, 38, color.RGBA{40, 40, 60, 255}, false)
}
