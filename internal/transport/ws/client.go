package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"darmie/internal/protocol"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingInterval   = 54 * time.Second // must be < pongWait
	maxMessageSize = 65536            // 64 KB (accommodates SDP blobs)
	sendBufSize    = 256
	maxMsgPerSec   = 10
)

// Client is a single WebSocket connection and its per-connection state. It owns
// transport concerns only; all application logic lives behind the Hub's
// service calls.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

	// Set after authentication.
	userID       string
	username     string
	sessionToken string // persisted session; doubles as the upload credential

	// Rooms this client is currently in.
	rooms   map[string]struct{}
	roomsMu sync.RWMutex

	once sync.Once // disconnect cleanup runs exactly once

	// Text-message rate limiting.
	msgMu    sync.Mutex
	msgCount int
	msgWin   time.Time
}

func newClient(conn *websocket.Conn, hub *Hub) *Client {
	return &Client{
		hub:   hub,
		conn:  conn,
		send:  make(chan []byte, sendBufSize),
		rooms: make(map[string]struct{}),
	}
}

// ─── Membership bookkeeping ─────────────────────────────────────────────────

func (c *Client) addRoom(id string) {
	c.roomsMu.Lock()
	c.rooms[id] = struct{}{}
	c.roomsMu.Unlock()
}

func (c *Client) removeRoom(id string) {
	c.roomsMu.Lock()
	delete(c.rooms, id)
	c.roomsMu.Unlock()
}

func (c *Client) roomIDs() []string {
	c.roomsMu.RLock()
	defer c.roomsMu.RUnlock()
	ids := make([]string, 0, len(c.rooms))
	for id := range c.rooms {
		ids = append(ids, id)
	}
	return ids
}

// ─── Lifecycle ──────────────────────────────────────────────────────────────

// close tears down the connection exactly once, regardless of which pump calls it.
func (c *Client) close() {
	c.once.Do(func() {
		c.conn.Close()
		c.hub.handleDisconnect(c)
	})
}

// readPump reads frames and routes them through the hub.
func (c *Client) readPump() {
	defer c.close()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("ws read error: %v", err)
			}
			return
		}
		c.hub.route(c, data)
	}
}

// writePump flushes the send channel and sends periodic pings.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ─── Send helpers ─────────────────────────────────────────────────────────────

func (c *Client) sendMsg(m *protocol.Message) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.trySend(data)
}

// trySend enqueues raw bytes, disconnecting the client if its buffer is full.
// Signaling messages must never be silently dropped, so a slow client is closed
// rather than allowed to fall behind.
func (c *Client) trySend(data []byte) {
	select {
	case c.send <- data:
	default:
		go c.close()
	}
}

func (c *Client) sendError(msg string) {
	c.sendMsg(mustMsg(protocol.TypeError, protocol.ErrorPayload{Message: msg}))
}

func (c *Client) sendAuthError(msg string) {
	c.sendMsg(mustMsg(protocol.TypeAuthError, protocol.AuthErrorPayload{Message: msg}))
}

// allowMessage reports whether this client is within the text rate limit.
func (c *Client) allowMessage() bool {
	now := time.Now()
	c.msgMu.Lock()
	defer c.msgMu.Unlock()
	if now.Sub(c.msgWin) >= time.Second {
		c.msgWin = now
		c.msgCount = 0
	}
	c.msgCount++
	return c.msgCount <= maxMsgPerSec
}
