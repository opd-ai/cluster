//go:build js && wasm

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/opd-ai/cluster/internal/ui"
	"github.com/opd-ai/cluster/internal/uiapi"
	"image/color"
)

// authScene shows a login form where the user enters an API key.
type authScene struct {
	onSuccess func(token string, role uiapi.Role)
	keyInput  string
	errMsg    string
	loginBtn  *ui.Button
	loginURL  string
	typing    bool
}

func newAuthScene(onSuccess func(string, uiapi.Role)) *authScene {
	a := &authScene{
		onSuccess: onSuccess,
		loginURL:  "/api/login",
	}
	a.loginBtn = ui.NewButton("Login", func() {
		a.doLogin()
	})
	a.loginBtn.SetBounds(ui.Rect{X: 540, Y: 420, W: 200, H: 44})
	return a
}

func (a *authScene) doLogin() {
	req := uiapi.LoginRequest{APIKey: strings.TrimSpace(a.keyInput)}
	body, err := json.Marshal(req)
	if err != nil {
		a.errMsg = "internal error: " + err.Error()
		return
	}
	resp, err := http.Post(a.loginURL, "application/json", bytes.NewReader(body))
	if err != nil {
		a.errMsg = "connection error: " + err.Error()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		a.errMsg = "invalid API key"
		return
	}
	var lr uiapi.LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		a.errMsg = "bad response"
		return
	}
	a.onSuccess(lr.Token, uiapi.Role(lr.Role))
}

func (a *authScene) Update(_ *SharedState) error {
	return a.loginBtn.Update()
}

func (a *authScene) Draw(screen *ebiten.Image, _ *SharedState) {
	screen.Fill(color.RGBA{18, 18, 28, 255})

	// Login box background.
	vector.DrawFilledRect(screen, 460, 340, 360, 180, color.RGBA{30, 30, 45, 255}, false)

	// Input field background.
	vector.DrawFilledRect(screen, 476, 368, 328, 38, color.RGBA{50, 50, 70, 255}, false)

	a.loginBtn.Draw(screen)
	_ = context.Background() // keep import
}
