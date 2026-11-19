package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIntegrationNoteMedia(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	h := New(Config{Origin: testOrigin, SessionTTL: time.Hour, MediaDir: dir}, db)
	admin := login(t, h, "admin@swim.test")
	coach := login(t, h, "coach@swim.test")
	parent := login(t, h, "parent@swim.test")
	student := login(t, h, "student@swim.test")
	other := login(t, h, "other-parent@swim.test")
	uploadURL := "/api/v1/students/amy/note-media"
	notesURL := "/api/v1/students/amy/progress-notes"
	photo := append([]byte{0xff, 0xd8, 0xff, 0xe0}, bytes.Repeat([]byte("p"), 2000)...)
	voice := append([]byte{0x1a, 0x45, 0xdf, 0xa3}, bytes.Repeat([]byte("v"), 3000)...)
	video := append([]byte("\x00\x00\x00\x18ftypqt  "), bytes.Repeat([]byte("m"), 5000)...)
	upload := func(file []byte, contentType string, auth authState, status int, code string) NoteMedia {
		t.Helper()
		w := uploadImport(t, h, uploadURL, file, contentType, auth)
		if code != "" {
			expectBookingError(t, w, status, code)
			return NoteMedia{}
		}
		expectStatus(t, w, status)
		return decodeBody[NoteMedia](t, w)
	}

	// Refusals: no storage configured, families, other swimmers, unknown types, disguised files and oversized files.
	expectBookingError(t, uploadImport(t, testHandler(db), uploadURL, photo, "image/jpeg", login(t, testHandler(db), "coach@swim.test")), 503, "media_unavailable")
	upload(photo, "image/jpeg", parent, 403, "forbidden")
	upload(photo, "image/jpeg", student, 403, "forbidden")
	expectBookingError(t, uploadImport(t, h, "/api/v1/students/ben/note-media", photo, "image/jpeg", coach), 404, "not_found")
	upload([]byte("<html>"), "text/html", coach, 415, "unsupported_media_type")
	upload([]byte("\x89PNG\r\n\x1a\n...."), "image/jpeg", coach, 422, "validation_error")
	upload(nil, "image/jpeg", coach, 422, "validation_error")
	upload(append(append([]byte{}, photo[:4]...), make([]byte, 15<<20)...), "image/jpeg", coach, 413, "too_large")

	image := upload(photo, "image/jpeg", coach, 201, "")
	audio := upload(voice, "audio/webm;codecs=opus", coach, 201, "")
	movie := upload(video, "video/quicktime", coach, 201, "")
	if image.Kind != "image" || audio.Kind != "audio" || audio.ContentType != "audio/webm" || movie.Kind != "video" || movie.SizeBytes != int64(len(video)) {
		t.Fatal(image, audio, movie)
	}
	if stored, err := os.ReadFile(filepath.Join(dir, movie.ID)); err != nil || !bytes.Equal(stored, video) {
		t.Fatal("stored video differs", err)
	}

	// A note can be only attachments; they keep the order given.
	body := map[string]any{"requestId": "media-1", "body": "", "mediaIds": []string{audio.ID, image.ID, movie.ID}}
	w := request(t, h, "POST", notesURL, body, coach)
	expectStatus(t, w, 201)
	note := decodeBody[ProgressNote](t, w)
	if len(note.Media) != 3 || note.Media[0].ID != audio.ID || note.Media[2].Kind != "video" || note.Media[1].URL != notesURL+"/"+note.ID+"/media/"+image.ID {
		t.Fatalf("%+v", note.Media)
	}
	w = request(t, h, "POST", notesURL, body, coach)
	expectStatus(t, w, 200)
	if replay := decodeBody[ProgressNote](t, w); replay.ID != note.ID || len(replay.Media) != 3 {
		t.Fatal(replay)
	}
	body["mediaIds"] = []string{image.ID, audio.ID, movie.ID}
	expectBookingError(t, request(t, h, "POST", notesURL, body, coach), 409, "request_conflict")
	expectBookingError(t, request(t, h, "POST", notesURL, map[string]any{"requestId": "media-2", "body": "Again", "mediaIds": []string{image.ID}}, coach), 422, "media_unavailable")
	expectBookingError(t, request(t, h, "POST", notesURL, map[string]any{"requestId": "media-3", "body": ""}, coach), 422, "validation_error")
	expectBookingError(t, request(t, h, "POST", notesURL, map[string]any{"requestId": "media-4", "body": "x", "mediaIds": []string{"a", "a"}}, coach), 422, "validation_error")
	upload(photo, "image/png", admin, 422, "validation_error")
	adminPhoto := upload(photo, "image/jpeg", admin, 201, "")
	expectBookingError(t, request(t, h, "POST", notesURL, map[string]any{"requestId": "media-5", "body": "Not mine", "mediaIds": []string{adminPhoto.ID}}, coach), 422, "media_unavailable")

	// Families read the note and its files; nobody else can.
	for _, actor := range []authState{parent, student} {
		w = request(t, h, "GET", notesURL, nil, actor)
		expectStatus(t, w, 200)
		list := decodeBody[struct {
			Items []ProgressNote `json:"items"`
		}](t, w)
		if len(list.Items) != 1 || len(list.Items[0].Media) != 3 {
			t.Fatalf("%+v", list.Items)
		}
		w = request(t, h, "GET", note.Media[2].URL, nil, actor)
		expectStatus(t, w, 200)
		if !bytes.Equal(w.Body.Bytes(), video) || w.Header().Get("Content-Type") != "video/quicktime" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal(w.Header())
		}
	}
	r := httptest.NewRequest("GET", note.Media[2].URL, nil)
	r.AddCookie(parent.cookie)
	r.Header.Set("Range", "bytes=0-99")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.Len() != 100 {
		t.Fatal(w.Code, w.Body.Len())
	}
	r = httptest.NewRequest("GET", note.Media[2].URL, nil)
	r.AddCookie(parent.cookie)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 304 {
		t.Fatal("cached copy not revalidated", w.Code)
	}
	expectStatus(t, request(t, h, "GET", note.Media[0].URL, nil, other), 404)
	expectStatus(t, request(t, h, "GET", notesURL+"/missing/media/"+audio.ID, nil, parent), 404)
	expectStatus(t, request(t, h, "GET", notesURL+"/"+note.ID+"/media/"+adminPhoto.ID, nil, admin), 404)

	// Corrections may leave text empty only when the note has attachments.
	expectStatus(t, request(t, h, "PATCH", notesURL+"/"+note.ID, map[string]any{"body": "Listen to the kick count", "reason": "Added text", "version": 1}, coach), 200)
	w = request(t, h, "PATCH", notesURL+"/"+note.ID, map[string]any{"body": "", "reason": "Text not needed", "version": 2}, coach)
	expectStatus(t, w, 200)
	if corrected := decodeBody[ProgressNote](t, w); corrected.Body != "" || len(corrected.Media) != 3 {
		t.Fatal(corrected)
	}
	w = request(t, h, "POST", notesURL, map[string]any{"requestId": "text-only", "body": "Text only"}, coach)
	expectStatus(t, w, 201)
	textOnly := decodeBody[ProgressNote](t, w)
	expectBookingError(t, request(t, h, "PATCH", notesURL+"/"+textOnly.ID, map[string]any{"body": "", "reason": "Empty", "version": 1}, coach), 422, "validation_error")

	// Attached files never change; unattached uploads are removed after a day.
	if _, err := db.Exec(ctx, "DELETE FROM note_media WHERE id=$1", image.ID); err == nil {
		t.Fatal("attached media was deleted")
	}
	if _, err := db.Exec(ctx, "UPDATE note_media SET sha256=repeat('0',64) WHERE id=$1", adminPhoto.ID); err == nil {
		t.Fatal("unattached media was rewritten")
	}
	stray := filepath.Join(dir, ".upload-stray")
	if err := os.WriteFile(stray, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupMedia(ctx, dir, db, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, adminPhoto.ID)); err != nil {
		t.Fatal("fresh upload removed early", err)
	}
	old := time.Now().Add(-25 * time.Hour)
	os.Chtimes(stray, old, old)
	if err := cleanupMedia(ctx, dir, db, time.Now().Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{adminPhoto.ID, ".upload-stray"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Fatal(gone, "kept", err)
		}
	}
	for _, kept := range []string{image.ID, audio.ID, movie.ID} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Fatal(kept, err)
		}
	}
	var remaining int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM note_media").Scan(&remaining); err != nil || remaining != 3 {
		t.Fatal(remaining, err)
	}
}

// A phone upload can outlast the server's short default timeouts; the reply must still arrive.
func TestIntegrationSlowNoteMediaUpload(t *testing.T) {
	db := testDB(t)
	h := New(Config{Origin: testOrigin, SessionTTL: time.Hour, MediaDir: t.TempDir()}, db)
	coach := login(t, h, "coach@swim.test")
	server := httptest.NewUnstartedServer(h)
	server.Config.ReadTimeout = 300 * time.Millisecond
	server.Config.WriteTimeout = 300 * time.Millisecond
	server.Start()
	defer server.Close()
	body, writer := io.Pipe()
	go func() {
		writer.Write([]byte{0xff, 0xd8, 0xff, 0xe0})
		for i := 0; i < 8; i++ {
			time.Sleep(100 * time.Millisecond)
			writer.Write(bytes.Repeat([]byte("p"), 1000))
		}
		writer.Close()
	}()
	r, _ := http.NewRequest("POST", server.URL+"/api/v1/students/amy/note-media", body)
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", "image/jpeg")
	r.Header.Set("X-CSRF-Token", coach.csrf)
	r.AddCookie(coach.cookie)
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 201 {
		t.Fatal(response.StatusCode)
	}
}
