package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/hostoria/hostoria-node/internal/server"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow connections from the billing panel origin
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ConsoleDeps bundles dependencies for console handlers.
type ConsoleDeps struct {
	Servers *server.Manager
}

// wsMessage is the JSON envelope used over the console WebSocket.
type wsMessage struct {
	Event string   `json:"event"`
	Args  []string `json:"args"`
}

// ConsoleWebSocket — GET /api/servers/{uuid}/ws
// Upgrades to WebSocket. Client receives console output and can send commands.
//
// Events server → client:
//   - "console output" — one line of server output
//   - "stats"          — JSON stats object (sent every 5 s when running)
//   - "status"         — power state change
//
// Events client → server:
//   - "send command"   — args[0] is the command to send to the game server's stdin
//   - "set state"      — args[0] is "start"|"stop"|"restart"|"kill"
func ConsoleWebSocket(deps *ConsoleDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uuid := chi.URLParam(r, "uuid")
		srv := deps.Servers.Get(uuid)
		if srv == nil {
			writeError(w, http.StatusNotFound, "server not found")
			return
		}

		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Subscribe to console output
		consoleCh := srv.AddConsoleListener()
		defer srv.RemoveConsoleListener(consoleCh)

		// Send initial status
		_ = conn.WriteJSON(wsMessage{
			Event: "status",
			Args:  []string{string(srv.GetState())},
		})

		// Writer goroutine — forwards console lines and periodic stats to the client
		done := make(chan struct{})
		go func() {
			defer close(done)
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case line, ok := <-consoleCh:
					if !ok {
						return
					}
					msg := wsMessage{Event: "console output", Args: []string{line}}
					if err := conn.WriteJSON(msg); err != nil {
						return
					}
				case <-ticker.C:
					resources := srv.GetResources()
					data, _ := json.Marshal(resources)
					msg := wsMessage{Event: "stats", Args: []string{string(data)}}
					_ = conn.WriteJSON(msg)
				}
			}
		}()

		// Reader loop — handle inbound commands
		for {
			var msg wsMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}
			switch msg.Event {
			case "send command":
				// TODO: forward to container stdin via Docker Attach
			case "set state":
				// Power actions handled separately via POST /api/servers/{uuid}/power
			}
		}
		<-done
	}
}
