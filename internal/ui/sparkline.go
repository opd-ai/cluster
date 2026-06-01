package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// sparklinePoint is one sample in a Sparkline.
type sparklinePoint struct {
	Value float64
}

// Sparkline draws a small line chart from a sliding window of values.
type Sparkline struct {
	bounds     Rect
	samples    []sparklinePoint
	maxSamples int
	LineColor  color.Color
	BGColor    color.Color
}

// NewSparkline creates a Sparkline with a given window size.
func NewSparkline(maxSamples int) *Sparkline {
	return &Sparkline{
		maxSamples: maxSamples,
		LineColor:  color.RGBA{100, 200, 100, 255},
		BGColor:    color.RGBA{20, 20, 30, 200},
	}
}

// Bounds implements Widget.
func (s *Sparkline) Bounds() Rect { return s.bounds }

// SetBounds implements Widget.
func (s *Sparkline) SetBounds(r Rect) { s.bounds = r }

// Push adds a new data point.
func (s *Sparkline) Push(v float64) {
	s.samples = append(s.samples, sparklinePoint{v})
	if len(s.samples) > s.maxSamples {
		s.samples = s.samples[len(s.samples)-s.maxSamples:]
	}
}

// Update implements Widget.
func (s *Sparkline) Update() error { return nil }

// Draw implements Widget.
func (s *Sparkline) Draw(screen *ebiten.Image) {
	x, y, w, h := float32(s.bounds.X), float32(s.bounds.Y),
		float32(s.bounds.W), float32(s.bounds.H)

	vector.DrawFilledRect(screen, x, y, w, h, s.BGColor, false)

	n := len(s.samples)
	if n < 2 {
		return
	}

	// Find min/max.
	min, max := s.samples[0].Value, s.samples[0].Value
	for _, p := range s.samples {
		if p.Value < min {
			min = p.Value
		}
		if p.Value > max {
			max = p.Value
		}
	}
	span := max - min
	if span == 0 {
		span = 1
	}

	// Draw lines between consecutive points.
	step := w / float32(n-1)
	for i := 1; i < n; i++ {
		x0 := x + float32(i-1)*step
		x1 := x + float32(i)*step
		y0 := y + h - float32((s.samples[i-1].Value-min)/span)*h
		y1 := y + h - float32((s.samples[i].Value-min)/span)*h
		vector.StrokeLine(screen, x0, y0, x1, y1, 1.5, s.LineColor, false)
	}
}
