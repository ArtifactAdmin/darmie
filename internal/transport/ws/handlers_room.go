package ws

import (
	"encoding/json"
	"log"

	"darmie/internal/domain"
	"darmie/internal/protocol"
)

func (h *Hub) handleListRooms(c *Client) {
	c.sendMsg(mustMsg(protocol.TypeRoomList, protocol.RoomListPayload{Rooms: h.reg.ListRooms()}))
}

func (h *Hub) handleCreateRoom(c *Client, raw json.RawMessage) {
	var p protocol.CreateRoomPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}
	r, err := h.reg.CreateRoom(p.Name)
	if err != nil {
		c.sendError(err.Error())
		return
	}

	c.sendMsg(mustMsg(protocol.TypeRoomCreated, protocol.RoomCreatedPayload{
		Room: protocol.RoomInfo{ID: r.id, Name: r.name, UserCount: 0},
	}))
	// Push the refreshed list to everyone else so the new room appears live.
	h.broadcastRoomList(c.userID)
}

func (h *Hub) handleJoinRoom(c *Client, raw json.RawMessage) {
	var p protocol.JoinRoomPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}

	// Single-room invariant: leave every current room before joining.
	h.leaveAllRooms(c)

	r, existing, err := h.reg.Join(c, p.RoomID)
	if err != nil {
		c.sendError(err.Error())
		return
	}

	existingUsers := make([]protocol.UserInfo, 0, len(existing))
	for _, ec := range existing {
		existingUsers = append(existingUsers, protocol.UserInfo{ID: ec.userID, Username: ec.username})
	}

	// Load history outside any presence lock.
	history := h.loadHistory(r.name)

	c.sendMsg(mustMsg(protocol.TypeRoomJoined, protocol.RoomJoinedPayload{
		Room:    protocol.RoomInfo{ID: r.id, Name: r.name, UserCount: len(existingUsers) + 1},
		Users:   existingUsers,
		History: history,
	}))

	userJoined := mustBytes(protocol.TypeUserJoined, protocol.UserJoinedPayload{
		RoomID: r.id,
		User:   protocol.UserInfo{ID: c.userID, Username: c.username},
	})
	for _, ec := range existing {
		ec.trySend(userJoined)
	}
}

func (h *Hub) handleLeaveRoom(c *Client, raw json.RawMessage) {
	var p protocol.LeaveRoomPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}
	r, ok := h.reg.Room(p.RoomID)
	if !ok {
		return
	}
	if h.doLeaveRoom(c, r) {
		c.sendMsg(mustMsg(protocol.TypeRoomLeft, map[string]string{"room_id": p.RoomID}))
	}
}

// leaveAllRooms removes c from every room it currently belongs to.
func (h *Hub) leaveAllRooms(c *Client) {
	for _, r := range h.reg.RoomsOf(c) {
		h.doLeaveRoom(c, r)
	}
}

// doLeaveRoom removes c from r and broadcasts user_left. Returns true if c was
// a member that was removed.
func (h *Hub) doLeaveRoom(c *Client, r *room) bool {
	remaining, removed := h.reg.Leave(c, r)
	if !removed {
		return false
	}
	data := mustBytes(protocol.TypeUserLeft, protocol.UserLeftPayload{RoomID: r.id, UserID: c.userID})
	for _, rc := range remaining {
		rc.trySend(data)
	}
	return true
}

// broadcastRoomList pushes the current room list to every authenticated client
// except exclude.
func (h *Hub) broadcastRoomList(exclude string) {
	data := mustBytes(protocol.TypeRoomList, protocol.RoomListPayload{Rooms: h.reg.ListRooms()})
	for _, c := range h.reg.AuthedClientsExcept(exclude) {
		c.trySend(data)
	}
}

// loadHistory fetches a room's persisted history and maps it to wire DTOs.
func (h *Hub) loadHistory(roomName string) []protocol.HistoryEntry {
	entries, err := h.chat.History(roomName)
	if err != nil {
		log.Printf("join: load history for %q: %v", roomName, err)
		return nil
	}
	out := make([]protocol.HistoryEntry, 0, len(entries))
	for _, e := range entries {
		he := protocol.HistoryEntry{
			Kind:         e.Kind,
			FromUserID:   e.FromUserID,
			FromUsername: e.FromUsername,
			Timestamp:    e.Timestamp,
			Content:      e.Content,
			FileID:       e.FileID,
			Filename:     e.Filename,
			MimeType:     e.MimeType,
			Size:         e.Size,
		}
		if e.Kind == domain.HistoryFile {
			he.URL = "/files/" + e.FileID
		}
		out = append(out, he)
	}
	return out
}
