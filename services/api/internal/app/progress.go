package app

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type ProgressNote struct {
	ID            string    `json:"id"`
	StudentID     string    `json:"studentId"`
	AuthorID      string    `json:"authorId"`
	AuthorName    string    `json:"authorName"`
	Body          string    `json:"body"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	UpdatedByID   string    `json:"updatedById"`
	UpdatedByName string    `json:"updatedByName"`
	Version       int       `json:"version"`
	Editable      bool      `json:"editable"`
}

const progressColumns = "n.id,n.student_id,n.author_id,author.name,n.body,n.created_at,n.updated_at,n.updated_by_id,editor.name,n.version"
const progressJoin = " FROM progress_notes n JOIN users author ON author.id=n.author_id JOIN users editor ON editor.id=n.updated_by_id"

func scanProgress(row pgx.Row, s Session) (ProgressNote, error) {
	var n ProgressNote
	err := row.Scan(&n.ID, &n.StudentID, &n.AuthorID, &n.AuthorName, &n.Body, &n.CreatedAt, &n.UpdatedAt, &n.UpdatedByID, &n.UpdatedByName, &n.Version)
	n.Editable = s.User.Role == "admin" || s.User.Role == "coach" && n.AuthorID == s.User.ID
	return n, err
}

func validProgressText(value string, limit int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > limit {
		return false
	}
	for _, c := range value {
		if unicode.IsControl(c) && c != '\n' && c != '\r' && c != '\t' {
			return false
		}
	}
	return true
}

func (a *API) progressAccess(w http.ResponseWriter, r *http.Request, s Session, tx pgx.Tx, write bool) bool {
	var active bool
	err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND active AND role=$2)", s.User.ID, s.User.Role).Scan(&active)
	if err != nil {
		internal(w, err)
		return false
	}
	if !active {
		fail(w, 401, "unauthenticated", "Sign in again")
		return false
	}
	if write && s.User.Role != "admin" && s.User.Role != "coach" {
		fail(w, 403, "forbidden", "Coach or administrator access is required")
		return false
	}
	var visible bool
	err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM students st WHERE "+studentScope+" AND st.id=$3)", s.User.Role, s.User.ID, r.PathValue("id")).Scan(&visible)
	if err != nil {
		internal(w, err)
		return false
	}
	if !visible {
		fail(w, 404, "not_found", "Student not found")
		return false
	}
	return true
}

func (a *API) listProgressNotes(w http.ResponseWriter, r *http.Request, s Session) {
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.progressAccess(w, r, s, tx, false) {
		return
	}
	rows, err := tx.Query(r.Context(), "SELECT "+progressColumns+progressJoin+" WHERE n.student_id=$1 ORDER BY n.created_at DESC,n.id DESC", r.PathValue("id"))
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []ProgressNote{}
	for rows.Next() {
		n, err := scanProgress(rows, s)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, n)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}

func (a *API) createProgressNote(w http.ResponseWriter, r *http.Request, s Session) {
	var input struct {
		RequestID string `json:"requestId"`
		Body      string `json:"body"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	validID := len(input.RequestID) >= 1 && len(input.RequestID) <= 128
	for _, c := range input.RequestID {
		if c < 33 || c > 126 {
			validID = false
		}
	}
	if !validID || !validProgressText(input.Body, 4000) {
		fail(w, 422, "validation_error", "Provide a note of 1–4000 characters and an ASCII request ID of 1–128 characters")
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
	var noteID, studentID, originalBody string
	err = tx.QueryRow(r.Context(), "SELECT id,student_id,original_body FROM progress_notes WHERE author_id=$1 AND request_id=$2", s.User.ID, input.RequestID).Scan(&noteID, &studentID, &originalBody)
	if err == nil {
		if studentID != r.PathValue("id") || originalBody != input.Body {
			fail(w, 409, "request_conflict", "This request ID was already used for different note details")
			return
		}
		n, err := scanProgress(tx.QueryRow(r.Context(), "SELECT "+progressColumns+progressJoin+" WHERE n.id=$1", noteID), s)
		if err != nil {
			internal(w, err)
			return
		}
		respond(w, 200, n)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	id, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO progress_notes(id,student_id,author_id,body,updated_by_id,request_id,original_body) VALUES($1,$2,$3,$4,$3,$5,$4)", id, r.PathValue("id"), s.User.ID, input.Body, input.RequestID); err != nil {
		internal(w, err)
		return
	}
	n, err := scanProgress(tx.QueryRow(r.Context(), "SELECT "+progressColumns+progressJoin+" WHERE n.id=$1", id), s)
	if err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "progress.created", n.ID, n); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, n)
}

func (a *API) updateProgressNote(w http.ResponseWriter, r *http.Request, s Session) {
	var input struct {
		Body    string `json:"body"`
		Version int    `json:"version"`
		Reason  string `json:"reason"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Version < 1 || !validProgressText(input.Body, 4000) || !validProgressText(input.Reason, 500) {
		fail(w, 422, "validation_error", "Provide a note, current version and correction reason of 1–500 characters")
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
	before, err := scanProgress(tx.QueryRow(r.Context(), "SELECT "+progressColumns+progressJoin+" WHERE n.id=$1 AND n.student_id=$2", r.PathValue("noteId"), r.PathValue("id")), s)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Progress note not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if !before.Editable {
		fail(w, 403, "forbidden", "Only the author coach or an administrator may correct this note")
		return
	}
	if before.Version != input.Version {
		fail(w, 409, "version_conflict", "This note changed; reload and review it before correcting")
		return
	}
	if _, err = tx.Exec(r.Context(), "UPDATE progress_notes SET body=$2,version=version+1,updated_at=clock_timestamp(),updated_by_id=$3 WHERE id=$1", before.ID, input.Body, s.User.ID); err != nil {
		internal(w, err)
		return
	}
	n, err := scanProgress(tx.QueryRow(r.Context(), "SELECT "+progressColumns+progressJoin+" WHERE n.id=$1", before.ID), s)
	if err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "progress.updated", n.ID, map[string]any{"before": before, "after": n, "reason": input.Reason}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, n)
}
