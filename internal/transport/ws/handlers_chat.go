package ws

import (
	"encoding/json"

	"darmie/internal/protocol"
)

func (h *Hub) handleTextMessage(c *Client, raw json.RawMessage) {
	var p protocol.TextMessagePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}

	r, ok := h.reg.Room(p.RoomID)
	if !ok {
		c.sendError("room not found")
		return
	}

	// Verify the sender is actually a member of that room.
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

	msg, err := h.chat.Post(r.name, c.userID, c.username, p.Content)
	if err != nil {
		c.sendError(err.Error())
		return
	}

	data := mustBytes(protocol.TypeTextMessage, protocol.IncomingTextPayload{
		RoomID:       p.RoomID,
		FromUserID:   msg.FromUserID,
		FromUsername: msg.FromUsername,
		Content:      msg.Content,
		Timestamp:    msg.Timestamp,
	})
	r.broadcast(data, "") // include sender so they see their own message
}
