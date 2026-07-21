package consolecmd

import (
	"sync"

	"github.com/gorilla/websocket"
)

// consoleWebSocketHandlers owns upgraded console connections. net/http no
// longer owns a connection after hijacking, so http.Server.Shutdown cannot
// close it or wait for its handler.
type consoleWebSocketHandlers struct {
	mu          sync.Mutex
	closing     bool
	active      sync.WaitGroup
	connections map[*websocket.Conn]struct{}
}

func (h *consoleWebSocketHandlers) Begin() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	h.active.Add(1)
	return true
}

func (h *consoleWebSocketHandlers) Done() {
	h.active.Done()
}

func (h *consoleWebSocketHandlers) Track(conn *websocket.Conn) bool {
	if conn == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	if h.connections == nil {
		h.connections = make(map[*websocket.Conn]struct{})
	}
	h.connections[conn] = struct{}{}
	return true
}

func (h *consoleWebSocketHandlers) Untrack(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.connections, conn)
	h.mu.Unlock()
}

func (h *consoleWebSocketHandlers) CloseAndWait() {
	h.mu.Lock()
	h.closing = true
	connections := make([]*websocket.Conn, 0, len(h.connections))
	for conn := range h.connections {
		connections = append(connections, conn)
	}
	h.mu.Unlock()

	for _, conn := range connections {
		_ = conn.Close()
	}
	h.active.Wait()
}
