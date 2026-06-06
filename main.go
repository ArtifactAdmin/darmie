package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"darmie/internal/hub"
	"darmie/internal/store"
)

func main() {
	addr := flag.String(
		"addr",
		":8080",
		"HTTP listen address")

	dbPath := flag.String(
		"db",
		"darmie.db",
		"SQLite database path for message history")

	uploadsDir := flag.String(
		"uploads",
		"uploads",
		"Directory for uploaded files")

	flag.Parse()

	if err := os.MkdirAll(*uploadsDir, 0o755); err != nil {
		log.Fatalf("create uploads dir: %v", err)
	}

	s := store.New()

	ms, err := store.NewMessageStore(*dbPath)
	if err != nil {
		log.Fatalf("message store: %v", err)
	}

	h := hub.New(s, ms, *uploadsDir)

	// WebSocket signaling endpoint.
	http.HandleFunc("/ws", h.HandleWebSocket)

	// File upload endpoint (authenticated via per-session token).
	http.HandleFunc("/upload", h.HandleUpload)

	// File download endpoint.
	http.HandleFunc("/files/", h.HandleFileDownload)

	// Serve static client files.
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)

	log.Printf("Darmie server listening on %s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
