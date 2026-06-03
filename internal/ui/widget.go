//go:build js && wasm

// Package ui provides the widget library for the console WASM client.
//
// All widgets follow the Update(state) → Draw(screen) pattern backed by
// Ebitengine primitives.  Font rendering uses golang.org/x/image/font with
// an embedded open font (IBM Plex Mono).
//
// This file defines the common Widget interface and shared layout types.
package ui

import "github.com/hajimehoshi/ebiten/v2"

// Widget is the base interface for all UI components.
type Widget interface {
	// Update processes input and advances animation state.
	Update() error
	// Draw renders the widget onto the target image.
	Draw(screen *ebiten.Image)
	// Bounds returns the widget's bounding rectangle.
	Bounds() Rect
	// SetBounds sets the widget's bounding rectangle.
	SetBounds(r Rect)
}

// Rect is a 2-D axis-aligned rectangle.
type Rect struct {
	X, Y, W, H int
}

// Contains returns true if (px, py) is inside r.
func (r Rect) Contains(px, py int) bool {
	return px >= r.X && px < r.X+r.W && py >= r.Y && py < r.Y+r.H
}
