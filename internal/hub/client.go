package hub

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"darmie/internal/protocol"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingInterval   = 54 * time.Second // must be less than pongWait
	maxMessageSize = 65536            // 64 KB (accommodates SDP blobs)
	sendBufSize    = 256
)

// Client represents a single WebSocket connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte

	// Set after authentication.
	userID      string
	username    string
	uploadToken string // revoked on disconnect

	// Rooms this client is currently in (protected by roomsMu).
	rooms   map[string]struct{}
	roomsMu sync.RWMutex

	// once ensures disconnect cleanup runs exactly once.
	once sync.Once

	// Rate limiting for text messages (10 per second).
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

// close tears down the connection exactly once, regardless of which pump calls it.
func (c *Client) close() {
	c.once.Do(func() {
		c.conn.Close()
		c.hub.handleDisconnect(c)
	})
}

// readPump reads messages from the WebSocket and routes them through the hub.
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

// writePump flushes the send channel to the WebSocket and sends periodic pings.
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

// sendMsg serialises a Message and queues it for the client.
func (c *Client) sendMsg(m *protocol.Message) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.trySend(data)
}

// trySend enqueues raw bytes; disconnects the client if its buffer is full.
// Signaling messages (offer/answer/ICE) must never be silently dropped.
func (c *Client) trySend(data []byte) {
	select {
	case c.send <- data:
	default:
		// The client's send buffer is full; it is too slow to keep connected.
		go c.close()
	}
}

// sendError sends a generic error message to the client.
func (c *Client) sendError(msg string) {
	c.sendMsg(mustMsg(protocol.TypeError, protocol.ErrorPayload{Message: msg}))
}

// sendAuthError sends an auth_error message to the client.
func (c *Client) sendAuthError(msg string) {
	c.sendMsg(mustMsg(protocol.TypeAuthError, protocol.AuthErrorPayload{Message: msg}))
}

// allowMessage returns true if this client is within the rate limit (10 msgs/sec).
// Excess messages are silently dropped rather than generating error traffic.
func (c *Client) allowMessage() bool {
	const maxPerSec = 10
	now := time.Now()
	c.msgMu.Lock()
	defer c.msgMu.Unlock()
	if now.Sub(c.msgWin) >= time.Second {
		c.msgWin = now
		c.msgCount = 0
	}
	c.msgCount++
	return c.msgCount <= maxPerSec
}
