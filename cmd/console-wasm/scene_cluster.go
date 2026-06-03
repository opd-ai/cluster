//go:build js && wasm

package main

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/opd-ai/cluster/internal/ui"
)

// clusterScene shows the cluster overview: node cards, job queue, log tail.
type clusterScene struct {
	onNavigate     func(target string)
	chatBtn        *ui.Button
	imageBtn       *ui.Button
	videoBtn       *ui.Button
	trainBtn       *ui.Button
	ragBtn         *ui.Button
	regBtn         *ui.Button
	nodeSparklines map[string]*ui.Sparkline
}

func newClusterScene(onNavigate func(string)) *clusterScene {
	s := &clusterScene{
		onNavigate:     onNavigate,
		nodeSparklines: make(map[string]*ui.Sparkline),
	}
	s.chatBtn = ui.NewButton("Chat", func() { onNavigate("chat") })
	s.chatBtn.SetBounds(ui.Rect{X: 20, Y: 12, W: 90, H: 32})

	s.imageBtn = ui.NewButton("Images", func() { onNavigate("imagestudio") })
	s.imageBtn.SetBounds(ui.Rect{X: 120, Y: 12, W: 90, H: 32})

	s.videoBtn = ui.NewButton("Video", func() { onNavigate("videostudio") })
	s.videoBtn.SetBounds(ui.Rect{X: 220, Y: 12, W: 90, H: 32})

	s.trainBtn = ui.NewButton("Training", func() { onNavigate("training") })
	s.trainBtn.SetBounds(ui.Rect{X: 320, Y: 12, W: 90, H: 32})

	s.ragBtn = ui.NewButton("RAG", func() { onNavigate("ragadmin") })
	s.ragBtn.SetBounds(ui.Rect{X: 420, Y: 12, W: 80, H: 32})

	s.regBtn = ui.NewButton("Registry", func() { onNavigate("registry") })
	s.regBtn.SetBounds(ui.Rect{X: 510, Y: 12, W: 100, H: 32})
	return s
}

func (s *clusterScene) Update(state *SharedState) error {
	for _, btn := range []*ui.Button{s.chatBtn, s.imageBtn, s.videoBtn, s.trainBtn, s.ragBtn, s.regBtn} {
		if err := btn.Update(); err != nil {
			return err
		}
	}

	// Update sparklines for each node.
	state.mu.RLock()
	for i := range state.Cluster.Nodes {
		n := &state.Cluster.Nodes[i]
		sl, ok := s.nodeSparklines[n.Name]
		if !ok {
			sl = ui.NewSparkline(60)
			s.nodeSparklines[n.Name] = sl
		}
		sl.Push(float64(n.VRAMUsed))
	}
	state.mu.RUnlock()
	return nil
}

func (s *clusterScene) Draw(screen *ebiten.Image, state *SharedState) {
	screen.Fill(color.RGBA{12, 12, 20, 255})

	// Navigation bar.
	vector.DrawFilledRect(screen, 0, 0, 1280, 52, color.RGBA{22, 22, 38, 255}, false)
	for _, btn := range []*ui.Button{s.chatBtn, s.imageBtn, s.videoBtn, s.trainBtn, s.ragBtn, s.regBtn} {
		btn.Draw(screen)
	}

	// Node cards.
	state.mu.RLock()
	nodes := state.Cluster.Nodes
	state.mu.RUnlock()

	cols := 4
	cardW, cardH := 290, 150
	for i, node := range nodes {
		col := i % cols
		row := i / cols
		x := 20 + col*(cardW+16)
		y := 68 + row*(cardH+16)
		drawNodeCard(screen, x, y, cardW, cardH, node.Name, node.Healthy,
			s.nodeSparklines[node.Name])
	}

	_ = fmt.Sprintf // keep import
}

func drawNodeCard(screen *ebiten.Image, x, y, w, h int, name string, healthy bool, sl *ui.Sparkline) {
	bg := color.RGBA{28, 28, 42, 255}
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(w), float32(h), bg, false)

	indicator := color.RGBA{60, 200, 80, 255}
	if !healthy {
		indicator = color.RGBA{220, 60, 60, 255}
	}
	vector.DrawFilledRect(screen, float32(x+w-18), float32(y+8), 10, 10, indicator, false)

	if sl != nil {
		sl.SetBounds(ui.Rect{X: x + 8, Y: y + 60, W: w - 16, H: 60})
		sl.Draw(screen)
	}
	_ = name
}
