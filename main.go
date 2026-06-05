package main

import (
	"flag"
	"log"
	"net/http"

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

	flag.Parse()

	s := store.New()

	ms, err := store.NewMessageStore(*dbPath)
	if err != nil {
		log.Fatalf("message store: %v", err)
	}

	h := hub.New(s, ms)

	// WebSocket signaling endpoint.
	http.HandleFunc("/ws", h.HandleWebSocket)

	// Serve static client files.
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)

	log.Printf("Darmie server listening on %s", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
