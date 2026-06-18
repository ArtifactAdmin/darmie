package ws

import (
	"encoding/json"

	"darmie/internal/protocol"
)

func (h *Hub) handleOffer(c *Client, raw json.RawMessage) {
	var p protocol.OfferPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}
	if target := h.peerClient(c, p.TargetUserID); target != nil {
		target.sendMsg(mustMsg(protocol.TypeOffer, protocol.RelayOfferPayload{
			FromUserID: c.userID,
			SDP:        p.SDP,
		}))
	}
}

func (h *Hub) handleAnswer(c *Client, raw json.RawMessage) {
	var p protocol.AnswerPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}
	if target := h.peerClient(c, p.TargetUserID); target != nil {
		target.sendMsg(mustMsg(protocol.TypeAnswer, protocol.RelayAnswerPayload{
			FromUserID: c.userID,
			SDP:        p.SDP,
		}))
	}
}

func (h *Hub) handleICECandidate(c *Client, raw json.RawMessage) {
	var p protocol.ICECandidatePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendError("invalid payload")
		return
	}
	if target := h.peerClient(c, p.TargetUserID); target != nil {
		target.sendMsg(mustMsg(protocol.TypeICECandidate, protocol.RelayICEPayload{
			FromUserID: c.userID,
			Candidate:  p.Candidate,
		}))
	}
}

// handleVideoStopped relays a video-stopped notice to room peers so they remove
// the sender's tile immediately, without relying on track.ended (which Chrome
// does not fire on removeTrack).
func (h *Hub) handleVideoStopped(c *Client) {
	data := mustBytes(protocol.TypeVideoStopped, protocol.VideoStoppedPayload{UserID: c.userID})
	for _, r := range h.reg.RoomsOf(c) {
		r.broadcast(data, c.userID)
	}
}

// peerClient returns the target client only if it shares a room with c. This is
// the server-side guard that two users cannot signal each other across rooms.
func (h *Hub) peerClient(c *Client, targetUserID string) *Client {
	if targetUserID == c.userID {
		return nil
	}
	target, ok := h.reg.Client(targetUserID)
	if !ok {
		return nil
	}
	for _, r := range h.reg.RoomsOf(c) {
		r.mu.RLock()
		_, shared := r.clients[targetUserID]
		r.mu.RUnlock()
		if shared {
			return target
		}
	}
	return nil
}
