package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/opd-ai/cluster/internal/ui"
)

// modelEntry represents one model/adapter in the registry.
type modelEntry struct {
	Name    string   `json:"name"`
	Tag     string   `json:"tag"`
	SHA     string   `json:"sha256"`
	SizeMB  int64    `json:"size_mb"`
	License string   `json:"license"`
	Nodes   []string `json:"nodes"`
}

// registryScene lets operators browse the model registry.
type registryScene struct {
	mu         sync.Mutex
	onBack     func()
	backBtn    *ui.Button
	refreshBtn *ui.Button
	models     []modelEntry
	selected   int
	busy       bool
	statusMsg  string
}

func newRegistryScene(onBack func()) *registryScene {
	s := &registryScene{
		onBack:   onBack,
		selected: -1,
	}
	s.backBtn = ui.NewButton("← Back", onBack)
	s.backBtn.SetBounds(ui.Rect{X: 12, Y: 12, W: 90, H: 32})
	s.refreshBtn = ui.NewButton("Refresh", func() { s.fetchModels() })
	s.refreshBtn.SetBounds(ui.Rect{X: 120, Y: 12, W: 100, H: 32})
	return s
}

func (s *registryScene) fetchModels() {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return
	}
	s.busy = true
	s.mu.Unlock()

	go func() {
		resp, err := http.Get("/v1/models")
		if err != nil {
			s.mu.Lock()
			s.statusMsg = "fetch error: " + err.Error()
			s.busy = false
			s.mu.Unlock()
			return
		}
		defer resp.Body.Close()
		var result struct {
			Data []modelEntry `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			s.mu.Lock()
			s.statusMsg = "decode error"
			s.busy = false
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		s.models = result.Data
		s.statusMsg = fmt.Sprintf("%d models", len(s.models))
		s.busy = false
		s.mu.Unlock()
	}()
}

func (s *registryScene) Update(_ *SharedState) error {
	_ = s.backBtn.Update()
	_ = s.refreshBtn.Update()
	s.mu.Lock()
	modelsEmpty := s.models == nil
	s.mu.Unlock()
	if modelsEmpty {
		s.fetchModels()
	}
	return nil
}

func (s *registryScene) Draw(screen *ebiten.Image, _ *SharedState) {
	screen.Fill(color.RGBA{12, 12, 20, 255})
	vector.DrawFilledRect(screen, 0, 0, 1280, 52, color.RGBA{22, 22, 38, 255}, false)
	s.backBtn.Draw(screen)
	s.refreshBtn.Draw(screen)

	// Model list area.
	vector.DrawFilledRect(screen, 16, 60, 600, 720, color.RGBA{18, 18, 30, 255}, false)
	// Detail panel.
	vector.DrawFilledRect(screen, 632, 60, 632, 720, color.RGBA{18, 18, 30, 255}, false)

	s.mu.Lock()
	models := s.models
	selected := s.selected
	s.mu.Unlock()

	rowH := float32(42)
	for i, m := range models {
		if i >= 16 {
			break
		}
		y := float32(68) + float32(i)*rowH
		bg := color.RGBA{28, 28, 44, 255}
		if i == selected {
			bg = color.RGBA{50, 50, 80, 255}
		}
		vector.DrawFilledRect(screen, 24, y, 584, rowH-4, bg, false)
		_ = m.Name
	}
}
