# Darmie

A real-time communication platform with text chat, voice, video, screen sharing, and peer-to-peer file transfer — built with a Go signaling server and a plain HTML/CSS/JS WebRTC client.

Then open **http://localhost:8080** in two or more browser tabs.

## Architecture

The Go server uses a ports-and-adapters layout: `main.go` is the composition
root, `internal/core` holds use cases, `internal/domain` and `internal/port`
define business rules and outbound contracts, and `internal/adapter` provides
SQLite, disk, and security implementations. `internal/transport/ws` is the
driving HTTP/WebSocket adapter and owns ephemeral room presence.

On the browser, `static/js/app.js` is the composition root for session and room
state. `MediaController` owns local media controls, `UploadController` owns
HTTP upload and preview lifecycle, `WebRTCManager` owns peer connections and
signaling, and `WSManager` owns WebSocket transport. Features subscribe to
WebRTC DataChannels through its public registration API rather than reaching
into peer-connection state.
