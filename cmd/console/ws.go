package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/opd-ai/cluster/internal/uiapi"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// serveWebSocket upgrades r to a WebSocket connection and pumps messages
// from ch until it is closed or the client disconnects.
func serveWebSocket(w http.ResponseWriter, r *http.Request, ch <-chan uiapi.Message) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Read loop — detect client close.
	go func() {
		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				cancel()
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := wsjson.Write(ctx, conn, msg); err != nil {
				// Encode and send as raw JSON to allow interface{} Payload.
				raw, _ := json.Marshal(msg)
				if err2 := conn.Write(ctx, websocket.MessageText, raw); err2 != nil {
					return
				}
			}
		}
	}
}
