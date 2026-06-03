//go:build js && wasm

// WebSocket client for the WASM build.
// Uses nhooyr.io/websocket which supports WASM (js/wasm target).
package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/opd-ai/cluster/internal/uiapi"
	"nhooyr.io/websocket"
)

// wsClient manages a reconnecting WebSocket connection.
type wsClient struct {
	url    string
	onMsg  func(uiapi.Message)
	cancel context.CancelFunc
}

// newWSClient creates a new wsClient.
func newWSClient(url string, onMsg func(uiapi.Message)) *wsClient {
	return &wsClient{url: url, onMsg: onMsg}
}

// run connects and reads messages, reconnecting on error.
func (c *wsClient) run() {
	for {
		if err := c.connect(); err != nil {
			log.Printf("ws error: %v — reconnecting in 3s", err)
			time.Sleep(3 * time.Second)
		}
	}
}

// connect establishes a single WebSocket connection and reads until error.
func (c *wsClient) connect() error {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	defer cancel()

	conn, _, err := websocket.Dial(ctx, c.url, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var msg uiapi.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("ws decode: %v", err)
			continue
		}
		c.onMsg(msg)
	}
}

// close cancels the current connection.
func (c *wsClient) close() {
	if c.cancel != nil {
		c.cancel()
	}
}
