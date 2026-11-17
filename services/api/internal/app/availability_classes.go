package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type AvailabilityClassInput struct {
	RequestID           string    `json:"requestId"`
	AvailabilityVersion int       `json:"availabilityVersion"`
	ClassTypeID         string    `json:"classTypeId"`
	StartsAt            time.Time `json:"startsAt"`
	EndsAt              time.Time `json:"endsAt"`
	Capacity            int       `json:"capacity"`
}

func (a *API) createAvailabilityClass(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input AvailabilityClassInput
	if !decode(w, r, &input) {
		return
	}
	candidate := ClassInput{VenueID: "source", CoachID: "source", ClassTypeID: input.ClassTypeID, StartsAt: input.StartsAt, EndsAt: input.EndsAt, Capacity: input.Capacity}
	if !validClass(candidate) || input.AvailabilityVersion < 1 || strings.TrimSpace(input.RequestID) != input.RequestID || len(input.RequestID) < 1 || len(input.RequestID) > 128 {
		fail(w, 422, "validation_error", "Provide valid lesson details, availability version and a request ID of 1–128 bytes")
		return
	}
	// Normalize equivalent timestamps before comparing retries.
	input.StartsAt, input.EndsAt = input.StartsAt.UTC(), input.EndsAt.UTC()
	payload, err := json.Marshal(input)
	if err != nil {
		internal(w, err)
		return
	}
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var classID string
	var matches bool
	err = tx.QueryRow(r.Context(), "SELECT class_id,availability_id=$3 AND request=$4::jsonb FROM availability_classes WHERE actor_id=$1 AND request_id=$2", s.User.ID, input.RequestID, r.PathValue("id"), payload).Scan(&classID, &matches)
	if err == nil {
		if !matches {
			fail(w, 409, "request_conflict", "This request ID was already used for different lesson details")
			return
		}
		x, err := scanClass(tx.QueryRow(r.Context(), "SELECT "+classColumns+classJoin+" WHERE c.id=$1", classID))
		if err != nil {
			internal(w, err)
			return
		}
		respond(w, 200, x)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	slot, err := scanAvailability(tx.QueryRow(r.Context(), "SELECT "+availabilityColumns+availabilityJoin+" WHERE a.id=$1", r.PathValue("id")))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Availability not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if slot.Version != input.AvailabilityVersion {
		fail(w, 409, "version_conflict", "Availability changed; reload before scheduling")
		return
	}
	if slot.Status != "submitted" {
		fail(w, 409, "availability_cancelled", "This availability was cancelled")
		return
	}
	if input.StartsAt.Before(slot.StartsAt) || input.EndsAt.After(slot.EndsAt) {
		fail(w, 422, "validation_error", "The entire lesson must fit inside the availability window")
		return
	}
	candidate.CoachID, candidate.VenueID = slot.CoachID, slot.VenueID
	if !checkClass(w, r, tx, candidate, "", nil) {
		return
	}
	if !input.StartsAt.After(a.now()) {
		fail(w, 409, "availability_elapsed", "Schedule a lesson that starts in the future")
		return
	}
	x, err := insertClass(r.Context(), tx, candidate, nil)
	if err != nil {
		internal(w, err)
		return
	}
	snapshot, err := json.Marshal(slot)
	if err != nil {
		internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO availability_classes(class_id,availability_id,actor_id,request_id,request,window_snapshot) VALUES($1,$2,$3,$4,$5,$6)", x.ID, slot.ID, s.User.ID, input.RequestID, payload, snapshot); err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "class.created", x.ID, map[string]any{"class": x, "availabilityId": slot.ID, "availabilityVersion": slot.Version}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, x)
}

func (a *API) listAvailabilityClasses(w http.ResponseWriter, r *http.Request, s Session) {
	if !availabilityReader(w, s) {
		return
	}
	tx, err := a.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var exists bool
	if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM coach_availability WHERE id=$1 AND ($2='admin' OR coach_id=$3))", r.PathValue("id"), s.User.Role, s.User.ID).Scan(&exists); err != nil {
		internal(w, err)
		return
	}
	if !exists {
		fail(w, 404, "not_found", "Availability not found")
		return
	}
	rows, err := tx.Query(r.Context(), "SELECT "+classColumns+classJoin+" JOIN availability_classes ac ON ac.class_id=c.id WHERE ac.availability_id=$1 AND ($2='admin' OR c.coach_id=$3) ORDER BY c.starts_at,c.id", r.PathValue("id"), s.User.Role, s.User.ID)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []ClassSession{}
	for rows.Next() {
		x, err := scanClass(rows)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
