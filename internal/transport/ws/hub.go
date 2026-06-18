// Package ws is the primary (driving) adapter: it terminates WebSocket and
// file-HTTP traffic and translates it into calls on the core services. It holds
// no business rules — only transport concerns and the in-memory presence model.
package ws

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"

	"darmie/internal/core"
	"darmie/internal/protocol"
)

// Hub is the WebSocket gateway. It wires the presence Registry to the core
// services and dispatches incoming protocol messages.
type Hub struct {
	reg   *Registry
	auth  *core.AuthService
	chat  *core.ChatService
	files *core.FileService

	maxUploadBytes int64
}

// Config carries the tunables and collaborators for a Hub.
type Config struct {
	Auth           *core.AuthService
	Chat           *core.ChatService
	Files          *core.FileService
	MaxRooms       int
	MaxRoomSize    int
	MaxUploadBytes int64
}

// New constructs a Hub and its presence Registry from cfg.
func New(cfg Config) *Hub {
	return &Hub{
		reg:            NewRegistry(cfg.MaxRooms, cfg.MaxRoomSize),
		auth:           cfg.Auth,
		chat:           cfg.Chat,
		files:          cfg.Files,
		maxUploadBytes: cfg.MaxUploadBytes,
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// Same-origin only. Adjust for cross-origin production deployments.
		origin := r.Header.Get("Origin")
		return origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
	},
}

// HandleWebSocket upgrades the connection and starts its read/write pumps.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	c := newClient(conn, h)
	go c.writePump()
	go c.readPump()
}

// route parses a raw frame and dispatches it to the right handler. Only auth
// messages are permitted before a client is authenticated.
func (h *Hub) route(c *Client, data []byte) {
	var msg protocol.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		c.sendError("invalid message format")
		return
	}

	switch msg.Type {
	case protocol.TypeRegister:
		h.handleRegister(c, msg.Payload)
	case protocol.TypeLogin:
		h.handleLogin(c, msg.Payload)
	case protocol.TypeResume:
		h.handleResume(c, msg.Payload)
	default:
		if c.userID == "" {
			c.sendError("not authenticated")
			return
		}
		h.routeAuthed(c, msg)
	}
}

// routeAuthed dispatches messages that require an authenticated client.
func (h *Hub) routeAuthed(c *Client, msg protocol.Message) {
	switch msg.Type {
	case protocol.TypeLogout:
		h.handleLogout(c)
	case protocol.TypeListRooms:
		h.handleListRooms(c)
	case protocol.TypeCreateRoom:
		h.handleCreateRoom(c, msg.Payload)
	case protocol.TypeJoinRoom:
		h.handleJoinRoom(c, msg.Payload)
	case protocol.TypeLeaveRoom:
		h.handleLeaveRoom(c, msg.Payload)
	case protocol.TypeTextMessage:
		h.handleTextMessage(c, msg.Payload)
	case protocol.TypeOffer:
		h.handleOffer(c, msg.Payload)
	case protocol.TypeAnswer:
		h.handleAnswer(c, msg.Payload)
	case protocol.TypeICECandidate:
		h.handleICECandidate(c, msg.Payload)
	case protocol.TypeVideoStopped:
		h.handleVideoStopped(c)
	default:
		c.sendError("unknown message type")
	}
}

// handleDisconnect cleans up a closed connection. The session is intentionally
// left intact so the user can resume after a reload or network blip.
func (h *Hub) handleDisconnect(c *Client) {
	if c.userID == "" {
		return // unauthenticated; nothing to clean up
	}

	h.reg.RemoveClient(c)
	h.leaveAllRooms(c)

	// Guard the narrow Join window where c is added to a room before its own
	// membership set is updated: a disconnect in that gap would leave a ghost.
	for _, r := range h.reg.GhostRooms(c) {
		h.doLeaveRoom(c, r)
	}

	log.Printf("client disconnected: %s (%s)", c.username, c.userID)
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

// mustMsg builds a Message, panicking only on programmer error (types we own).
func mustMsg(t protocol.MessageType, payload interface{}) *protocol.Message {
	m, err := protocol.NewMessage(t, payload)
	if err != nil {
		panic(err)
	}
	return m
}

// mustBytes serialises a typed message to JSON bytes.
func mustBytes(t protocol.MessageType, payload interface{}) []byte {
	b, err := json.Marshal(mustMsg(t, payload))
	if err != nil {
		panic(err)
	}
	return b
}
