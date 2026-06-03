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

// videoStudioScene is the video generation studio.
type videoStudioScene struct {
	mu       sync.Mutex
	onBack   func()
	backBtn  *ui.Button
	genBtn   *ui.Button
	prompt   string
	jobID    string
	progress *ui.ProgressBar
	busy     bool
}

func newVideoStudioScene(onBack func()) *videoStudioScene {
	s := &videoStudioScene{onBack: onBack}
	s.backBtn = ui.NewButton("← Back", onBack)
	s.backBtn.SetBounds(ui.Rect{X: 12, Y: 12, W: 90, H: 32})
	s.genBtn = ui.NewButton("Generate", func() { s.generate() })
	s.genBtn.SetBounds(ui.Rect{X: 1060, Y: 752, W: 200, H: 36})
	s.progress = ui.NewProgressBar()
	s.progress.SetBounds(ui.Rect{X: 16, Y: 740, W: 1020, H: 6})
	return s
}

func (s *videoStudioScene) generate() {
	text := strings.TrimSpace(s.prompt)
	s.mu.Lock()
	if text == "" || s.busy {
		s.mu.Unlock()
		return
	}
	s.busy = true
	s.progress.Value = 0.05
	s.mu.Unlock()

	go func() {
		id := s.submitJob(text)
		s.mu.Lock()
		s.jobID = id
		s.busy = false
		s.mu.Unlock()
	}()
}

func (s *videoStudioScene) submitJob(prompt string) string {
	payload := map[string]any{"prompt": prompt}
	body, _ := json.Marshal(payload)
	resp, err := http.Post("/v1/videos/generations", "application/json", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	return result.ID
}

func (s *videoStudioScene) Update(_ *SharedState) error {
	_ = s.backBtn.Update()
	_ = s.genBtn.Update()
	return nil
}

func (s *videoStudioScene) Draw(screen *ebiten.Image, _ *SharedState) {
	screen.Fill(color.RGBA{14, 14, 22, 255})
	vector.DrawFilledRect(screen, 0, 0, 1280, 52, color.RGBA{22, 22, 38, 255}, false)
	s.backBtn.Draw(screen)
	s.genBtn.Draw(screen)
	s.progress.Draw(screen)

	// Video preview area.
	vector.DrawFilledRect(screen, 16, 60, 840, 660, color.RGBA{20, 20, 32, 255}, false)
	// Controls panel.
	vector.DrawFilledRect(screen, 872, 60, 392, 660, color.RGBA{20, 20, 32, 255}, false)
	// Prompt input.
	vector.DrawFilledRect(screen, 16, 752, 1020, 36, color.RGBA{40, 40, 60, 255}, false)

	_ = strings.TrimSpace // keep import used in generate
}
