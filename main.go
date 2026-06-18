// Command darmie is the composition root: it constructs the outbound adapters,
// wires them into the core services, and exposes those services through the
// WebSocket + HTTP driving adapter. This is the only place concrete types are
// assembled — every other package depends on interfaces, not implementations.
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"

	"darmie/internal/adapter/diskstore"
	"darmie/internal/adapter/security"
	"darmie/internal/adapter/sqlite"
	"darmie/internal/core"
	"darmie/internal/transport/ws"
)

const (
	maxRoomSize    = 12        // cap for mesh-topology rooms
	maxRooms       = 100       // prevent unbounded room growth
	maxHistory     = 200       // messages kept/replayed per room
	maxUploadBytes = 100 << 20 // 100 MB per file
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "darmie.db", "SQLite database path")
	uploadsDir := flag.String("uploads", "uploads", "Directory for uploaded files")
	flag.Parse()

	// ── Outbound adapters (driven side) ──────────────────────────────────────
	db, err := sqlite.Open(*dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	storage, err := diskstore.New(*uploadsDir)
	if err != nil {
		log.Fatalf("init file storage: %v", err)
	}

	hasher, err := security.NewBcryptHasher()
	if err != nil {
		log.Fatalf("init password hasher: %v", err)
	}
	tokens := security.NewTokenGenerator(32)

	// ── Core services (use-case layer) ───────────────────────────────────────
	auth := core.NewAuthService(db.Users(), db.Sessions(), hasher, tokens, nil)
	chat := core.NewChatService(db.Messages(maxHistory), maxHistory, nil)
	files := core.NewFileService(db.Files(), storage, nil)

	// ── Driving adapter (WebSocket + file HTTP) ──────────────────────────────
	hub := ws.New(ws.Config{
		Auth:           auth,
		Chat:           chat,
		Files:          files,
		MaxRooms:       maxRooms,
		MaxRoomSize:    maxRoomSize,
		MaxUploadBytes: maxUploadBytes,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleWebSocket)
	mux.HandleFunc("/upload", hub.HandleUpload)
	mux.HandleFunc("/files/", hub.HandleFileDownload)
	mux.Handle("/", noCacheHTML(http.FileServer(http.Dir("static"))))

	log.Printf("Darmie server listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// noCacheHTML tells browsers to always revalidate the HTML entry document so a
// new deployment's asset versions take effect on reload. Static assets are
// cache-busted by their own `?v=N` query, so they are left untouched.
func noCacheHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; p == "/" || strings.HasSuffix(p, ".html") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}
