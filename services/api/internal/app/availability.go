package app

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

type AvailabilityInput struct {
	VenueID  string    `json:"venueId"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	Capacity int       `json:"capacity"`
}
type AvailabilitySlot struct {
	ID        string `json:"id"`
	CoachID   string `json:"coachId"`
	CoachName string `json:"coachName"`
	AvailabilityInput
	VenueName string `json:"venueName"`
	Status    string `json:"status"`
	Version   int    `json:"version"`
	Editable  bool   `json:"editable"`
}
type AvailabilitySettings struct {
	LockHours int `json:"lockHours"`
	Version   int `json:"version"`
}

func (a *API) availabilityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/availability", a.auth(a.listAvailability))
	mux.HandleFunc("GET /api/v1/availability/{id}", a.auth(a.getAvailability))
	mux.HandleFunc("POST /api/v1/availability", a.auth(a.createAvailability))
	mux.HandleFunc("PATCH /api/v1/availability/{id}", a.auth(a.updateAvailability))
	mux.HandleFunc("POST /api/v1/availability/{id}/cancel", a.auth(a.cancelAvailability))
	mux.HandleFunc("GET /api/v1/settings/availability", a.auth(a.getAvailabilitySettings))
	mux.HandleFunc("PATCH /api/v1/settings/availability", a.auth(a.updateAvailabilitySettings))
}
func availabilityReader(w http.ResponseWriter, s Session) bool {
	if s.User.Role != "admin" && s.User.Role != "coach" {
		fail(w, 403, "forbidden", "Coach or administrator access is required")
		return false
	}
	return true
}
func availabilityWriter(w http.ResponseWriter, s Session) bool {
	if s.User.Role != "coach" {
		fail(w, 403, "forbidden", "Only coaches manage their own availability")
		return false
	}
	return true
}
func availabilityEditable(x AvailabilitySlot, s Session, now time.Time, lockHours int) bool {
	return s.User.Role == "coach" && x.CoachID == s.User.ID && x.Status == "submitted" && !x.StartsAt.Before(now.Add(time.Duration(lockHours)*time.Hour))
}

const availabilityColumns = "a.id,a.coach_id,u.name,a.venue_id,a.starts_at,a.ends_at,a.capacity,v.name,a.status,a.version"
const availabilityJoin = " FROM coach_availability a JOIN users u ON u.id=a.coach_id JOIN venues v ON v.id=a.venue_id "

func scanAvailability(row pgx.Row) (AvailabilitySlot, error) {
	var x AvailabilitySlot
	err := row.Scan(&x.ID, &x.CoachID, &x.CoachName, &x.VenueID, &x.StartsAt, &x.EndsAt, &x.Capacity, &x.VenueName, &x.Status, &x.Version)
	return x, err
}
func (a *API) getAvailabilitySettings(w http.ResponseWriter, r *http.Request, s Session) {
	if !availabilityReader(w, s) {
		return
	}
	var x AvailabilitySettings
	if err := a.db.QueryRow(r.Context(), "SELECT lock_hours,version FROM availability_settings WHERE id").Scan(&x.LockHours, &x.Version); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func (a *API) updateAvailabilitySettings(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input struct {
		LockHours *int `json:"lockHours"`
		Version   int  `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.LockHours == nil || *input.LockHours < 0 || *input.LockHours > 8760 || input.Version < 1 {
		fail(w, 422, "validation_error", "Lock hours must be 0–8760 with a valid version")
		return
	}
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var before AvailabilitySettings
	if err = tx.QueryRow(r.Context(), "SELECT lock_hours,version FROM availability_settings WHERE id").Scan(&before.LockHours, &before.Version); err != nil {
		internal(w, err)
		return
	}
	if before.Version != input.Version {
		fail(w, 409, "version_conflict", "Availability settings changed; reload before saving")
		return
	}
	x := AvailabilitySettings{*input.LockHours, before.Version + 1}
	if _, err = tx.Exec(r.Context(), "UPDATE availability_settings SET lock_hours=$1,version=$2 WHERE id", x.LockHours, x.Version); err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "availability.settings.updated", "availability", map[string]any{"before": before, "after": x}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func (a *API) listAvailability(w http.ResponseWriter, r *http.Request, s Session) {
	if !availabilityReader(w, s) {
		return
	}
	start, end, err := dateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	status := r.URL.Query().Get("status")
	if err != nil || (status != "" && status != "submitted" && status != "cancelled") {
		fail(w, 422, "validation_error", "Provide an inclusive Hong Kong date range of 1–93 days and valid status")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var lockHours int
	if err = tx.QueryRow(r.Context(), "SELECT lock_hours FROM availability_settings WHERE id").Scan(&lockHours); err != nil {
		internal(w, err)
		return
	}
	now := a.now()
	rows, err := tx.Query(r.Context(), "SELECT "+availabilityColumns+availabilityJoin+" WHERE a.starts_at>=$1 AND a.starts_at<$2 AND ($3='admin' OR a.coach_id=$4) AND ($5='' OR a.coach_id=$5) AND ($6='' OR a.venue_id=$6) AND ($7='' OR a.status=$7) ORDER BY a.starts_at,a.id", start, end, s.User.Role, s.User.ID, r.URL.Query().Get("coachId"), r.URL.Query().Get("venueId"), status)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []AvailabilitySlot{}
	for rows.Next() {
		x, err := scanAvailability(rows)
		if err != nil {
			internal(w, err)
			return
		}
		x.Editable = availabilityEditable(x, s, now, lockHours)
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *API) getAvailability(w http.ResponseWriter, r *http.Request, s Session) {
	if !availabilityReader(w, s) {
		return
	}
	tx, err := a.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	x, err := scanAvailability(tx.QueryRow(r.Context(), "SELECT "+availabilityColumns+availabilityJoin+" WHERE a.id=$1 AND ($2='admin' OR a.coach_id=$3)", r.PathValue("id"), s.User.Role, s.User.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Availability not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	var lockHours int
	if err = tx.QueryRow(r.Context(), "SELECT lock_hours FROM availability_settings WHERE id").Scan(&lockHours); err != nil {
		internal(w, err)
		return
	}
	x.Editable = availabilityEditable(x, s, a.now(), lockHours)
	respond(w, 200, x)
}
func validAvailability(x AvailabilityInput) bool {
	return validClass(ClassInput{VenueID: x.VenueID, ClassTypeID: "availability", CoachID: "owner", StartsAt: x.StartsAt, EndsAt: x.EndsAt, Capacity: x.Capacity})
}
func availabilityEligibility(ctx context.Context, tx pgx.Tx, coachID, venueID, previousVenue string) error {
	var active bool
	var role string
	if err := tx.QueryRow(ctx, "SELECT active,role FROM users WHERE id=$1 FOR SHARE", coachID).Scan(&active, &role); err != nil {
		return err
	}
	if !active || role != "coach" {
		return pgx.ErrNoRows
	}
	if err := tx.QueryRow(ctx, "SELECT active FROM venues WHERE id=$1 FOR SHARE", venueID).Scan(&active); err != nil {
		return err
	}
	if !active && venueID != previousVenue {
		return pgx.ErrNoRows
	}
	return nil
}
func availabilityConflict(ctx context.Context, tx pgx.Tx, coachID, id string, x AvailabilityInput) (bool, error) {
	var conflict bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM coach_availability WHERE coach_id=$1 AND id<>$2 AND status='submitted' AND starts_at<$4 AND ends_at>$3) OR EXISTS(SELECT 1 FROM class_sessions WHERE coach_id=$1 AND starts_at<$4 AND ends_at>$3)`, coachID, id, x.StartsAt, x.EndsAt).Scan(&conflict)
	return conflict, err
}
func checkAvailability(w http.ResponseWriter, r *http.Request, tx pgx.Tx, coachID, id, previousVenue string, input AvailabilityInput) bool {
	if err := availabilityEligibility(r.Context(), tx, coachID, input.VenueID, previousVenue); errors.Is(err, pgx.ErrNoRows) {
		fail(w, 422, "validation_error", "Choose an active venue and coach account")
		return false
	} else if err != nil {
		internal(w, err)
		return false
	}
	conflict, err := availabilityConflict(r.Context(), tx, coachID, id, input)
	if err != nil {
		internal(w, err)
		return false
	}
	if conflict {
		fail(w, 409, "availability_conflict", "This slot overlaps your submitted availability or an assigned class")
		return false
	}
	return true
}
func (a *API) createAvailability(w http.ResponseWriter, r *http.Request, s Session) {
	if !availabilityWriter(w, s) {
		return
	}
	var input AvailabilityInput
	if !decode(w, r, &input) {
		return
	}
	if !validAvailability(input) {
		fail(w, 422, "validation_error", "Provide a venue, valid start/end and positive capacity")
		return
	}
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var lockHours int
	if err = tx.QueryRow(r.Context(), "SELECT lock_hours FROM availability_settings WHERE id").Scan(&lockHours); err != nil {
		internal(w, err)
		return
	}
	if !checkAvailability(w, r, tx, s.User.ID, "", "", input) {
		return
	}
	now := a.now()
	if input.StartsAt.Before(now.Add(time.Duration(lockHours) * time.Hour)) {
		fail(w, 409, "availability_locked", "The start is inside the availability lock window")
		return
	}
	id, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO coach_availability(id,coach_id,venue_id,starts_at,ends_at,capacity) VALUES($1,$2,$3,$4,$5,$6)", id, s.User.ID, input.VenueID, input.StartsAt, input.EndsAt, input.Capacity); err != nil {
		internal(w, err)
		return
	}
	x, err := scanAvailability(tx.QueryRow(r.Context(), "SELECT "+availabilityColumns+availabilityJoin+" WHERE a.id=$1", id))
	if err != nil {
		internal(w, err)
		return
	}
	x.Editable = availabilityEditable(x, s, now, lockHours)
	if err = audit(r.Context(), tx, s.User.ID, "availability.created", id, x); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, x)
}
func (a *API) updateAvailability(w http.ResponseWriter, r *http.Request, s Session) {
	if !availabilityWriter(w, s) {
		return
	}
	var input struct {
		AvailabilityInput
		Version int `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !validAvailability(input.AvailabilityInput) || input.Version < 1 {
		fail(w, 422, "validation_error", "Provide valid availability and version")
		return
	}
	a.mutateAvailability(w, r, s, input.Version, &input.AvailabilityInput)
}
func (a *API) cancelAvailability(w http.ResponseWriter, r *http.Request, s Session) {
	if !availabilityWriter(w, s) {
		return
	}
	var input struct {
		Version int `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Version < 1 {
		fail(w, 422, "validation_error", "Provide the current version")
		return
	}
	a.mutateAvailability(w, r, s, input.Version, nil)
}
func (a *API) mutateAvailability(w http.ResponseWriter, r *http.Request, s Session, version int, input *AvailabilityInput) {
	tx, err := a.beginScheduling(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	before, err := scanAvailability(tx.QueryRow(r.Context(), "SELECT "+availabilityColumns+availabilityJoin+" WHERE a.id=$1 AND a.coach_id=$2", r.PathValue("id"), s.User.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Availability not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if before.Version != version {
		fail(w, 409, "version_conflict", "Availability changed; reload before saving")
		return
	}
	var lockHours int
	if err = tx.QueryRow(r.Context(), "SELECT lock_hours FROM availability_settings WHERE id").Scan(&lockHours); err != nil {
		internal(w, err)
		return
	}
	// Acquire eligibility row locks before evaluating the time boundary: these
	// locks can wait behind account or venue changes after the scheduling lock.
	if input != nil {
		if !checkAvailability(w, r, tx, s.User.ID, before.ID, before.VenueID, *input) {
			return
		}
	} else {
		var active bool
		if err = tx.QueryRow(r.Context(), "SELECT active FROM users WHERE id=$1 FOR SHARE", s.User.ID).Scan(&active); err != nil {
			internal(w, err)
			return
		}
		if !active {
			fail(w, 403, "forbidden", "The coach account is inactive")
			return
		}
	}
	now := a.now()
	if !availabilityEditable(before, s, now, lockHours) {
		fail(w, 409, "availability_locked", "This availability is cancelled or inside the lock window")
		return
	}
	action := "availability.cancelled"
	if input != nil {
		if input.StartsAt.Before(now.Add(time.Duration(lockHours) * time.Hour)) {
			fail(w, 409, "availability_locked", "The new start is inside the availability lock window")
			return
		}
		if _, err = tx.Exec(r.Context(), "UPDATE coach_availability SET venue_id=$2,starts_at=$3,ends_at=$4,capacity=$5,version=version+1 WHERE id=$1", before.ID, input.VenueID, input.StartsAt, input.EndsAt, input.Capacity); err != nil {
			internal(w, err)
			return
		}
		action = "availability.updated"
	} else {
		if _, err = tx.Exec(r.Context(), "UPDATE coach_availability SET status='cancelled',version=version+1 WHERE id=$1", before.ID); err != nil {
			internal(w, err)
			return
		}
	}
	x, err := scanAvailability(tx.QueryRow(r.Context(), "SELECT "+availabilityColumns+availabilityJoin+" WHERE a.id=$1", before.ID))
	if err != nil {
		internal(w, err)
		return
	}
	x.Editable = availabilityEditable(x, s, now, lockHours)
	if err = audit(r.Context(), tx, s.User.ID, action, x.ID, map[string]any{"before": before, "after": x}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
