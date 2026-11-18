package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type Enrolment struct {
	ID        string           `json:"id"`
	StudentID string           `json:"studentId"`
	SeriesID  string           `json:"seriesId"`
	Bookings  []StudentBooking `json:"bookings"`
	CreatedAt time.Time        `json:"createdAt"`
}

type enrolmentSession struct {
	ClassID          string `json:"classId"`
	ClassVersion     int    `json:"classVersion"`
	ClassTypeVersion int    `json:"classTypeVersion"`
}

type enrolmentInput struct {
	SeriesID        string             `json:"seriesId"`
	Sessions        []enrolmentSession `json:"sessions"`
	CreditPackageID string             `json:"creditPackageId"`
	IdempotencyKey  string             `json:"idempotencyKey"`
	OverrideReason  string             `json:"overrideReason,omitempty"`
}

func (a *API) createEnrolment(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "parent" && s.User.Role != "admin" {
		fail(w, 403, "forbidden", "Guardian or administrator access is required")
		return
	}
	var in enrolmentInput
	if !decode(w, r, &in) {
		return
	}
	in.OverrideReason = strings.TrimSpace(in.OverrideReason)
	validID := func(id string) bool { return id != "" && len(id) <= 100 && !strings.ContainsRune(id, 0) }
	if !validID(in.SeriesID) || !validID(in.CreditPackageID) || !validFinanceKey(in.IdempotencyKey) ||
		len(in.Sessions) < 1 || len(in.Sessions) > 14 ||
		utf8.RuneCountInString(in.OverrideReason) > 500 || strings.ContainsRune(in.OverrideReason, 0) {
		fail(w, 422, "validation_error", "Provide a series, 1–14 sessions, a credit package and valid request key and override reason")
		return
	}
	if s.User.Role != "admin" && in.OverrideReason != "" {
		fail(w, 403, "forbidden", "Only administrators may override a swimming level mismatch")
		return
	}
	sort.Slice(in.Sessions, func(i, j int) bool { return in.Sessions[i].ClassID < in.Sessions[j].ClassID })
	for i, session := range in.Sessions {
		if !validID(session.ClassID) || session.ClassVersion < 1 || session.ClassTypeVersion < 1 ||
			(i > 0 && in.Sessions[i-1].ClassID == session.ClassID) {
			fail(w, 422, "validation_error", "Provide distinct classes with valid versions")
			return
		}
	}

	ctx := r.Context()
	tx, studentLevel, ok := a.beginStudentBooking(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	enrolment := Enrolment{StudentID: r.PathValue("id"), SeriesID: in.SeriesID}
	canonical, _ := json.Marshal(struct {
		StudentID string `json:"studentId"`
		enrolmentInput
	}{enrolment.StudentID, in})
	var same bool
	err := tx.QueryRow(ctx, "SELECT id,created_at,request=$3::jsonb FROM enrolments WHERE actor_id=$1 AND idempotency_key=$2",
		s.User.ID, in.IdempotencyKey, canonical).Scan(&enrolment.ID, &enrolment.CreatedAt, &same)
	status := 200
	if err == nil {
		if !same {
			fail(w, 409, "conflict", "Request key already used with different enrolment details")
			return
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM class_series WHERE id=$1)", in.SeriesID).Scan(&exists)
		if err != nil {
			internal(w, err)
			return
		}
		if !exists {
			fail(w, 404, "not_found", "Class series not found")
			return
		}
		enrolment.ID, err = RandomToken()
		if err != nil {
			internal(w, err)
			return
		}
		// Lock all selected classes in ID order before the package, as in single booking.
		for _, selected := range in.Sessions {
			var seriesID *string
			err = tx.QueryRow(ctx, "SELECT series_id FROM class_sessions WHERE id=$1 FOR UPDATE", selected.ClassID).Scan(&seriesID)
			if errors.Is(err, pgx.ErrNoRows) {
				fail(w, 404, "not_found", "Class not found")
				return
			}
			if err != nil {
				internal(w, err)
				return
			}
			if seriesID == nil || *seriesID != in.SeriesID {
				fail(w, 422, "validation_error", "Every selected class must belong to the weekly series")
				return
			}
		}
		_, err = lockBookingPackage(ctx, tx, in.CreditPackageID, enrolment.StudentID)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, 404, "not_found", "Credit package not found")
			return
		}
		if err != nil {
			internal(w, err)
			return
		}
		enrolment.CreatedAt = a.now().UTC().Truncate(time.Microsecond)
		rule := effectiveBookingValidity()
		_, err = tx.Exec(ctx, `INSERT INTO enrolments(id,student_id,series_id,credit_package_id,actor_id,idempotency_key,request,created_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, enrolment.ID, enrolment.StudentID, in.SeriesID, in.CreditPackageID, s.User.ID,
			in.IdempotencyKey, canonical, enrolment.CreatedAt)
		if err != nil {
			internal(w, err)
			return
		}
		overrideUsed := false
		for _, selected := range in.Sessions {
			booking, reserved := a.reserveBooking(ctx, tx, w, s, enrolment.StudentID, studentLevel,
				bookingInput{ClassID: selected.ClassID, ClassVersion: selected.ClassVersion, ClassTypeVersion: selected.ClassTypeVersion,
					CreditPackageID: in.CreditPackageID, OverrideReason: in.OverrideReason},
				canonical, in.SeriesID, &enrolment.ID, enrolment.CreatedAt, rule)
			if !reserved {
				return
			}
			overrideUsed = overrideUsed || booking.Class.Level != studentLevel
		}
		if in.OverrideReason != "" && !overrideUsed {
			fail(w, 422, "validation_error", "Provide an override reason only when swimming levels differ")
			return
		}
		err = audit(ctx, tx, s.User.ID, "enrolment.created", enrolment.ID, map[string]any{
			"studentId": enrolment.StudentID, "seriesId": in.SeriesID, "creditPackageId": in.CreditPackageID, "sessionCount": len(in.Sessions),
		})
		if err != nil {
			internal(w, err)
			return
		}
		status = 201
	} else {
		internal(w, err)
		return
	}

	rows, err := tx.Query(ctx, "SELECT b.id,b.student_id,b.status,b.created_at,"+classColumns+classJoin+
		"JOIN bookings b ON b.class_id=c.id WHERE b.enrolment_id=$1 ORDER BY c.starts_at,c.id", enrolment.ID)
	if err == nil {
		enrolment.Bookings, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (StudentBooking, error) {
			return scanStudentBooking(row)
		})
	}
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, status, enrolment)
}
