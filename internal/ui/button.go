package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// Button is a clickable button widget.
type Button struct {
	bounds  Rect
	Label   string
	OnClick func()
	hovered bool
	pressed bool

	// Colors
	BgColor      color.Color
	HoverColor   color.Color
	PressColor   color.Color
	TextColor    color.Color
}

// NewButton creates a button with default colors.
func NewButton(label string, onClick func()) *Button {
	return &Button{
		Label:      label,
		OnClick:    onClick,
		BgColor:    color.RGBA{60, 60, 80, 255},
		HoverColor: color.RGBA{80, 80, 110, 255},
		PressColor: color.RGBA{40, 40, 60, 255},
		TextColor:  color.White,
	}
}

// Bounds implements Widget.
func (b *Button) Bounds() Rect { return b.bounds }

// SetBounds implements Widget.
func (b *Button) SetBounds(r Rect) { b.bounds = r }

// Update implements Widget.
func (b *Button) Update() error {
	mx, my := ebiten.CursorPosition()
	b.hovered = b.bounds.Contains(mx, my)
	if b.hovered && ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		b.pressed = true
	} else if b.pressed && !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		b.pressed = false
		if b.hovered && b.OnClick != nil {
			b.OnClick()
		}
	}
	return nil
}

// Draw implements Widget.
func (b *Button) Draw(screen *ebiten.Image) {
	bg := b.BgColor
	if b.pressed {
		bg = b.PressColor
	} else if b.hovered {
		bg = b.HoverColor
	}

	x, y, w, h := float32(b.bounds.X), float32(b.bounds.Y),
		float32(b.bounds.W), float32(b.bounds.H)
	vector.DrawFilledRect(screen, x, y, w, h, bg, false)
}
