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

// imageStudioScene is the image generation studio.
type imageStudioScene struct {
	mu       sync.Mutex
	onBack   func()
	backBtn  *ui.Button
	genBtn   *ui.Button
	prompt   string
	lastURL  string
	progress *ui.ProgressBar
	busy     bool
}

func newImageStudioScene(onBack func()) *imageStudioScene {
	s := &imageStudioScene{onBack: onBack}
	s.backBtn = ui.NewButton("← Back", onBack)
	s.backBtn.SetBounds(ui.Rect{X: 12, Y: 12, W: 90, H: 32})
	s.genBtn = ui.NewButton("Generate", func() { s.generate() })
	s.genBtn.SetBounds(ui.Rect{X: 1060, Y: 752, W: 200, H: 36})
	s.progress = ui.NewProgressBar()
	s.progress.SetBounds(ui.Rect{X: 16, Y: 740, W: 1020, H: 6})
	return s
}

func (s *imageStudioScene) generate() {
	text := strings.TrimSpace(s.prompt)
	if text == "" {
		return
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return
	}
	s.busy = true
	s.progress.Value = 0.1
	s.mu.Unlock()

	go func() {
		url := s.callGenerate(text)
		s.mu.Lock()
		s.lastURL = url
		s.progress.Value = 1.0
		s.busy = false
		s.mu.Unlock()
	}()
}

func (s *imageStudioScene) callGenerate(prompt string) string {
	payload := map[string]any{"prompt": prompt, "n": 1, "size": "1024x1024"}
	body, _ := json.Marshal(payload)
	resp, err := http.Post("/v1/images/generations", "application/json", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var result struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Data) == 0 {
		return ""
	}
	return result.Data[0].URL
}

func (s *imageStudioScene) Update(_ *SharedState) error {
	_ = s.backBtn.Update()
	_ = s.genBtn.Update()
	return nil
}

func (s *imageStudioScene) Draw(screen *ebiten.Image, _ *SharedState) {
	screen.Fill(color.RGBA{14, 14, 22, 255})
	vector.DrawFilledRect(screen, 0, 0, 1280, 52, color.RGBA{22, 22, 38, 255}, false)
	s.backBtn.Draw(screen)
	s.genBtn.Draw(screen)

	s.mu.Lock()
	s.progress.Draw(screen)
	s.mu.Unlock()

	// Image preview area.
	vector.DrawFilledRect(screen, 16, 60, 740, 660, color.RGBA{20, 20, 32, 255}, false)
	// Prompt / controls area.
	vector.DrawFilledRect(screen, 772, 60, 492, 660, color.RGBA{20, 20, 32, 255}, false)
	// Prompt input field.
	vector.DrawFilledRect(screen, 16, 752, 1020, 36, color.RGBA{40, 40, 60, 255}, false)
}
