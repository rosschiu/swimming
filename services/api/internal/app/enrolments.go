package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type Enrolment struct {
	ID                    string           `json:"id"`
	StudentID             string           `json:"studentId"`
	SeriesID              string           `json:"seriesId"`
	Bookings              []StudentBooking `json:"bookings"`
	CreatedAt             time.Time        `json:"createdAt"`
	SourceEnrolmentID     string           `json:"sourceEnrolmentId,omitempty"`
	CarriedForwardCredits *int             `json:"carriedForwardCredits,omitempty"`
}

type enrolmentSession struct {
	ClassID          string `json:"classId"`
	ClassVersion     int    `json:"classVersion"`
	ClassTypeVersion int    `json:"classTypeVersion"`
}

type partialEnrolmentConfirmation struct {
	SessionIDs      []string `json:"sessionIds"`
	SettingsVersion int      `json:"settingsVersion"`
}

type enrolmentInput struct {
	SeriesID               string                        `json:"seriesId"`
	Sessions               []enrolmentSession            `json:"sessions"`
	CreditPackageID        string                        `json:"creditPackageId"`
	IdempotencyKey         string                        `json:"idempotencyKey"`
	BookingValidityVersion *int                          `json:"bookingValidityVersion,omitempty"`
	OverrideReason         string                        `json:"overrideReason,omitempty"`
	PartialConfirmation    *partialEnrolmentConfirmation `json:"partialConfirmation,omitempty"`
	SourcePackageID        string                        `json:"sourcePackageId,omitempty"`
	SettingsVersion        *int                          `json:"settingsVersion,omitempty"`
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
	sourceEnrolmentID := r.PathValue("enrolmentId")
	validID := func(id string) bool { return id != "" && len(id) <= 100 && !strings.ContainsRune(id, 0) }
	if !validID(in.SeriesID) || !validID(in.CreditPackageID) || !validFinanceKey(in.IdempotencyKey) ||
		(in.BookingValidityVersion != nil && *in.BookingValidityVersion < 1) || len(in.Sessions) < 1 || len(in.Sessions) > 14 ||
		utf8.RuneCountInString(in.OverrideReason) > 500 || strings.ContainsRune(in.OverrideReason, 0) {
		fail(w, 422, "validation_error", "Provide a series, 1–14 sessions, a credit package and valid request key and override reason")
		return
	}
	if (in.SettingsVersion != nil && *in.SettingsVersion < 1) ||
		(sourceEnrolmentID != "" && (!validID(sourceEnrolmentID) || !validID(in.SourcePackageID) || in.SettingsVersion == nil || in.BookingValidityVersion == nil)) ||
		(sourceEnrolmentID == "" && in.SourcePackageID != "") {
		fail(w, 422, "validation_error", "Renewal requires the source package and current fixed enrolment and booking validity versions")
		return
	}
	if s.User.Role != "admin" && in.OverrideReason != "" {
		fail(w, 403, "forbidden", "Only administrators may override a swimming level mismatch")
		return
	}
	requestedIDs := make(map[string]bool, len(in.Sessions))
	sort.Slice(in.Sessions, func(i, j int) bool { return in.Sessions[i].ClassID < in.Sessions[j].ClassID })
	for i, session := range in.Sessions {
		if !validID(session.ClassID) || session.ClassVersion < 1 || session.ClassTypeVersion < 1 ||
			(i > 0 && in.Sessions[i-1].ClassID == session.ClassID) {
			fail(w, 422, "validation_error", "Provide distinct classes with valid versions")
			return
		}
		requestedIDs[session.ClassID] = true
	}
	if partial := in.PartialConfirmation; partial != nil {
		if partial.SettingsVersion < 1 || len(partial.SessionIDs) == 0 || len(partial.SessionIDs) > len(in.Sessions) {
			fail(w, 422, "validation_error", "Provide the available session IDs and settings version")
			return
		}
		slices.Sort(partial.SessionIDs)
		for i, id := range partial.SessionIDs {
			if !requestedIDs[id] || (i > 0 && partial.SessionIDs[i-1] == id) {
				fail(w, 422, "validation_error", "Provide distinct session IDs from the original request")
				return
			}
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
		StudentID         string `json:"studentId"`
		SourceEnrolmentID string `json:"sourceEnrolmentId,omitempty"`
		enrolmentInput
	}{enrolment.StudentID, sourceEnrolmentID, in})
	var same bool
	err := tx.QueryRow(ctx, "SELECT id,created_at,request=$3::jsonb FROM enrolments WHERE actor_id=$1 AND idempotency_key=$2",
		s.User.ID, in.IdempotencyKey, canonical).Scan(&enrolment.ID, &enrolment.CreatedAt, &same)
	status := 200
	if err == nil {
		if !same {
			fail(w, 409, "conflict", "Request key already used with different enrolment details")
			return
		}
		if sourceEnrolmentID != "" {
			if err = loadEnrolmentRenewal(ctx, tx, &enrolment); err != nil {
				internal(w, err)
				return
			}
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		settings, err := readFixedEnrolmentSettings(ctx, tx, "FOR SHARE")
		if err != nil {
			internal(w, err)
			return
		}
		if (in.SettingsVersion != nil && *in.SettingsVersion != settings.Version) ||
			(in.PartialConfirmation != nil && (in.PartialConfirmation.SettingsVersion != settings.Version ||
				settings.AllocationPolicy != fixedAllocateSubset)) {
			fail(w, 409, "fixed_enrolment_settings_changed", "Fixed enrolment settings changed. Reload before confirming.")
			return
		}
		rule, confirmed := confirmedBookingValidity(ctx, tx, w, in.BookingValidityVersion)
		if !confirmed {
			return
		}
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
		starts := make(map[string]time.Time, len(in.Sessions))
		for _, selected := range in.Sessions {
			var seriesID *string
			var startsAt time.Time
			err = tx.QueryRow(ctx, "SELECT series_id,starts_at FROM class_sessions WHERE id=$1 FOR UPDATE", selected.ClassID).Scan(&seriesID, &startsAt)
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
			starts[selected.ClassID] = startsAt
		}
		if sourceEnrolmentID != "" {
			if !a.prepareEnrolmentRenewal(w, r, s, tx, settings, &enrolment, in.SourcePackageID, in.CreditPackageID, in.Sessions) {
				return
			}
		} else {
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
		}
		_, err = tx.Exec(ctx, `INSERT INTO enrolments(id,student_id,series_id,credit_package_id,actor_id,idempotency_key,request,created_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, enrolment.ID, enrolment.StudentID, in.SeriesID, in.CreditPackageID, s.User.ID,
			in.IdempotencyKey, canonical, enrolment.CreatedAt)
		if err != nil {
			internal(w, err)
			return
		}
		var unavailable func(code, message string)
		var skipped bool
		if settings.AllocationPolicy == fixedAllocateSubset {
			unavailable = func(_, _ string) { skipped = true }
			// Give earlier lessons first access to a limited credit balance.
			sort.SliceStable(in.Sessions, func(i, j int) bool {
				return starts[in.Sessions[i].ClassID].Before(starts[in.Sessions[j].ClassID])
			})
		}
		overrideUsed := false
		available := make([]string, 0, len(in.Sessions))
		for _, selected := range in.Sessions {
			skipped = false
			booking, reserved := a.reserveBooking(ctx, tx, w, s, enrolment.StudentID, studentLevel,
				bookingInput{ClassID: selected.ClassID, ClassVersion: selected.ClassVersion, ClassTypeVersion: selected.ClassTypeVersion,
					CreditPackageID: in.CreditPackageID, OverrideReason: in.OverrideReason},
				canonical, in.SeriesID, &enrolment.ID, enrolment.CreatedAt, rule, unavailable)
			if skipped {
				continue
			}
			if !reserved {
				return
			}
			available = append(available, selected.ClassID)
			overrideUsed = overrideUsed || booking.Class.Level != studentLevel
		}
		partial := in.PartialConfirmation
		if partial != nil || len(available) < len(in.Sessions) {
			sortedAvailable := slices.Clone(available)
			slices.Sort(sortedAvailable)
			if partial == nil || !slices.Equal(partial.SessionIDs, sortedAvailable) {
				respond(w, 409, map[string]any{"error": map[string]any{
					"code":    "partial_enrolment_confirmation_required",
					"message": "Review the available sessions before confirming the enrolment.",
					"details": map[string]any{"availableSessionIds": available, "settingsVersion": settings.Version},
				}})
				return
			}
		}
		if in.OverrideReason != "" && !overrideUsed {
			fail(w, 422, "validation_error", "Provide an override reason only when swimming levels differ")
			return
		}
		err = audit(ctx, tx, s.User.ID, "enrolment.created", enrolment.ID, map[string]any{
			"studentId": enrolment.StudentID, "seriesId": in.SeriesID, "creditPackageId": in.CreditPackageID, "sessionCount": len(available),
			"requestedSessionCount": len(in.Sessions), "allocationPolicy": settings.AllocationPolicy, "settingsVersion": settings.Version,
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
