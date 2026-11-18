package app

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

const (
	bookingValidityLessonEnd   = "lesson_end"
	bookingValidityLessonStart = "lesson_start"
)

type BookingValiditySettings struct {
	ValidityRule string `json:"validityRule"`
	Version      int    `json:"version"`
}

func effectiveBookingValidity(ctx context.Context, tx pgx.Tx) (BookingValiditySettings, error) {
	var settings BookingValiditySettings
	err := tx.QueryRow(ctx, "SELECT validity_rule,version FROM booking_validity_settings WHERE id").Scan(&settings.ValidityRule, &settings.Version)
	return settings, err
}

func confirmedBookingValidity(ctx context.Context, tx pgx.Tx, w http.ResponseWriter, version *int) (string, bool) {
	settings, err := effectiveBookingValidity(ctx, tx)
	if err != nil {
		internal(w, err)
		return "", false
	}
	if version != nil && *version != settings.Version {
		fail(w, 409, "booking_validity_changed", "Credit validity settings changed; reload booking options")
		return "", false
	}
	return settings.ValidityRule, true
}

func (a *API) bookingValiditySettings(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	ctx := r.Context()
	tx, err := a.beginIdentity(ctx)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	settings, err := effectiveBookingValidity(ctx, tx)
	if err != nil {
		internal(w, err)
		return
	}
	if r.Method == http.MethodGet {
		financeCommit(w, r, tx, 200, settings)
		return
	}
	var in BookingValiditySettings
	if !decode(w, r, &in) {
		return
	}
	if in.Version < 1 || (in.ValidityRule != bookingValidityLessonEnd && in.ValidityRule != bookingValidityLessonStart) {
		fail(w, 422, "validation_error", "Provide lesson_end or lesson_start and a valid settings version")
		return
	}
	if settings.Version != in.Version {
		fail(w, 409, "version_conflict", "Credit validity settings changed; reload before editing")
		return
	}
	in.Version++
	if _, err = tx.Exec(ctx, "UPDATE booking_validity_settings SET validity_rule=$1,version=$2 WHERE id", in.ValidityRule, in.Version); err != nil {
		internal(w, err)
		return
	}
	if err = audit(ctx, tx, s.User.ID, "booking.validity_settings.updated", "school", map[string]any{"before": settings, "after": in}); err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, in)
}
