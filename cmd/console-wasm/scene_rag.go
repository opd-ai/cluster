package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/opd-ai/cluster/internal/ui"
)

// ragChunk is a single RAG document chunk shown in the browser.
type ragChunk struct {
	ID      string  `json:"id"`
	Source  string  `json:"source"`
	Content string  `json:"content"`
	Score   float64 `json:"score,omitempty"`
}

// ragQueryResponse is the response from /rag/query.
type ragQueryResponse struct {
	Chunks []ragChunk `json:"chunks"`
}

// ragAdminScene lets operators browse/ingest RAG collections.
type ragAdminScene struct {
	onBack     func()
	backBtn    *ui.Button
	queryBtn   *ui.Button
	ingestBtn  *ui.Button
	queryInput string
	collection string
	chunks     []ragChunk
	statusMsg  string
	busy       bool
}

func newRAGAdminScene(onBack func()) *ragAdminScene {
	s := &ragAdminScene{
		onBack:     onBack,
		collection: "default",
	}
	s.backBtn = ui.NewButton("← Back", onBack)
	s.backBtn.SetBounds(ui.Rect{X: 12, Y: 12, W: 90, H: 32})
	s.queryBtn = ui.NewButton("Search", func() { s.runQuery() })
	s.queryBtn.SetBounds(ui.Rect{X: 900, Y: 752, W: 120, H: 36})
	s.ingestBtn = ui.NewButton("Re-index", func() { s.triggerReindex() })
	s.ingestBtn.SetBounds(ui.Rect{X: 1040, Y: 752, W: 220, H: 36})
	return s
}

func (s *ragAdminScene) runQuery() {
	q := strings.TrimSpace(s.queryInput)
	if q == "" || s.busy {
		return
	}
	s.busy = true
	go func() {
		defer func() { s.busy = false }()
		url := fmt.Sprintf("/rag/query?collection=%s&q=%s&top_k=10",
			s.collection, q)
		resp, err := http.Get(url) // #nosec G107
		if err != nil {
			s.statusMsg = "query error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		var result ragQueryResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			s.statusMsg = "decode error"
			return
		}
		s.chunks = result.Chunks
		s.statusMsg = fmt.Sprintf("%d chunks", len(s.chunks))
	}()
}

func (s *ragAdminScene) triggerReindex() {
	if s.busy {
		return
	}
	s.busy = true
	go func() {
		defer func() { s.busy = false }()
		resp, err := http.Post("/api/reindex", "application/json", nil)
		if err != nil {
			s.statusMsg = "reindex error: " + err.Error()
			return
		}
		resp.Body.Close()
		s.statusMsg = "re-index triggered"
	}()
}

func (s *ragAdminScene) Update(_ *SharedState) error {
	_ = s.backBtn.Update()
	_ = s.queryBtn.Update()
	_ = s.ingestBtn.Update()
	return nil
}

func (s *ragAdminScene) Draw(screen *ebiten.Image, _ *SharedState) {
	screen.Fill(color.RGBA{12, 12, 20, 255})
	vector.DrawFilledRect(screen, 0, 0, 1280, 52, color.RGBA{22, 22, 38, 255}, false)
	s.backBtn.Draw(screen)
	s.queryBtn.Draw(screen)
	s.ingestBtn.Draw(screen)

	// Chunks list area.
	vector.DrawFilledRect(screen, 16, 60, 1248, 660, color.RGBA{18, 18, 30, 255}, false)

	// Draw up to 8 chunk rows.
	for i, chunk := range s.chunks {
		if i >= 8 {
			break
		}
		y := float32(68 + i*80)
		vector.DrawFilledRect(screen, 24, y, 1232, 72, color.RGBA{28, 28, 44, 255}, false)
		_ = chunk.Source
	}

	// Query input.
	vector.DrawFilledRect(screen, 16, 752, 860, 36, color.RGBA{40, 40, 60, 255}, false)
}
