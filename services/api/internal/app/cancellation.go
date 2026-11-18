package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// BookingCancellation is the family-visible record of a guardian cancelling one lesson.
type BookingCancellation struct {
	Timing        string       `json:"timing"`
	CreditOutcome string       `json:"creditOutcome"`
	CancelledAt   time.Time    `json:"cancelledAt"`
	Lesson        ClassSession `json:"lesson"`
}

// CancellationPreview states the consequence a guardian confirms; the write recomputes it under lock.
type CancellationPreview struct {
	BookingID     string       `json:"bookingId"`
	Lesson        ClassSession `json:"lesson"`
	Timing        string       `json:"timing"`
	CreditOutcome string       `json:"creditOutcome"`
	LateFrom      time.Time    `json:"lateFrom"`
	ClosesAt      time.Time    `json:"closesAt"`
}

type cancellationState struct {
	preview   CancellationPreview
	studentID string
	packageID string
	policy    CreditPolicy
	cutoff    int
}

func (a *API) cancellationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/students/{id}/bookings/{bookingId}/cancellation", a.auth(a.previewCancellation))
	mux.HandleFunc("POST /api/v1/students/{id}/bookings/{bookingId}/cancellation", a.auth(a.cancelBooking))
}

// loadCancellation classifies a still-cancellable booking. Self-service closes at lesson start;
// a booking is early up to and including the configured guardian cutoff before the start.
func loadCancellation(ctx context.Context, tx pgx.Tx, w http.ResponseWriter, studentID, bookingID string, now time.Time, lock bool) (cancellationState, bool) {
	var x cancellationState
	var attended bool
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF b,c"
	}
	fields := []any{&x.preview.BookingID, &x.studentID, &x.packageID, &attended, &x.cutoff}
	var lesson ClassSession
	c := &lesson
	fields = append(fields, &c.ID, &c.VenueID, &c.ClassTypeID, &c.CoachID, &c.StartsAt, &c.EndsAt, &c.Capacity, &c.VenueName, &c.Region, &c.ClassTypeName, &c.Level, &c.CoachName, &c.SeriesID, &c.Version, &c.ClassTypeVersion)
	err := tx.QueryRow(ctx, `SELECT b.id,b.student_id,b.credit_package_id,ba.booking_id IS NOT NULL,ls.parent_change_cutoff_hours,`+classColumns+classJoin+`
		JOIN bookings b ON b.class_id=c.id CROSS JOIN lesson_settings ls LEFT JOIN booking_attendance ba ON ba.booking_id=b.id
		WHERE b.id=$1 AND b.student_id=$2 AND b.status='reserved'`+suffix, bookingID, studentID).Scan(fields...)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Booking not found")
		return x, false
	}
	if err != nil {
		internal(w, err)
		return x, false
	}
	x.preview.Lesson = lesson
	x.preview.ClosesAt = lesson.StartsAt
	x.preview.LateFrom = lesson.StartsAt.Add(-time.Duration(x.cutoff) * time.Hour)
	if attended || !now.Before(lesson.StartsAt) {
		fail(w, 409, "cancellation_closed", "This lesson has started or already has attendance; contact the school")
		return x, false
	}
	var expiry *time.Time
	var data []byte
	packageQuery := "SELECT expires_on,policy FROM credit_packages WHERE id=$1 AND student_id=$2"
	if lock {
		packageQuery += " FOR UPDATE"
	}
	if err = tx.QueryRow(ctx, packageQuery, x.packageID, x.studentID).Scan(&expiry, &data); err == nil {
		err = json.Unmarshal(data, &x.policy)
	}
	if err != nil {
		internal(w, err)
		return x, false
	}
	x.preview.Timing = "late"
	retain := x.policy.LateCancellationRetainsCredit
	if !now.After(x.preview.LateFrom) {
		x.preview.Timing = "early"
		retain = x.policy.EarlyCancellationRetainsCredit
	}
	switch {
	case !retain:
		x.preview.CreditOutcome = "consumed"
	case expiry != nil && !now.Before(packageExpiryCutoff(*expiry)):
		x.preview.CreditOutcome = "expired"
	default:
		x.preview.CreditOutcome = "retained"
	}
	return x, true
}

func (a *API) previewCancellation(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "parent" {
		fail(w, 403, "forbidden", "Guardian access is required")
		return
	}
	tx, ok := a.beginBookingRead(w, r, s)
	if !ok {
		return
	}
	ctx := r.Context()
	defer tx.Rollback(ctx)
	var scoped bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM students st WHERE "+studentScope+" AND st.id=$3)", s.User.Role, s.User.ID, r.PathValue("id")).Scan(&scoped); err != nil {
		internal(w, err)
		return
	}
	if !scoped {
		fail(w, 404, "not_found", "Student not found")
		return
	}
	x, ok := loadCancellation(ctx, tx, w, r.PathValue("id"), r.PathValue("bookingId"), a.now().UTC(), false)
	if ok {
		respond(w, 200, x.preview)
	}
}

func (a *API) cancelBooking(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "parent" {
		fail(w, 403, "forbidden", "Guardian access is required")
		return
	}
	var in struct {
		ClassID        string `json:"classId"`
		Timing         string `json:"timing"`
		CreditOutcome  string `json:"creditOutcome"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validFinanceKey(in.ClassID) || (in.Timing != "early" && in.Timing != "late") || (in.CreditOutcome != "consumed" && in.CreditOutcome != "retained" && in.CreditOutcome != "expired") || !validFinanceKey(in.IdempotencyKey) {
		fail(w, 422, "validation_error", "Provide the reviewed timing, credit outcome and a request key")
		return
	}
	ctx := r.Context()
	tx, _, ok := a.beginStudentBooking(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	studentID, bookingID := r.PathValue("id"), r.PathValue("bookingId")
	request, _ := json.Marshal(map[string]string{"bookingId": bookingID, "classId": in.ClassID, "timing": in.Timing, "creditOutcome": in.CreditOutcome})
	var replay string
	var same bool
	err := tx.QueryRow(ctx, "SELECT booking_id,request=$3::jsonb FROM booking_cancellations WHERE actor_id=$1 AND idempotency_key=$2", s.User.ID, in.IdempotencyKey, request).Scan(&replay, &same)
	if err == nil {
		if replay != bookingID || !same {
			fail(w, 409, "conflict", "Request key already used with different cancellation details")
			return
		}
		respondCancelledBooking(w, r, tx, studentID, bookingID)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	x, ok := loadCancellation(ctx, tx, w, studentID, bookingID, now, true)
	if !ok {
		return
	}
	// An approved move or a changed rule since review needs a fresh confirmation of the current lesson.
	if x.preview.Lesson.ID != in.ClassID || x.preview.Timing != in.Timing || x.preview.CreditOutcome != in.CreditOutcome {
		respond(w, 409, map[string]any{"error": map[string]any{"code": "cancellation_changed", "message": "The cancellation outcome changed; review it again", "details": x.preview}})
		return
	}
	reserved, err := hasBookingReservation(ctx, tx, x.packageID, bookingID)
	if err != nil {
		internal(w, err)
		return
	}
	if !reserved {
		fail(w, 409, "credit_unavailable", "Booking does not have a reserved credit")
		return
	}
	available, consumed, expired := outcomeMovement(x.preview.CreditOutcome)
	movementID, err := insertMovement(ctx, tx, x.packageID, "booking:"+bookingID+":cancelled", available, -1, consumed, expired, s.User.ID, "Lesson cancelled by guardian", now)
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE bookings SET status='cancelled' WHERE id=$1", bookingID)
	}
	if err == nil {
		lesson, _ := json.Marshal(x.preview.Lesson)
		policy, _ := json.Marshal(x.policy)
		_, err = tx.Exec(ctx, `INSERT INTO booking_cancellations(booking_id,actor_id,cancelled_at,timing,credit_outcome,movement_id,class_snapshot,policy,cutoff_hours,idempotency_key,request)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, bookingID, s.User.ID, now, x.preview.Timing, x.preview.CreditOutcome, movementID, lesson, policy, x.cutoff, in.IdempotencyKey, request)
	}
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "booking.cancelled", bookingID, map[string]any{"studentId": studentID, "classId": x.preview.Lesson.ID,
			"creditPackageId": x.packageID, "movementId": movementID, "timing": x.preview.Timing, "creditOutcome": x.preview.CreditOutcome, "cutoffHours": x.cutoff, "policy": x.policy})
	}
	if err == nil {
		err = closePendingChanges(ctx, tx, bookingID, s.User.ID, "Booking cancelled", now)
	}
	if err == nil {
		err = a.createCancellationNotices(ctx, tx, studentID, bookingID, x.preview.Lesson.CoachID)
	}
	if err != nil {
		internal(w, err)
		return
	}
	respondCancelledBooking(w, r, tx, studentID, bookingID)
}

func respondCancelledBooking(w http.ResponseWriter, r *http.Request, tx pgx.Tx, studentID, bookingID string) {
	booking, err := scanStudentBooking(tx.QueryRow(r.Context(), "SELECT "+studentBookingColumns+studentBookingJoin+"WHERE b.id=$1 AND b.student_id=$2", bookingID, studentID))
	if err == nil {
		booking.Cancellation, err = loadBookingCancellation(r.Context(), tx, bookingID)
	}
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, booking)
}

func loadBookingCancellation(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, bookingID string) (*BookingCancellation, error) {
	var x BookingCancellation
	var lesson []byte
	err := q.QueryRow(ctx, "SELECT timing,credit_outcome,cancelled_at,class_snapshot FROM booking_cancellations WHERE booking_id=$1", bookingID).
		Scan(&x.Timing, &x.CreditOutcome, &x.CancelledAt, &lesson)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err == nil {
		err = json.Unmarshal(lesson, &x.Lesson)
	}
	return &x, err
}

// createCancellationNotices tells every linked guardian, the student and the class coach in-app. Families also get a
// generic email when the school enables it, subject to each recipient's service-email preference and the allowlist.
func (a *API) createCancellationNotices(ctx context.Context, tx pgx.Tx, studentID, bookingID, coachID string) error {
	var email bool
	if err := tx.QueryRow(ctx, "SELECT cancellation_email FROM lesson_settings WHERE id").Scan(&email); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT u.id,u.email,u.role,u.service_email_enabled FROM users u
		WHERE u.active AND (u.id=$2 OR EXISTS (SELECT 1 FROM student_access sa WHERE sa.student_id=$1 AND sa.user_id=u.id
			AND ((u.role='parent' AND sa.kind='guardian') OR (u.role='student' AND sa.kind='self')))) ORDER BY u.id`, studentID, coachID)
	if err != nil {
		return err
	}
	type recipient struct {
		id, address, role string
		enabled           bool
	}
	recipients, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (recipient, error) {
		var x recipient
		err := row.Scan(&x.id, &x.address, &x.role, &x.enabled)
		return x, err
	})
	if err != nil {
		return err
	}
	for _, x := range recipients {
		id, err := RandomToken()
		if err != nil {
			return err
		}
		// A booking is cancelled once, so each recipient has exactly one notice for it.
		if _, err = tx.Exec(ctx, "INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version) VALUES($1,$2,$3,'booking.cancelled',$4,1)", id, x.id, studentID, bookingID); err != nil {
			return err
		}
		if !email || x.role == "coach" {
			continue
		}
		status, reason := "pending", ""
		if !x.enabled {
			status, reason = "suppressed", "email_disabled"
		} else if !a.teachingEmailAllowed(x.address) {
			status, reason = "suppressed", "email_unavailable"
		}
		deliveryID, err := RandomToken()
		if err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO notification_deliveries(id,notification_id,status,next_attempt_at,completed_at,last_failure) VALUES($1,$2,$3,CASE WHEN $3='pending' THEN clock_timestamp() END,CASE WHEN $3='suppressed' THEN clock_timestamp() END,NULLIF($4,''))", deliveryID, id, status, reason)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
