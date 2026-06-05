# Darmie

A real-time communication platform with text chat, voice, video, screen sharing, and peer-to-peer file transfer — built with a Go signaling server and a plain HTML/CSS/JS WebRTC client.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Browser A                       Browser B                  │
│  ┌──────────────┐                ┌──────────────┐           │
│  │  WebRTC      │◄── P2P ───────►│  WebRTC      │           │
│  │  audio/video │                │  audio/video │           │
│  │  DataChannel │                │  DataChannel │           │
│  └──────┬───────┘                └──────┬───────┘           │
│         │ WebSocket                     │ WebSocket         │
└─────────┼─────────────────────────────┼─────────────────────┘
          │                             │
          ▼                             ▼
   ┌──────────────────────────────────────┐
   │  Go Signaling + HTTP Server          │
   │  • User auth (bcrypt)                │
   │  • Room management                   │
   │  • WebRTC offer/answer/ICE relay     │
   │  • Text message broadcast            │
   └──────────────────────────────────────┘
```

**Topology:** Mesh — each user has a direct RTCPeerConnection to every other user in the room (capped at 12 users per room).

---

## Protocol

Every WebSocket message is a JSON envelope:

```json
{ "type": "<message_type>", "payload": { … } }
```

### Client → Server

| Type | Payload | Description |
|---|---|---|
| `register` | `{username, password}` | Create a new account |
| `login` | `{username, password}` | Authenticate |
| `list_rooms` | `{}` | Request current room list |
| `create_room` | `{name}` | Create a new room |
| `join_room` | `{room_id}` | Join a room (leaves current room first) |
| `leave_room` | `{room_id}` | Leave a room |
| `text_message` | `{room_id, content}` | Broadcast text to room |
| `offer` | `{target_user_id, sdp}` | WebRTC offer (server validates same-room) |
| `answer` | `{target_user_id, sdp}` | WebRTC answer |
| `ice_candidate` | `{target_user_id, candidate}` | ICE candidate |

### Server → Client

| Type | Payload | Description |
|---|---|---|
| `auth_success` | `{user_id, username}` | Auth succeeded |
| `auth_error` | `{message}` | Auth failed |
| `room_list` | `{rooms:[]}` | Current room list |
| `room_created` | `{room}` | Room was created |
| `room_joined` | `{room, users:[]}` | Joined room with member snapshot |
| `room_left` | `{room_id}` | Left room |
| `user_joined` | `{room_id, user}` | Another user joined the room |
| `user_left` | `{room_id, user_id}` | Another user left the room |
| `text_message` | `{room_id, from_user_id, from_username, content, timestamp}` | Incoming chat message |
| `offer` | `{from_user_id, sdp}` | Relayed WebRTC offer |
| `answer` | `{from_user_id, sdp}` | Relayed WebRTC answer |
| `ice_candidate` | `{from_user_id, candidate}` | Relayed ICE candidate |
| `error` | `{message}` | Generic error |

> **Security:** The server populates `from_user_id` from the authenticated WebSocket session — clients cannot spoof this. Offer/answer/ICE relay is rejected unless both users share a room.

---

## WebRTC Flow

```
Existing user (A)            New user (B)          Server
      │                           │                   │
      │       JOIN_ROOM ──────────────────────────────►│
      │                           │                   │
      │◄─────────────────── USER_JOINED(B) ───────────│
      │                           │◄──── ROOM_JOINED([A]) ─│
      │                           │                   │
      │  createPeer(B)            │                   │
      │  createDataChannel        │                   │
      │  (triggers negotiation)   │                   │
      │                           │                   │
      │──── OFFER(B, sdp) ────────────────────────────►│
      │                           │◄──── OFFER(A, sdp) ──│
      │                           │  createPeer(A)    │
      │                           │  setRemoteDesc    │
      │◄───────────── ANSWER(A, sdp) ─────────────────│
      │  setRemoteDesc            │──── ANSWER(B, sdp) ───►│
      │                           │                   │
      │◄══ ICE candidates ══════════════════════════►│
      │                P2P connection established     │
```

**Perfect Negotiation** is used for renegotiation (track add/remove, screen share). Polite/impolite roles are assigned by comparing user-ID strings (lexicographically lower = polite).

### File Transfer (P2P DataChannel)

1. Sender creates a named DataChannel (`file:<uuid>`) on the peer connection.
2. Sends JSON metadata: `{ type:"file_meta", name, size, mimeType }`.
3. Sends file as `ArrayBuffer` chunks of 16 KB with backpressure via `bufferedAmount`.
4. Sends `{ type:"file_end" }`.
5. Receiver reassembles chunks into a `Blob` and offers a download link.

---

## Project Structure

```
darmie/
├── main.go                      Go HTTP + WebSocket server entry point
├── go.mod
├── internal/
│   ├── protocol/protocol.go     Message type constants and payload structs
│   ├── store/store.go           Thread-safe in-memory user store (bcrypt)
│   └── hub/
│       ├── hub.go               Room management + message routing
│       └── client.go            Per-connection WebSocket handler
└── static/
    ├── index.html               Single-page app
    ├── css/style.css
    └── js/
        ├── protocol.js          Message type constants (mirrors Go)
        ├── ws.js                WebSocket manager
        ├── webrtc.js            RTCPeerConnection manager
        ├── filetransfer.js      DataChannel file chunking
        ├── ui.js                All DOM manipulation (XSS-safe)
        └── app.js               Application entry point
```

---

## Running

**Requirements:** Go 1.21+

```bash
# From the darmie/ directory
go run .

# Custom port
go run . -addr :3000

# Build binary
go build -o darmie .
./darmie
```

Then open **http://localhost:8080** in two or more browser tabs.

---

## Design Decisions

| Decision | Rationale |
|---|---|
| WebSocket = session | No JWT needed; connection IS the auth context |
| Mesh topology | Simple for small rooms (≤12 users); no SFU complexity |
| Single room at a time | Simplifies peer connection lifecycle |
| `sync.Once` disconnect | Guarantees cleanup runs exactly once from either pump |
| Slow-client disconnect | Full send buffer → close connection (signaling must not drop) |
| Server-populated sender ID | Prevents identity spoofing in forwarded messages |
| `textContent` everywhere | Prevents XSS from user-generated content |
| Perfect Negotiation | Handles all renegotiation (track add/remove) without glare |

---

## Limitations & Future Work

- **No persistence** — messages and users are in-memory only; restart clears everything.
- **No TURN server** — WebRTC will fail to connect through symmetric NAT without one. Add a TURN server to the `ICE_SERVERS` list in `webrtc.js`.
- **No TLS built-in** — run behind a TLS-terminating reverse proxy (nginx, Caddy) in production. HTTPS is required for browser media APIs.
- **No rate limiting** — add per-IP rate limits for auth endpoints in production.
- **Mesh scales to ~12 users** — larger rooms need an SFU (e.g. mediasoup, Pion).
