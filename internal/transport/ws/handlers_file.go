package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"darmie/internal/domain"
	"darmie/internal/protocol"
)

// HandleUpload accepts a multipart POST, stores the file, records metadata, and
// broadcasts a file_message to the room.
//
//	POST /upload?token=<sessionToken>&room_id=<roomID>
//	Body: multipart/form-data, field "file"
func (h *Hub) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Authenticate via the session token, then require the user to be connected
	// and a member of the target room.
	sess, err := h.auth.Resume(r.URL.Query().Get("token"))
	if err != nil {
		jsonErr(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	roomID := r.URL.Query().Get("room_id")

	if _, connected := h.reg.Client(sess.UserID); !connected {
		jsonErr(w, "not connected", http.StatusUnauthorized)
		return
	}
	rm, ok := h.reg.Room(roomID)
	if !ok {
		jsonErr(w, "room not found", http.StatusBadRequest)
		return
	}
	rm.mu.RLock()
	_, member := rm.clients[sess.UserID]
	rm.mu.RUnlock()
	if !member {
		jsonErr(w, "you are not in that room", http.StatusForbidden)
		return
	}

	// Cap the body before parsing to avoid buffering an oversized payload.
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes+4096)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		jsonErr(w, "file too large or malformed form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, "missing 'file' field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	origName := sanitizeFilename(filepath.Base(header.Filename))
	if origName == "" || origName == "." {
		origName = "file"
	}
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	rec, err := h.files.Save(domain.File{
		ID:           uuid.NewString(),
		RoomName:     rm.name,
		FromUserID:   sess.UserID,
		FromUsername: sess.Username,
		Filename:     origName,
		MimeType:     mimeType,
	}, file)
	if err != nil {
		log.Printf("upload: %v", err)
		jsonErr(w, "server error", http.StatusInternalServerError)
		return
	}

	fileURL := "/files/" + rec.ID
	rm.broadcast(mustBytes(protocol.TypeFileMessage, protocol.FileMessagePayload{
		RoomID:       roomID,
		FromUserID:   rec.FromUserID,
		FromUsername: rec.FromUsername,
		FileID:       rec.ID,
		Filename:     rec.Filename,
		MimeType:     rec.MimeType,
		Size:         rec.Size,
		URL:          fileURL,
		Timestamp:    rec.Timestamp,
	}), "")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"url": fileURL})
}

// HandleFileDownload serves an uploaded file by its ID, with full support for
// HTTP range requests so browsers can seek through audio and video files.
//
//	GET /files/<fileID>
func (h *Hub) HandleFileDownload(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/files/")
	if fileID == "" || strings.ContainsAny(fileID, `/\`) || strings.Contains(fileID, "..") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	rec, rc, err := h.files.Open(fileID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer rc.Close()

	// Serve audio, video, and images inline so the browser can play/display
	// them directly; all other types are forced to download.
	disposition := "attachment"
	mt := rec.MimeType
	if strings.HasPrefix(mt, "audio/") || strings.HasPrefix(mt, "video/") || strings.HasPrefix(mt, "image/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", rec.MimeType)
	w.Header().Set("Content-Disposition", disposition+`; filename="`+sanitizeFilename(rec.Filename)+`"`)

	// ServeContent handles Accept-Ranges, Content-Length, 206 Partial Content,
	// and conditional GET automatically — essential for audio/video seeking.
	http.ServeContent(w, r, rec.Filename, time.UnixMilli(rec.Timestamp), rc)
}

// jsonErr writes a JSON {"error":"..."} response.
func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// sanitizeFilename strips characters unsafe in HTTP headers or file paths.
func sanitizeFilename(name string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '"', '\\', '\r', '\n', '/', '\x00':
			return '_'
		}
		return r
	}, name)
}
