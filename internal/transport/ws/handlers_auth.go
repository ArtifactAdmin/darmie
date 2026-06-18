package ws

import (
	"encoding/json"

	"darmie/internal/domain"
	"darmie/internal/protocol"
)

func (h *Hub) handleRegister(c *Client, raw json.RawMessage) {
	var p protocol.RegisterPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendAuthError("invalid payload")
		return
	}
	sess, err := h.auth.Register(p.Username, p.Password)
	if err != nil {
		c.sendAuthError(err.Error())
		return
	}
	h.authenticate(c, sess)
}

func (h *Hub) handleLogin(c *Client, raw json.RawMessage) {
	var p protocol.LoginPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendAuthError("invalid payload")
		return
	}
	sess, err := h.auth.Login(p.Username, p.Password)
	if err != nil {
		c.sendAuthError(domain.ErrInvalidCredentials.Error())
		return
	}
	h.authenticate(c, sess)
}

// handleResume re-authenticates a client from a persisted session token,
// avoiding a re-prompt for credentials after a page reload.
func (h *Hub) handleResume(c *Client, raw json.RawMessage) {
	var p protocol.ResumePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		c.sendAuthError("invalid payload")
		return
	}
	sess, err := h.auth.Resume(p.SessionToken)
	if err != nil {
		c.sendAuthError(err.Error())
		return
	}
	h.authenticate(c, sess)
}

// handleLogout invalidates the session so the token can no longer be resumed,
// then lets the client tear down its own connection.
func (h *Hub) handleLogout(c *Client) {
	if err := h.auth.Logout(c.sessionToken); err != nil {
		// Non-fatal: the connection is going away regardless.
		c.sendError("logout failed")
	}
}

// authenticate binds a session to the connection, evicts any prior session for
// the same user, and confirms success to the client.
func (h *Hub) authenticate(c *Client, sess *domain.Session) {
	c.userID = sess.UserID
	c.username = sess.Username
	c.sessionToken = sess.Token

	if old := h.reg.AddClient(c); old != nil {
		go old.close() // kick the previous session outside any lock
	}

	c.sendMsg(mustMsg(protocol.TypeAuthSuccess, protocol.AuthSuccessPayload{
		UserID:       sess.UserID,
		Username:     sess.Username,
		SessionToken: sess.Token,
	}))
}
