package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Photos, voice recordings and videos attached to progress notes. A coach uploads each file first, then shares the
// note with the uploaded IDs; files stay private and are served only to readers of that note.

type NoteMedia struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	URL         string `json:"url,omitempty"`
}

type mediaFormat struct {
	kind  string
	limit int64
	magic func([]byte) bool
}

const (
	maxNoteMedia      = 6
	unattachedMaxAge  = 24 * time.Hour
	mediaTransferTime = 10 * time.Minute
)

func isISOBMFF(b []byte) bool { return len(b) >= 12 && string(b[4:8]) == "ftyp" }
func isWebM(b []byte) bool    { return bytes.HasPrefix(b, []byte{0x1a, 0x45, 0xdf, 0xa3}) }

// Formats phones record or pick: browsers' MediaRecorder output (WebM, Ogg, MP4), camera photos and iPhone videos.
var mediaFormats = map[string]mediaFormat{
	"image/jpeg": {"image", 15 << 20, func(b []byte) bool { return bytes.HasPrefix(b, []byte{0xff, 0xd8, 0xff}) }},
	"image/png":  {"image", 15 << 20, func(b []byte) bool { return bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) }},
	"image/webp": {"image", 15 << 20, func(b []byte) bool { return len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" }},
	"audio/webm": {"audio", 25 << 20, isWebM},
	"audio/ogg":  {"audio", 25 << 20, func(b []byte) bool { return bytes.HasPrefix(b, []byte("OggS")) }},
	"audio/mp4":  {"audio", 25 << 20, isISOBMFF},
	"audio/mpeg": {"audio", 25 << 20, func(b []byte) bool {
		return bytes.HasPrefix(b, []byte("ID3")) || len(b) > 1 && b[0] == 0xff && b[1]&0xe0 == 0xe0
	}},
	"video/mp4":       {"video", 100 << 20, isISOBMFF},
	"video/quicktime": {"video", 100 << 20, isISOBMFF},
	"video/webm":      {"video", 100 << 20, isWebM},
}

func (a *API) mediaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/students/{id}/note-media", a.auth(a.uploadNoteMedia))
	mux.HandleFunc("GET /api/v1/students/{id}/progress-notes/{noteId}/media/{mediaId}", a.auth(a.serveNoteMedia))
}

func mediaURL(studentID, noteID, mediaID string) string {
	return "/api/v1/students/" + studentID + "/progress-notes/" + noteID + "/media/" + mediaID
}

func (a *API) uploadNoteMedia(w http.ResponseWriter, r *http.Request, s Session) {
	if a.cfg.MediaDir == "" {
		fail(w, 503, "media_unavailable", "Photo, voice and video uploads are not set up on this server")
		return
	}
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	format, ok := mediaFormats[contentType]
	if !ok {
		fail(w, 415, "unsupported_media_type", "Upload a JPEG, PNG or WebP photo, a WebM, Ogg, MP4 or MP3 recording, or an MP4, MOV or WebM video")
		return
	}
	// Check access before reading a large body, and again when recording the upload.
	if !a.checkProgressAccess(w, r, s, true) {
		return
	}
	// A video from a phone can take minutes to arrive; the server's default timeouts are for small JSON requests.
	// The write deadline starts with the request too, so it must move as well or the reply is lost.
	transfer := http.NewResponseController(w)
	transfer.SetReadDeadline(time.Now().Add(mediaTransferTime))
	transfer.SetWriteDeadline(time.Now().Add(mediaTransferTime))
	tmp, err := os.CreateTemp(a.cfg.MediaDir, ".upload-")
	if err != nil {
		internal(w, err)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	hash := sha256.New()
	body := http.MaxBytesReader(w, r.Body, format.limit)
	head := make([]byte, 512)
	n, err := io.ReadFull(body, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		mediaReadFailure(w, err, format)
		return
	}
	head = head[:n]
	if n == 0 || !format.magic(head) {
		fail(w, 422, "validation_error", "The file does not match its type; choose the photo, recording or video again")
		return
	}
	size, err := io.Copy(io.MultiWriter(tmp, hash), io.MultiReader(bytes.NewReader(head), body))
	if err != nil {
		mediaReadFailure(w, err, format)
		return
	}
	if err = tmp.Sync(); err != nil {
		internal(w, err)
		return
	}
	id, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.progressAccess(w, r, s, tx, true) {
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO note_media(id,student_id,uploader_id,kind,content_type,size_bytes,sha256) VALUES($1,$2,$3,$4,$5,$6,$7)",
		id, r.PathValue("id"), s.User.ID, format.kind, contentType, size, hex.EncodeToString(hash.Sum(nil))); err != nil {
		internal(w, err)
		return
	}
	final := filepath.Join(a.cfg.MediaDir, id)
	if err = os.Rename(tmp.Name(), final); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		os.Remove(final)
		internal(w, err)
		return
	}
	respond(w, 201, NoteMedia{ID: id, Kind: format.kind, ContentType: contentType, SizeBytes: size})
}

func mediaReadFailure(w http.ResponseWriter, err error, format mediaFormat) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(w, 413, "too_large", map[string]string{"image": "Photos must be 15 MB or smaller", "audio": "Recordings must be 25 MB or smaller", "video": "Videos must be 100 MB or smaller"}[format.kind])
		return
	}
	fail(w, 400, "upload_interrupted", "The upload did not finish; try again")
}

// checkProgressAccess runs progressAccess in its own short transaction.
func (a *API) checkProgressAccess(w http.ResponseWriter, r *http.Request, s Session, write bool) bool {
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return false
	}
	defer tx.Rollback(r.Context())
	return a.progressAccess(w, r, s, tx, write)
}

func (a *API) serveNoteMedia(w http.ResponseWriter, r *http.Request, s Session) {
	if a.cfg.MediaDir == "" {
		fail(w, 404, "not_found", "Attachment not found")
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.progressAccess(w, r, s, tx, false) {
		return
	}
	var contentType, digest string
	var created time.Time
	err = tx.QueryRow(r.Context(), `SELECT m.content_type,m.sha256,m.created_at FROM note_media m JOIN progress_notes n ON n.id=m.note_id
		WHERE m.id=$1 AND n.id=$2 AND n.student_id=$3`, r.PathValue("mediaId"), r.PathValue("noteId"), r.PathValue("id")).Scan(&contentType, &digest, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Attachment not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	tx.Rollback(r.Context())
	f, err := os.Open(filepath.Join(a.cfg.MediaDir, filepath.Base(r.PathValue("mediaId"))))
	if err != nil {
		internal(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Content-Disposition", "inline")
	// Revalidate each time so a reader who loses access cannot keep using a cached copy.
	w.Header().Set("Cache-Control", "private, no-cache")
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(mediaTransferTime))
	w.Header().Set("ETag", `"`+digest+`"`)
	// ServeContent answers range requests, which phones use to play and seek video.
	http.ServeContent(w, r, "", created, f)
}

// attachNoteMedia links uploaded files to a new note, in the order given. It reports false when any is unusable.
func attachNoteMedia(ctx context.Context, tx pgx.Tx, noteID, studentID, uploaderID string, ids []string) (bool, error) {
	if len(ids) == 0 {
		return true, nil
	}
	var found int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT id FROM note_media WHERE id=ANY($1) AND student_id=$2 AND uploader_id=$3 AND note_id IS NULL FOR UPDATE) m`,
		ids, studentID, uploaderID).Scan(&found); err != nil || found != len(ids) {
		return false, err
	}
	for i, id := range ids {
		if _, err := tx.Exec(ctx, "UPDATE note_media SET note_id=$1,position=$2 WHERE id=$3", noteID, i, id); err != nil {
			return false, err
		}
	}
	return true, nil
}

// loadNoteMedia fills in each note's attachments.
func loadNoteMedia(ctx context.Context, tx pgx.Tx, notes []ProgressNote) error {
	if len(notes) == 0 {
		return nil
	}
	index := map[string]int{}
	ids := make([]string, len(notes))
	for i, n := range notes {
		index[n.ID] = i
		ids[i] = n.ID
	}
	rows, err := tx.Query(ctx, "SELECT note_id,id,kind,content_type,size_bytes FROM note_media WHERE note_id=ANY($1) ORDER BY note_id,position", ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var noteID string
		var m NoteMedia
		if err = rows.Scan(&noteID, &m.ID, &m.Kind, &m.ContentType, &m.SizeBytes); err != nil {
			return err
		}
		n := &notes[index[noteID]]
		m.URL = mediaURL(n.StudentID, n.ID, m.ID)
		n.Media = append(n.Media, m)
	}
	return rows.Err()
}

// RunMediaCleanup removes uploads that were never attached to a note, and interrupted upload files.
func RunMediaCleanup(ctx context.Context, cfg Config, db *pgxpool.Pool) {
	if cfg.MediaDir == "" {
		return
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := cleanupMedia(ctx, cfg.MediaDir, db, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("media cleanup: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func cleanupMedia(ctx context.Context, dir string, db *pgxpool.Pool, now time.Time) error {
	rows, err := db.Query(ctx, "DELETE FROM note_media WHERE note_id IS NULL AND created_at<$1 RETURNING id", now.Add(-unattachedMaxAge))
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err = os.Remove(filepath.Join(dir, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && strings.HasPrefix(e.Name(), ".upload-") && now.Sub(info.ModTime()) > unattachedMaxAge {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}
