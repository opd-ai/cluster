//go:build js && wasm

package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// ProgressBar renders a horizontal percentage bar.
type ProgressBar struct {
	bounds    Rect
	Value     float64 // 0–1
	BGColor   color.Color
	FillColor color.Color
}

// NewProgressBar creates a ProgressBar with default colors.
func NewProgressBar() *ProgressBar {
	return &ProgressBar{
		BGColor:   color.RGBA{40, 40, 55, 255},
		FillColor: color.RGBA{60, 180, 80, 255},
	}
}

// Bounds implements Widget.
func (p *ProgressBar) Bounds() Rect { return p.bounds }

// SetBounds implements Widget.
func (p *ProgressBar) SetBounds(r Rect) { p.bounds = r }

// Update implements Widget.
func (p *ProgressBar) Update() error { return nil }

// Draw implements Widget.
func (p *ProgressBar) Draw(screen *ebiten.Image) {
	x, y, w, h := float32(p.bounds.X), float32(p.bounds.Y),
		float32(p.bounds.W), float32(p.bounds.H)
	vector.DrawFilledRect(screen, x, y, w, h, p.BGColor, false)

	fill := float32(p.Value) * w
	if fill > 0 {
		vector.DrawFilledRect(screen, x, y, fill, h, p.FillColor, false)
	}
}
