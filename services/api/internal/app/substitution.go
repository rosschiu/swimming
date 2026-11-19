package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// ClassSubstitution records one coach replacing another for a single occurrence.
type ClassSubstitution struct {
	ID            string       `json:"id"`
	Lesson        ClassSession `json:"lesson"`
	FromCoachID   string       `json:"fromCoachId"`
	ToCoachID     string       `json:"toCoachId"`
	Reason        string       `json:"reason"`
	SubstitutedAt time.Time    `json:"substitutedAt"`
}

func (a *API) substitutionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/classes/{id}/substitution", a.auth(a.substituteCoach))
}

// substituteCoach moves one upcoming occurrence to another coach. Bookings, times, venue, capacity and credits are
// untouched; roster and pending-change decisions follow the class's current coach, so access moves with it.
func (a *API) substituteCoach(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var in struct {
		CoachID        string `json:"coachId"`
		Reason         string `json:"reason"`
		Version        int    `json:"version"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !validFinanceKey(in.CoachID) || in.Reason == "" || !utf8.ValidString(in.Reason) || utf8.RuneCountInString(in.Reason) > 500 ||
		strings.ContainsRune(in.Reason, 0) || in.Version < 1 || !validFinanceKey(in.IdempotencyKey) {
		fail(w, 422, "validation_error", "Provide a coach, a reason of 1–500 characters, the reviewed class version and a request key")
		return
	}
	ctx := r.Context()
	tx, err := a.beginScheduling(ctx)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(829417002)"); err != nil {
		internal(w, err)
		return
	}
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	classID := r.PathValue("id")
	request, _ := json.Marshal(map[string]any{"classId": classID, "coachId": in.CoachID, "reason": in.Reason, "version": in.Version})
	var replay string
	var same bool
	err = tx.QueryRow(ctx, "SELECT id,request=$3::jsonb FROM class_substitutions WHERE actor_id=$1 AND idempotency_key=$2", s.User.ID, in.IdempotencyKey, request).Scan(&replay, &same)
	if err == nil {
		if !same {
			fail(w, 409, "conflict", "Request key already used with different substitution details")
			return
		}
		respondSubstitution(w, r, tx, replay)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	before, err := scanClass(tx.QueryRow(ctx, "SELECT "+classColumns+classJoin+" WHERE c.id=$1 FOR UPDATE OF c", classID))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Class not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	switch {
	case before.CancelledAt != nil:
		fail(w, 409, "class_cancelled", "A cancelled class cannot be reassigned")
		return
	case before.Version != in.Version:
		fail(w, 409, "version_conflict", "The lesson changed; review it again")
		return
	case !now.Before(before.StartsAt):
		fail(w, 409, "class_started", "Started lessons cannot be reassigned")
		return
	case before.CoachID == in.CoachID:
		fail(w, 422, "validation_error", "Choose a different coach")
		return
	}
	next := before.ClassInput
	next.CoachID = in.CoachID
	// The incoming coach must be active and free of overlaps and travel-gap conflicts.
	if !checkClass(w, r, tx, next, before.ID, &before.ClassInput) {
		return
	}
	id, err := RandomToken()
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE class_sessions SET coach_id=$2,version=version+1 WHERE id=$1", classID, in.CoachID)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO class_substitutions(id,class_id,from_coach_id,to_coach_id,reason,actor_id,substituted_at,idempotency_key,request)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, classID, before.CoachID, in.CoachID, in.Reason, s.User.ID, now, in.IdempotencyKey, request)
	}
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "class.substituted", classID, map[string]any{"substitutionId": id, "fromCoachId": before.CoachID, "toCoachId": in.CoachID, "reason": in.Reason})
	}
	if err == nil {
		err = createSubstitutionNotices(ctx, tx, classID, before.CoachID, in.CoachID)
	}
	if err != nil {
		internal(w, err)
		return
	}
	respondSubstitution(w, r, tx, id)
}

// createSubstitutionNotices tells each booked swimmer's guardians and student, and both coaches, in-app. Each notice is
// sourced from the booking and versioned by the substitution count, so repeated substitutions each notify once.
// No email is sent for substitutions.
func createSubstitutionNotices(ctx context.Context, tx pgx.Tx, classID, fromCoach, toCoach string) error {
	rows, err := tx.Query(ctx, `SELECT u.id,b.student_id,b.id FROM bookings b JOIN users u ON u.active AND (u.id IN ($2,$3) OR EXISTS (
			SELECT 1 FROM student_access sa WHERE sa.student_id=b.student_id AND sa.user_id=u.id
			AND ((u.role='parent' AND sa.kind='guardian') OR (u.role='student' AND sa.kind='self'))))
		WHERE b.class_id=$1 AND b.status='reserved' ORDER BY b.id,u.id`, classID, fromCoach, toCoach)
	if err != nil {
		return err
	}
	type notice struct{ recipient, student, booking string }
	notices, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (notice, error) {
		var x notice
		err := row.Scan(&x.recipient, &x.student, &x.booking)
		return x, err
	})
	if err != nil {
		return err
	}
	var version int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM class_substitutions WHERE class_id=$1", classID).Scan(&version); err != nil {
		return err
	}
	for _, x := range notices {
		id, err := RandomToken()
		if err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version) VALUES($1,$2,$3,'class.coach_changed',$4,$5)", id, x.recipient, x.student, x.booking, version)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func respondSubstitution(w http.ResponseWriter, r *http.Request, tx pgx.Tx, id string) {
	var x ClassSubstitution
	var classID string
	err := tx.QueryRow(r.Context(), "SELECT id,class_id,from_coach_id,to_coach_id,reason,substituted_at FROM class_substitutions WHERE id=$1", id).
		Scan(&x.ID, &classID, &x.FromCoachID, &x.ToCoachID, &x.Reason, &x.SubstitutedAt)
	if err == nil {
		x.Lesson, err = scanClass(tx.QueryRow(r.Context(), "SELECT "+classColumns+classJoin+" WHERE c.id=$1", classID))
	}
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, x)
}
