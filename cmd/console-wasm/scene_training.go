//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/opd-ai/cluster/internal/ui"
	"github.com/opd-ai/cluster/internal/uiapi"
)

// trainingScene shows active training jobs with loss/lr sparklines.
type trainingScene struct {
	onBack    func()
	backBtn   *ui.Button
	lossLines map[string]*ui.Sparkline
	lrLines   map[string]*ui.Sparkline
	jobs      []uiapi.JobState
}

func newTrainingScene(onBack func()) *trainingScene {
	s := &trainingScene{
		onBack:    onBack,
		lossLines: make(map[string]*ui.Sparkline),
		lrLines:   make(map[string]*ui.Sparkline),
	}
	s.backBtn = ui.NewButton("← Back", onBack)
	s.backBtn.SetBounds(ui.Rect{X: 12, Y: 12, W: 90, H: 32})
	return s
}

func (s *trainingScene) Update(state *SharedState) error {
	_ = s.backBtn.Update()

	// Refresh job list every N ticks would use a ticker in production.
	// For this scaffolding, fetch once when jobs is nil.
	if s.jobs == nil {
		s.fetchJobs()
	}

	// Apply server-pushed training metrics.
	// (In production this is handled via WebSocket messages.)
	_ = state
	return nil
}

func (s *trainingScene) fetchJobs() {
	resp, err := http.Get("/api/jobs")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var all []uiapi.JobState
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return
	}
	var training []uiapi.JobState
	for _, j := range all {
		if j.Kind == uiapi.JobKindTraining {
			training = append(training, j)
		}
	}
	s.jobs = training
}

func (s *trainingScene) Draw(screen *ebiten.Image, _ *SharedState) {
	screen.Fill(color.RGBA{12, 12, 20, 255})
	vector.DrawFilledRect(screen, 0, 0, 1280, 52, color.RGBA{22, 22, 38, 255}, false)
	s.backBtn.Draw(screen)

	for i, job := range s.jobs {
		y := 68 + i*210
		drawJobCard(screen, 16, y, 1248, 190, job, s.lossLines[job.ID])
	}
}

func drawJobCard(screen *ebiten.Image, x, y, w, h int, job uiapi.JobState, sl *ui.Sparkline) {
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(w), float32(h),
		color.RGBA{22, 22, 38, 255}, false)

	_ = fmt.Sprintf(
		"job %s status %s progress %.0f%%",
		job.ID, job.Status, job.Progress*100,
	)

	pb := ui.NewProgressBar()
	pb.Value = job.Progress
	pb.SetBounds(ui.Rect{X: x + 8, Y: y + 8, W: w - 16, H: 8})
	pb.Draw(screen)

	if sl != nil {
		sl.SetBounds(ui.Rect{X: x + 8, Y: y + 30, W: w - 16, H: 100})
		sl.Draw(screen)
	}
}
