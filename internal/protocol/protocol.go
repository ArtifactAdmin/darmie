// Package protocol defines the WebSocket signaling protocol for Darmie.
// Every message is a JSON object: {"type": "<MessageType>", "payload": {...}}.
package protocol

import "encoding/json"

// MessageType identifies the kind of a protocol message.
type MessageType string

const (
	// Client
	TypeRegister     MessageType = "register"
	TypeLogin        MessageType = "login"
	TypeResume       MessageType = "resume" // resume a persisted session by token
	TypeLogout       MessageType = "logout" // invalidate the current session
	TypeListRooms    MessageType = "list_rooms"
	TypeCreateRoom   MessageType = "create_room"
	TypeJoinRoom     MessageType = "join_room"
	TypeLeaveRoom    MessageType = "leave_room"
	TypeTextMessage  MessageType = "text_message"
	TypeOffer        MessageType = "offer"
	TypeAnswer       MessageType = "answer"
	TypeICECandidate MessageType = "ice_candidate"
	TypeVideoStopped MessageType = "video_stopped" // (relayed to room)

	// Server
	TypeFileMessage MessageType = "file_message"
	TypeAuthSuccess MessageType = "auth_success"
	TypeAuthError   MessageType = "auth_error"
	TypeRoomList    MessageType = "room_list"
	TypeRoomJoined  MessageType = "room_joined"
	TypeRoomLeft    MessageType = "room_left"
	TypeRoomCreated MessageType = "room_created"
	TypeUserJoined  MessageType = "user_joined"
	TypeUserLeft    MessageType = "user_left"
	TypeError       MessageType = "error"
)

// Message is the envelope for all protocol messages.
type Message struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// NewMessage serialises payload and wraps it in a Message.
func NewMessage(t MessageType, payload interface{}) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{Type: t, Payload: data}, nil
}

// Payload types

type RegisterPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ResumePayload carries a persisted session token for silent re-authentication
// after a page reload or dropped connection.
type ResumePayload struct {
	SessionToken string `json:"session_token"`
}

type CreateRoomPayload struct {
	Name string `json:"name"`
}

type JoinRoomPayload struct {
	RoomID string `json:"room_id"`
}

type LeaveRoomPayload struct {
	RoomID string `json:"room_id"`
}

type TextMessagePayload struct {
	RoomID  string `json:"room_id"`
	Content string `json:"content"`
}

// OfferPayload carries a WebRTC offer SDP from the sender to a target peer.
type OfferPayload struct {
	TargetUserID string          `json:"target_user_id"`
	SDP          json.RawMessage `json:"sdp"`
}

// AnswerPayload carries a WebRTC answer SDP from the sender to a target peer.
type AnswerPayload struct {
	TargetUserID string          `json:"target_user_id"`
	SDP          json.RawMessage `json:"sdp"`
}

// ICECandidatePayload carries an ICE candidate from the sender to a target peer.
type ICECandidatePayload struct {
	TargetUserID string          `json:"target_user_id"`
	Candidate    json.RawMessage `json:"candidate"`
}

// Server payload types

// UserInfo is a minimal user representation sent to clients.
type UserInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// RoomInfo is a minimal room representation sent to clients.
type RoomInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UserCount int    `json:"user_count"`
}

type AuthSuccessPayload struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	// SessionToken doubles as the credential for HTTP file uploads and as the
	// token a client persists to resume its session after a reload.
	SessionToken string `json:"session_token"`
}

type AuthErrorPayload struct {
	Message string `json:"message"`
}

type RoomListPayload struct {
	Rooms []RoomInfo `json:"rooms"`
}

// HistoryEntry is a unified record for the room history replay, covering both
// text messages (Kind == "text") and uploaded files (Kind == "file").
type HistoryEntry struct {
	Kind         string `json:"kind"` // "text" or "file"
	FromUserID   string `json:"from_user_id"`
	FromUsername string `json:"from_username"`
	Timestamp    int64  `json:"timestamp"`
	// text-only
	Content string `json:"content,omitempty"`
	// file-only
	FileID   string `json:"file_id,omitempty"`
	Filename string `json:"filename,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
	URL      string `json:"url,omitempty"`
}

// FileMessagePayload is broadcast to all room members when a file is uploaded.
type FileMessagePayload struct {
	RoomID       string `json:"room_id"`
	FromUserID   string `json:"from_user_id"`
	FromUsername string `json:"from_username"`
	FileID       string `json:"file_id"`
	Filename     string `json:"filename"`
	MimeType     string `json:"mime_type"`
	Size         int64  `json:"size"`
	URL          string `json:"url"`
	Timestamp    int64  `json:"timestamp"`
}

type RoomJoinedPayload struct {
	Room    RoomInfo       `json:"room"`
	Users   []UserInfo     `json:"users"`
	History []HistoryEntry `json:"history"`
}

type RoomCreatedPayload struct {
	Room RoomInfo `json:"room"`
}

type UserJoinedPayload struct {
	RoomID string   `json:"room_id"`
	User   UserInfo `json:"user"`
}

type UserLeftPayload struct {
	RoomID string `json:"room_id"`
	UserID string `json:"user_id"`
}

type IncomingTextPayload struct {
	RoomID       string `json:"room_id"`
	FromUserID   string `json:"from_user_id"`
	FromUsername string `json:"from_username"`
	Content      string `json:"content"`
	Timestamp    int64  `json:"timestamp"` // Unix milliseconds
}

// RelayOfferPayload is sent to the target peer; from_user_id is server-populated.
type RelayOfferPayload struct {
	FromUserID string          `json:"from_user_id"`
	SDP        json.RawMessage `json:"sdp"`
}

// RelayAnswerPayload is sent to the offering peer; from_user_id is server-populated.
type RelayAnswerPayload struct {
	FromUserID string          `json:"from_user_id"`
	SDP        json.RawMessage `json:"sdp"`
}

// RelayICEPayload is sent to the target peer; from_user_id is server-populated.
type RelayICEPayload struct {
	FromUserID string          `json:"from_user_id"`
	Candidate  json.RawMessage `json:"candidate"`
}

// VideoStoppedPayload is broadcast to room peers when a user stops all video.
type VideoStoppedPayload struct {
	UserID string `json:"user_id"`
}

type ErrorPayload struct {
	Message string `json:"message"`
}
