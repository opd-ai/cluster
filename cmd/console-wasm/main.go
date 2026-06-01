// Command console-wasm is the WASM browser client for the cluster management
// console.  It must be compiled with:
//
//	GOOS=js GOARCH=wasm go build -o main.wasm ./cmd/console-wasm
//
// The resulting main.wasm is served by cmd/console together with the standard
// wasm_exec.js shim from the Go distribution.
//
// Architecture:
//   - ebiten.RunGame drives the render loop at 60 fps.
//   - An App struct holds a scene router and a WS connection to the console
//     server's /api/ws endpoint.
//   - Scenes (auth, cluster, chat, image/video studio, training) satisfy the
//     Scene interface: Update() + Draw(screen).
//   - A SharedState struct carries data pushed by the server and is updated
//     from the WS goroutine; the render goroutine reads it under a RWMutex.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/opd-ai/cluster/internal/uiapi"
)

// -------------------------------------------------------------------------
// Scene interface
// -------------------------------------------------------------------------

// Scene is a full-screen view managed by the App's scene router.
type Scene interface {
	Update(state *SharedState) error
	Draw(screen *ebiten.Image, state *SharedState)
}

// -------------------------------------------------------------------------
// SharedState — thread-safe server data
// -------------------------------------------------------------------------

// SharedState holds the latest server-pushed data.
type SharedState struct {
	mu      sync.RWMutex
	Cluster uiapi.ClusterState
	Jobs    []uiapi.JobState
	Logs    []uiapi.LogLine
	Token   string
	Role    uiapi.Role
}

// ApplyMessage updates SharedState from a server push.
func (s *SharedState) ApplyMessage(msg uiapi.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.Type {
	case uiapi.MsgClusterState:
		if raw, err := json.Marshal(msg.Payload); err == nil {
			_ = json.Unmarshal(raw, &s.Cluster)
		}
	case uiapi.MsgLogLine:
		if raw, err := json.Marshal(msg.Payload); err == nil {
			var line uiapi.LogLine
			if err := json.Unmarshal(raw, &line); err == nil {
				s.Logs = append(s.Logs, line)
				if len(s.Logs) > 500 {
					s.Logs = s.Logs[len(s.Logs)-500:]
				}
			}
		}
	case uiapi.MsgJobProgress:
		// Handled by specific scenes.
	}
}

// -------------------------------------------------------------------------
// App — Ebitengine game object
// -------------------------------------------------------------------------

// App is the root Ebitengine game.
type App struct {
	state   SharedState
	scenes  map[string]Scene
	current string
	ws      *wsClient
	wsURL   string
}

// NewApp creates a new App.
func NewApp(wsURL string) *App {
	a := &App{
		wsURL: wsURL,
		scenes: make(map[string]Scene),
	}
	a.registerScenes()
	a.current = "auth"
	return a
}

func (a *App) registerScenes() {
	a.scenes["auth"] = newAuthScene(func(token string, role uiapi.Role) {
		a.state.mu.Lock()
		a.state.Token = token
		a.state.Role = role
		a.state.mu.Unlock()
		a.connectWS(token)
		a.current = "cluster"
	})
	a.scenes["cluster"] = newClusterScene(func(target string) {
		a.current = target
	})
	a.scenes["chat"] = newChatScene(func() { a.current = "cluster" })
	a.scenes["imagestudio"] = newImageStudioScene(func() { a.current = "cluster" })
	a.scenes["videostudio"] = newVideoStudioScene(func() { a.current = "cluster" })
	a.scenes["training"] = newTrainingScene(func() { a.current = "cluster" })
	a.scenes["ragadmin"] = newRAGAdminScene(func() { a.current = "cluster" })
	a.scenes["registry"] = newRegistryScene(func() { a.current = "cluster" })
}

// connectWS opens the WebSocket connection.
func (a *App) connectWS(token string) {
	url := fmt.Sprintf("%s?token=%s", a.wsURL, token)
	c := newWSClient(url, func(msg uiapi.Message) {
		a.state.ApplyMessage(msg)
	})
	go c.run()
	a.ws = c
}

// Update implements ebiten.Game.
func (a *App) Update() error {
	s := a.scenes[a.current]
	if s == nil {
		return fmt.Errorf("unknown scene: %s", a.current)
	}
	return s.Update(&a.state)
}

// Draw implements ebiten.Game.
func (a *App) Draw(screen *ebiten.Image) {
	s := a.scenes[a.current]
	if s == nil {
		return
	}
	s.Draw(screen, &a.state)
}

// Layout implements ebiten.Game.
func (a *App) Layout(_, _ int) (int, int) {
	return 1280, 800
}

// -------------------------------------------------------------------------
// main
// -------------------------------------------------------------------------

func main() {
	// wsURL is injected by index.html as window.consoleWSURL.
	wsURL := jsGlobal("consoleWSURL", "ws://localhost:8080/api/ws")

	ebiten.SetWindowTitle("Cluster Console")
	ebiten.SetWindowSize(1280, 800)

	app := NewApp(wsURL)
	if err := ebiten.RunGame(app); err != nil {
		log.Fatal(err)
	}
}
