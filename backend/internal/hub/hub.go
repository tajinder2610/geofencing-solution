// Package hub fans real-time alert messages out to connected WebSocket clients.
package hub

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 25 * time.Second
	maxMessageSize = 1024
	sendBuffer     = 64
)

type client struct {
	conn *websocket.Conn
	send chan []byte
}

// Hub tracks connected clients and broadcasts messages to all of them.
type Hub struct {
	upgrader websocket.Upgrader

	mu      sync.RWMutex
	clients map[*client]struct{}
	closed  bool
}

// New creates a Hub. checkOrigin decides whether a WebSocket handshake origin is accepted.
func New(checkOrigin func(r *http.Request) bool) *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			CheckOrigin:     checkOrigin,
		},
		clients: make(map[*client]struct{}),
	}
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Broadcast queues msg for every client and returns how many accepted it.
// A client whose buffer is full is too slow and is disconnected rather than blocking everyone else.
func (h *Hub) Broadcast(msg []byte) int {
	h.mu.RLock()
	var slow []*client
	delivered := 0
	for c := range h.clients {
		select {
		case c.send <- msg:
			delivered++
		default:
			slow = append(slow, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range slow {
		log.Printf("hub: dropping slow client %s", c.conn.RemoteAddr())
		h.remove(c)
	}
	return delivered
}

// ServeWS upgrades the request and runs the client's read/write pumps until it disconnects.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has already written the error response
	}
	c := &client{conn: conn, send: make(chan []byte, sendBuffer)}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		conn.Close()
		return
	}
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	go h.writePump(c)
	h.readPump(c) // blocks until the client goes away
}

// Close disconnects all clients and rejects new ones.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		h.remove(c)
	}
}

// remove unregisters c exactly once and closes its connection.
func (h *Hub) remove(c *client) {
	h.mu.Lock()
	_, ok := h.clients[c]
	delete(h.clients, c)
	h.mu.Unlock()
	if ok {
		close(c.send)
		c.conn.Close()
	}
}

// readPump discards inbound messages; it exists to process control frames and detect disconnects.
func (h *Hub) readPump(c *client) {
	defer h.remove(c)
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *Hub) writePump(c *client) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, ""))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				h.remove(c)
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.remove(c)
				return
			}
		}
	}
}
