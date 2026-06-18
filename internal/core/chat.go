package core

import (
	"log"
	"time"

	"darmie/internal/domain"
	"darmie/internal/port"
)

// ChatService handles posting and replaying room chat history.
type ChatService struct {
	messages   port.MessageRepository
	historyLen int
	now        port.Clock
}

// NewChatService wires a ChatService. historyLen caps how many entries a
// History call returns. If now is nil, wall-clock time is used.
func NewChatService(messages port.MessageRepository, historyLen int, now port.Clock) *ChatService {
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	return &ChatService{messages: messages, historyLen: historyLen, now: now}
}

// Post validates and persists a chat message, returning the stored record so
// the caller can broadcast it. A persistence failure is logged but does not
// fail the call: a delivered message must never be dropped because of a
// best-effort write to history.
func (s *ChatService) Post(roomName, fromUserID, fromUsername, content string) (domain.Message, error) {
	if err := domain.ValidateMessage(content); err != nil {
		return domain.Message{}, err
	}
	m := domain.Message{
		RoomName:     roomName,
		FromUserID:   fromUserID,
		FromUsername: fromUsername,
		Content:      content,
		Timestamp:    s.now(),
	}
	if err := s.messages.SaveText(m); err != nil {
		log.Printf("chat: persist message in %q: %v", roomName, err)
	}
	return m, nil
}

// History returns the recent message + file history for a room.
func (s *ChatService) History(roomName string) ([]domain.HistoryEntry, error) {
	return s.messages.Load(roomName, s.historyLen)
}
