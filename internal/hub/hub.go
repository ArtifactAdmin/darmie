// Package hub manages WebSocket clients, rooms, and message routing.
package hub

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"darmie/internal/protocol"
	"darmie/internal/store"
)

const (
	maxRoomSize    = 12        // cap for mesh-topology rooms
	maxRooms       = 100       // prevent unbounded room growth
	maxMsgLen      = 2000      // max text chat content length
	maxNameLen     = 50        // max room name length
	maxHistory     = 200       // max messages kept per room
	maxUploadBytes = 100 << 20 // 100 MB per file
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// Restrict to same-origin. Adjust as needed for production deployments.
		origin := r.Header.Get("Origin")
		host := "http://" + r.Host
		hostTLS := "https://" + r.Host
		return origin == "" || origin == host || origin == hostTLS
	},
}

// ─── Room ─────────────────────────────────────────────────────────────────────

type room struct {
	id      string
	name    string
	mu      sync.RWMutex
	clients map[string]*Client // userID → Client
}

func newRoom(name string) *room {
	return &room{
		id:      uuid.New().String(),
		name:    name,
		clients: make(map[string]*Client),
	}
}

// snapshot returns a copy of the current client list while holding the lock.
func (r *room) snapshot() []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Client, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, c)
	}
	return out
}

// broadcast sends data to every client except exclude, disconnecting laggards.
func (r *room) broadcast(data []byte, exclude string) {
	for _, c := range r.snapshot() {
		if c.userID == exclude {
			continue
		}
		c.trySend(data)
	}
}

// ─── Hub ──────────────────────────────────────────────────────────────────────

// Hub is the central coordinator for all connections and rooms.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client // userID → Client (authenticated only)
	rooms   map[string]*room   // roomID → room
	store   *store.Store
	msgs    *store.MessageStore

	uploadTokensMu sync.RWMutex
	uploadTokens   map[string]string // token → userID
	uploadsDir     string
}

// New creates a Hub backed by the given user store and message store.
func New(s *store.Store, ms *store.MessageStore, uploadsDir string) *Hub {
	return &Hub{
		clients:      make(map[string]*Client),
		rooms:        make(map[string]*room),
		store:        s,
		msgs:         ms,
		uploadTokens: make(map[string]string),
		uploadsDir:   uploadsDir,
	}
}

// HandleWebSocket upgrades an HTTP connection to WebSocket and starts the client pumps.
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

// ─── Routing ──────────────────────────────────────────────────────────────────

// route parses a raw WebSocket message and dispatches to the correct handler.
func (h *Hub) route(c *Client, data []byte) {
	var msg protocol.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		c.sendError("invalid message format")
		return
	}

	// Unauthenticated clients may only register or login.
	if msg.Type != protocol.TypeRegister && msg.Type != protocol.TypeLogin {
		if c.userID == "" {
			c.sendError("not authenticated")
			return
		}
	}

	switch msg.Type {
	case protocol.TypeRegister:
		h.handleRegister(c, msg.Payload)
	case protocol.TypeLogin:
		h.handleLogin(c, msg.Payload)
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

// ─── Auth handlers ────────────────────────────────────────────────────────────

func (h *Hub) handleRegister(c *Client, raw json.RawMessage) {
	var p protocol.RegisterPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendAuthError("invalid payload")
		return
	}

	u, err := h.store.Register(p.Username, p.Password)
	if err != nil {
		c.sendAuthError(err.Error())
		return
	}

	token := h.attachClient(c, u.ID, u.Username)
	c.sendMsg(mustMsg(protocol.TypeAuthSuccess, protocol.AuthSuccessPayload{
		UserID:      u.ID,
		Username:    u.Username,
		UploadToken: token,
	}))
}

func (h *Hub) handleLogin(c *Client, raw json.RawMessage) {
	var p protocol.LoginPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendAuthError("invalid payload")
		return
	}

	u, err := h.store.Login(p.Username, p.Password)
	if err != nil {
		c.sendAuthError("invalid username or password")
		return
	}

	// Set identity fields before taking the lock so the client is immediately
	// identifiable if close() races with another goroutine.
	c.userID = u.ID
	c.username = u.Username

	// Atomically evict any existing session and register this client.
	// Doing both under one lock acquisition closes the TOCTOU race where a
	// concurrent login goroutine could overwrite us between the evict and register.
	h.mu.Lock()
	old := h.clients[u.ID]
	h.clients[u.ID] = c
	h.mu.Unlock()

	if old != nil && old != c {
		go old.close() // kick the old session outside the lock to avoid deadlock
	}

	token := h.generateUploadToken(c, u.ID)
	c.sendMsg(mustMsg(protocol.TypeAuthSuccess, protocol.AuthSuccessPayload{
		UserID:      u.ID,
		Username:    u.Username,
		UploadToken: token,
	}))
}

// attachClient registers an authenticated client with the hub and returns a
// fresh upload token for use in HTTP file uploads.
func (h *Hub) attachClient(c *Client, userID, username string) string {
	c.userID = userID
	c.username = username

	h.mu.Lock()
	h.clients[userID] = c
	h.mu.Unlock()

	return h.generateUploadToken(c, userID)
}

// generateUploadToken creates a cryptographically-random token, stores it, and
// records it on the client so it can be revoked on disconnect.
func (h *Hub) generateUploadToken(c *Client, userID string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Printf("generateUploadToken: %v", err)
		return ""
	}
	token := base64.RawURLEncoding.EncodeToString(b)

	// Revoke any previous token for this client.
	h.uploadTokensMu.Lock()
	if c.uploadToken != "" {
		delete(h.uploadTokens, c.uploadToken)
	}
	c.uploadToken = token
	h.uploadTokens[token] = userID
	h.uploadTokensMu.Unlock()

	return token
}

// ─── Room handlers ────────────────────────────────────────────────────────────

func (h *Hub) handleListRooms(c *Client) {
	h.mu.RLock()
	infos := make([]protocol.RoomInfo, 0, len(h.rooms))
	for _, r := range h.rooms {
		r.mu.RLock()
		infos = append(infos, protocol.RoomInfo{
			ID:        r.id,
			Name:      r.name,
			UserCount: len(r.clients),
		})
		r.mu.RUnlock()
	}
	h.mu.RUnlock()

	c.sendMsg(mustMsg(protocol.TypeRoomList, protocol.RoomListPayload{Rooms: infos}))
}

func (h *Hub) handleCreateRoom(c *Client, raw json.RawMessage) {
	var p protocol.CreateRoomPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || utf8.RuneCountInString(p.Name) > maxNameLen {
		c.sendError("room name must be 1–50 characters")
		return
	}

	h.mu.Lock()
	if len(h.rooms) >= maxRooms {
		h.mu.Unlock()
		c.sendError("maximum number of rooms reached")
		return
	}
	for _, existing := range h.rooms {
		if strings.EqualFold(existing.name, p.Name) {
			h.mu.Unlock()
			c.sendError("a room with that name already exists")
			return
		}
	}
	r := newRoom(p.Name)
	h.rooms[r.id] = r
	h.mu.Unlock()

	c.sendMsg(mustMsg(protocol.TypeRoomCreated, protocol.RoomCreatedPayload{
		Room: protocol.RoomInfo{ID: r.id, Name: r.name, UserCount: 0},
	}))
}

func (h *Hub) handleJoinRoom(c *Client, raw json.RawMessage) {
	var p protocol.JoinRoomPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}

	// Leave all current rooms before joining a new one (single-room invariant).
	h.leaveAllRooms(c)

	h.mu.RLock()
	r, exists := h.rooms[p.RoomID]
	h.mu.RUnlock()
	if !exists {
		c.sendError("room not found")
		return
	}

	// Atomically read existing members and add the new client.
	r.mu.Lock()
	if len(r.clients) >= maxRoomSize {
		r.mu.Unlock()
		c.sendError("room is full")
		return
	}
	existingUsers := make([]protocol.UserInfo, 0, len(r.clients))
	existingClients := make([]*Client, 0, len(r.clients))
	for _, ec := range r.clients {
		existingUsers = append(existingUsers, protocol.UserInfo{ID: ec.userID, Username: ec.username})
		existingClients = append(existingClients, ec)
	}
	r.clients[c.userID] = c
	r.mu.Unlock()

	// Track membership on the client.
	c.roomsMu.Lock()
	c.rooms[r.id] = struct{}{}
	c.roomsMu.Unlock()

	// Load message history from the database (done outside the room lock).
	history, err := h.msgs.Load(r.name, maxHistory)
	if err != nil {
		log.Printf("handleJoinRoom: load history for %q: %v", r.name, err)
	}

	// Tell the joiner who is already here, plus message history.
	c.sendMsg(mustMsg(protocol.TypeRoomJoined, protocol.RoomJoinedPayload{
		Room:    protocol.RoomInfo{ID: r.id, Name: r.name, UserCount: len(existingUsers) + 1},
		Users:   existingUsers,
		History: history,
	}))

	// Notify existing members of the new arrival.
	userJoined := mustBytes(protocol.TypeUserJoined, protocol.UserJoinedPayload{
		RoomID: r.id,
		User:   protocol.UserInfo{ID: c.userID, Username: c.username},
	})
	for _, ec := range existingClients {
		ec.trySend(userJoined)
	}
}

func (h *Hub) handleLeaveRoom(c *Client, raw json.RawMessage) {
	var p protocol.LeaveRoomPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}

	h.mu.RLock()
	r, exists := h.rooms[p.RoomID]
	h.mu.RUnlock()
	if !exists {
		return
	}

	if removed := h.doLeaveRoom(c, r); removed {
		c.sendMsg(mustMsg(protocol.TypeRoomLeft, map[string]string{"room_id": p.RoomID}))
	}
}

// leaveAllRooms removes c from every room it is currently in.
func (h *Hub) leaveAllRooms(c *Client) {
	c.roomsMu.RLock()
	roomIDs := make([]string, 0, len(c.rooms))
	for id := range c.rooms {
		roomIDs = append(roomIDs, id)
	}
	c.roomsMu.RUnlock()

	h.mu.RLock()
	for _, id := range roomIDs {
		if r, ok := h.rooms[id]; ok {
			h.mu.RUnlock()
			h.doLeaveRoom(c, r)
			h.mu.RLock()
		}
	}
	h.mu.RUnlock()
}

// doLeaveRoom removes a client from a room and broadcasts user_left.
// Returns true if the client was a member and was actually removed.
func (h *Hub) doLeaveRoom(c *Client, r *room) bool {
	r.mu.Lock()
	if _, member := r.clients[c.userID]; !member {
		r.mu.Unlock()
		return false
	}
	delete(r.clients, c.userID)
	remaining := make([]*Client, 0, len(r.clients))
	for _, rc := range r.clients {
		remaining = append(remaining, rc)
	}
	r.mu.Unlock()

	c.roomsMu.Lock()
	delete(c.rooms, r.id)
	c.roomsMu.Unlock()

	data := mustBytes(protocol.TypeUserLeft, protocol.UserLeftPayload{
		RoomID: r.id,
		UserID: c.userID,
	})
	for _, rc := range remaining {
		rc.trySend(data)
	}
	return true
}

// ─── Chat handler ─────────────────────────────────────────────────────────────

func (h *Hub) handleTextMessage(c *Client, raw json.RawMessage) {
	var p protocol.TextMessagePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}
	if utf8.RuneCountInString(p.Content) > maxMsgLen {
		c.sendError("message too long")
		return
	}

	h.mu.RLock()
	r, exists := h.rooms[p.RoomID]
	h.mu.RUnlock()
	if !exists {
		c.sendError("room not found")
		return
	}

	// Verify sender is actually in that room.
	r.mu.RLock()
	_, member := r.clients[c.userID]
	r.mu.RUnlock()
	if !member {
		c.sendError("you are not in that room")
		return
	}

	// Silently drop messages that exceed the per-client rate limit.
	if !c.allowMessage() {
		return
	}

	entry := protocol.IncomingTextPayload{
		RoomID:       p.RoomID,
		FromUserID:   c.userID,
		FromUsername: c.username,
		Content:      p.Content,
		Timestamp:    time.Now().UnixMilli(),
	}

	h.msgs.SaveText(r.name, entry)

	data := mustBytes(protocol.TypeTextMessage, entry)
	r.broadcast(data, "") // include sender so they see their own message
}

// ─── WebRTC relay handlers ────────────────────────────────────────────────────

func (h *Hub) handleOffer(c *Client, raw json.RawMessage) {
	var p protocol.OfferPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}
	target := h.peerClient(c, p.TargetUserID)
	if target == nil {
		return
	}
	target.sendMsg(mustMsg(protocol.TypeOffer, protocol.RelayOfferPayload{
		FromUserID: c.userID,
		SDP:        p.SDP,
	}))
}

func (h *Hub) handleAnswer(c *Client, raw json.RawMessage) {
	var p protocol.AnswerPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}
	target := h.peerClient(c, p.TargetUserID)
	if target == nil {
		return
	}
	target.sendMsg(mustMsg(protocol.TypeAnswer, protocol.RelayAnswerPayload{
		FromUserID: c.userID,
		SDP:        p.SDP,
	}))
}

func (h *Hub) handleICECandidate(c *Client, raw json.RawMessage) {
	var p protocol.ICECandidatePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}
	target := h.peerClient(c, p.TargetUserID)
	if target == nil {
		return
	}
	target.sendMsg(mustMsg(protocol.TypeICECandidate, protocol.RelayICEPayload{
		FromUserID: c.userID,
		Candidate:  p.Candidate,
	}))
}

// handleVideoStopped relays a video-stopped notification to all room peers so
// they can remove the sender's video tile immediately, without relying on the
// browser firing track.ended (which Chrome does not do on removeTrack).
func (h *Hub) handleVideoStopped(c *Client) {
	c.roomsMu.RLock()
	roomIDs := make([]string, 0, len(c.rooms))
	for id := range c.rooms {
		roomIDs = append(roomIDs, id)
	}
	c.roomsMu.RUnlock()

	data := mustBytes(protocol.TypeVideoStopped, protocol.VideoStoppedPayload{
		UserID: c.userID,
	})

	h.mu.RLock()
	for _, id := range roomIDs {
		if r, ok := h.rooms[id]; ok {
			r.broadcast(data, c.userID) // exclude the sender
		}
	}
	h.mu.RUnlock()
}

// peerClient returns the *Client for targetUserID only if they share a room with c.
func (h *Hub) peerClient(c *Client, targetUserID string) *Client {
	if targetUserID == c.userID {
		return nil
	}

	c.roomsMu.RLock()
	roomIDs := make([]string, 0, len(c.rooms))
	for id := range c.rooms {
		roomIDs = append(roomIDs, id)
	}
	c.roomsMu.RUnlock()

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, rid := range roomIDs {
		r, ok := h.rooms[rid]
		if !ok {
			continue
		}
		r.mu.RLock()
		_, targetInRoom := r.clients[targetUserID]
		target := h.clients[targetUserID]
		r.mu.RUnlock()
		if targetInRoom && target != nil {
			return target
		}
	}
	return nil
}

// ─── Disconnect ───────────────────────────────────────────────────────────────

// handleDisconnect cleans up a client that has closed its connection.
func (h *Hub) handleDisconnect(c *Client) {
	if c.userID == "" {
		return // unauthenticated client, nothing to clean up
	}

	// Revoke the upload token so in-flight requests fail gracefully.
	if c.uploadToken != "" {
		h.uploadTokensMu.Lock()
		delete(h.uploadTokens, c.uploadToken)
		h.uploadTokensMu.Unlock()
	}

	h.mu.Lock()
	// Only remove if this is still the registered client for this user ID.
	if h.clients[c.userID] == c {
		delete(h.clients, c.userID)
	}
	h.mu.Unlock()

	h.leaveAllRooms(c)

	// Paranoia scan: guard against the narrow race in handleJoinRoom where c
	// is added to r.clients before c.rooms is updated. If disconnect fires in
	// that gap, leaveAllRooms misses the room and c remains a ghost member.
	// Scanning all rooms here (bounded by maxRooms = 100) is cheap and safe.
	h.mu.RLock()
	var ghosts []*room
	for _, r := range h.rooms {
		r.mu.RLock()
		if _, ok := r.clients[c.userID]; ok {
			ghosts = append(ghosts, r)
		}
		r.mu.RUnlock()
	}
	h.mu.RUnlock()
	for _, r := range ghosts {
		h.doLeaveRoom(c, r)
	}

	log.Printf("client disconnected: %s (%s)", c.username, c.userID)
}

// ─── File upload / download ───────────────────────────────────────────────────

// HandleUpload accepts a multipart POST, saves the file to disk, records
// metadata in the database, and broadcasts a file_message to the room.
//
//	POST /upload?token=<uploadToken>&room_id=<roomID>
//	Body: multipart/form-data, field name "file"
func (h *Hub) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Authenticate via upload token.
	token := r.URL.Query().Get("token")
	roomID := r.URL.Query().Get("room_id")

	h.uploadTokensMu.RLock()
	userID, ok := h.uploadTokens[token]
	h.uploadTokensMu.RUnlock()
	if !ok || token == "" {
		jsonErr(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	h.mu.RLock()
	c, clientOk := h.clients[userID]
	rm, roomOk := h.rooms[roomID]
	h.mu.RUnlock()
	if !clientOk {
		jsonErr(w, "not connected", http.StatusUnauthorized)
		return
	}
	if !roomOk {
		jsonErr(w, "room not found", http.StatusBadRequest)
		return
	}

	rm.mu.RLock()
	_, member := rm.clients[userID]
	rm.mu.RUnlock()
	if !member {
		jsonErr(w, "you are not in that room", http.StatusForbidden)
		return
	}

	// Cap the body size before parsing to avoid reading a huge payload first.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+4096)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		jsonErr(w, "file too large or malformed form (max 100 MB)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, "missing 'file' field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Sanitize the original filename: keep only the base, replace control chars.
	origName := sanitizeFilename(filepath.Base(header.Filename))
	if origName == "" || origName == "." {
		origName = "file"
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Write to disk.
	fileID := uuid.New().String()
	diskPath := filepath.Join(h.uploadsDir, fileID)
	out, err := os.Create(diskPath)
	if err != nil {
		log.Printf("upload: create %s: %v", diskPath, err)
		jsonErr(w, "server error", http.StatusInternalServerError)
		return
	}

	size, copyErr := io.Copy(out, file)
	out.Close()
	if copyErr != nil {
		log.Printf("upload: write %s: %v", diskPath, copyErr)
		os.Remove(diskPath)
		jsonErr(w, "server error", http.StatusInternalServerError)
		return
	}

	// Persist metadata.
	ts := time.Now().UnixMilli()
	rec := store.FileRecord{
		ID:           fileID,
		RoomName:     rm.name,
		FromUserID:   userID,
		FromUsername: c.username,
		Filename:     origName,
		MimeType:     mimeType,
		Size:         size,
		Timestamp:    ts,
	}
	if err := h.msgs.SaveFile(rec); err != nil {
		log.Printf("upload: save metadata: %v", err)
		os.Remove(diskPath)
		jsonErr(w, "server error", http.StatusInternalServerError)
		return
	}

	// Broadcast to the whole room (including the uploader).
	fileURL := "/files/" + fileID
	payload := protocol.FileMessagePayload{
		RoomID:       roomID,
		FromUserID:   userID,
		FromUsername: c.username,
		FileID:       fileID,
		Filename:     origName,
		MimeType:     mimeType,
		Size:         size,
		URL:          fileURL,
		Timestamp:    ts,
	}
	rm.broadcast(mustBytes(protocol.TypeFileMessage, payload), "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": fileURL})
}

// HandleFileDownload serves an uploaded file by its UUID.
//
//	GET /files/<fileID>
func (h *Hub) HandleFileDownload(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/files/")
	// Reject empty, multi-segment, or path-traversal attempts.
	if fileID == "" || strings.ContainsAny(fileID, "/\\..") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	rec, err := h.msgs.GetFile(fileID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	diskPath := filepath.Join(h.uploadsDir, fileID)
	f, err := os.Open(diskPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", rec.MimeType)
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+sanitizeFilename(rec.Filename)+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(rec.Size, 10))
	io.Copy(w, f) //nolint:errcheck
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// mustMsg creates a Message and panics on error (only called with types we control).
func mustMsg(t protocol.MessageType, payload interface{}) *protocol.Message {
	m, err := protocol.NewMessage(t, payload)
	if err != nil {
		panic(err)
	}
	return m
}

// mustBytes serialises a typed message to JSON bytes.
func mustBytes(t protocol.MessageType, payload interface{}) []byte {
	m := mustMsg(t, payload)
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

// jsonErr writes a JSON {"error":"..."} response.
func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}

// sanitizeFilename strips characters that are unsafe in HTTP headers or file paths.
func sanitizeFilename(name string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '"', '\\', '\r', '\n', '/', '\x00':
			return '_'
		}
		return r
	}, name)
}
