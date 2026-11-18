package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type LessonChange struct {
	ID             string       `json:"id"`
	BookingID      string       `json:"bookingId"`
	StudentID      string       `json:"studentId"`
	StudentName    string       `json:"studentName"`
	OriginalClass  ClassSession `json:"originalClass"`
	TargetClass    ClassSession `json:"targetClass"`
	Status         string       `json:"status"`
	RequestedAt    time.Time    `json:"requestedAt"`
	RequestedBy    string       `json:"requestedBy"`
	DecidedAt      *time.Time   `json:"decidedAt,omitempty"`
	DecidedBy      *string      `json:"decidedBy,omitempty"`
	DecisionReason *string      `json:"decisionReason,omitempty"`
}

const lessonChangeSelect = `SELECT r.id,b.id,b.student_id,st.name,r.original_class,r.target_class,
	r.status,r.requested_at,r.requested_by,r.decided_at,r.decided_by,r.decision_reason
	FROM lesson_change_requests r JOIN bookings b ON b.id=r.booking_id
	JOIN students st ON st.id=b.student_id JOIN class_sessions original ON original.id=r.original_class_id`

func scanLessonChange(row pgx.Row) (LessonChange, error) {
	var x LessonChange
	err := row.Scan(&x.ID, &x.BookingID, &x.StudentID, &x.StudentName, &x.OriginalClass, &x.TargetClass,
		&x.Status, &x.RequestedAt, &x.RequestedBy, &x.DecidedAt, &x.DecidedBy, &x.DecisionReason)
	return x, err
}

func (a *API) lessonChangeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/lesson-changes", a.auth(a.listLessonChanges))
	mux.HandleFunc("GET /api/v1/students/{id}/bookings/{bookingId}/change-options", a.auth(a.lessonChangeOptions))
	mux.HandleFunc("POST /api/v1/students/{id}/bookings/{bookingId}/change-requests", a.auth(a.requestLessonChange))
	mux.HandleFunc("PUT /api/v1/lesson-changes/{id}/decision", a.auth(a.decideLessonChange))
}

func (a *API) listLessonChanges(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "parent" && s.User.Role != "coach" && s.User.Role != "admin" {
		fail(w, 403, "forbidden", "Guardian, coach or administrator access is required")
		return
	}
	limit, cursor, ok := timestampPaging(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "pending" {
		fail(w, 422, "validation_error", "Status must be pending")
		return
	}
	ctx := r.Context()
	tx, ok := a.beginBookingRead(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	var cutoff int
	if err := tx.QueryRow(ctx, "SELECT parent_change_cutoff_hours FROM lesson_settings WHERE id").Scan(&cutoff); err != nil {
		internal(w, err)
		return
	}
	rows, err := tx.Query(ctx, lessonChangeSelect+`
		WHERE ($1='admin' OR ($1='coach' AND original.coach_id=$2)
		OR ($1='parent' AND EXISTS (SELECT 1 FROM student_access sa WHERE sa.student_id=b.student_id AND sa.user_id=$2 AND sa.kind='guardian')))
		AND ($3='' OR (r.requested_at,r.id)<($4,$3)) AND ($6='' OR r.status=$6)
		ORDER BY r.requested_at DESC,r.id DESC LIMIT $5`, s.User.Role, s.User.ID, cursor.ID, cursor.CreatedAt, limit+1, status)
	if err != nil {
		internal(w, err)
		return
	}
	items, err := pgx.AppendRows([]LessonChange{}, rows, func(row pgx.CollectableRow) (LessonChange, error) { return scanLessonChange(row) })
	if err != nil {
		internal(w, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		value := encodeTimestampCursor(last.RequestedAt, last.ID)
		next = &value
	}
	financeCommit(w, r, tx, 200, map[string]any{"items": items, "cutoffHours": cutoff, "nextCursor": next})
}

type changeBooking struct {
	ID, StudentID, Level, ClassID, PackageID string
	Attended                                 bool
}

func loadChangeBooking(ctx context.Context, tx pgx.Tx, id, studentID string) (changeBooking, error) {
	var b changeBooking
	err := tx.QueryRow(ctx, `SELECT b.id,b.student_id,st.level,b.class_id,b.credit_package_id,ba.booking_id IS NOT NULL
		FROM bookings b JOIN students st ON st.id=b.student_id LEFT JOIN booking_attendance ba ON ba.booking_id=b.id
		WHERE b.id=$1 AND b.student_id=$2 AND b.status='reserved' FOR UPDATE OF b,st`, id, studentID).
		Scan(&b.ID, &b.StudentID, &b.Level, &b.ClassID, &b.PackageID, &b.Attended)
	return b, err
}

func loadChangeClass(ctx context.Context, tx pgx.Tx, id string) (ClassSession, error) {
	return scanClass(tx.QueryRow(ctx, "SELECT "+classColumns+classJoin+" WHERE c.id=$1 FOR UPDATE OF c", id))
}

func lessonChangeTarget(ctx context.Context, tx pgx.Tx, b changeBooking, target ClassSession, expiry *time.Time, rule string, now time.Time) (string, error) {
	if target.ID == b.ClassID || !target.StartsAt.After(now) {
		return "class_unavailable", nil
	}
	if target.Level != b.Level {
		return "level_mismatch", nil
	}
	if !bookingCreditCovers(rule, target, expiry, now) {
		return "credit_unavailable", nil
	}
	if err := eligibleClass(ctx, tx, target.ClassInput, nil); errors.Is(err, pgx.ErrNoRows) {
		return "class_unavailable", nil
	} else if err != nil {
		return "", err
	}
	var reserved int
	var overlap bool
	err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM bookings WHERE class_id=$1 AND status='reserved'),
		EXISTS (SELECT 1 FROM bookings b JOIN class_sessions c ON c.id=b.class_id
			WHERE b.student_id=$2 AND b.id<>$3 AND b.status='reserved' AND c.starts_at<$5 AND c.ends_at>$4)`,
		target.ID, b.StudentID, b.ID, target.StartsAt, target.EndsAt).Scan(&reserved, &overlap)
	if err != nil {
		return "", err
	}
	if overlap {
		return "student_overlap", nil
	}
	if reserved >= target.Capacity {
		return "class_full", nil
	}
	return "", nil
}

func changeTargetResult(w http.ResponseWriter, code string, err error) bool {
	if err != nil {
		internal(w, err)
		return false
	}
	if code != "" {
		messages := map[string]string{
			"class_unavailable":  "This class is unavailable for a lesson change",
			"level_mismatch":     "Student level differs from the target class level",
			"credit_unavailable": "The reserved credit does not cover the target class",
			"student_overlap":    "Student already has a booking that overlaps this class",
			"class_full":         "This class has no places available",
		}
		fail(w, 409, code, messages[code])
		return false
	}
	return true
}

func changeWindow(ctx context.Context, tx pgx.Tx) (int, int, error) {
	var cutoff, version int
	err := tx.QueryRow(ctx, "SELECT parent_change_cutoff_hours,version FROM lesson_settings WHERE id FOR SHARE").Scan(&cutoff, &version)
	return cutoff, version, err
}

func canRequestLessonChange(b changeBooking, original ClassSession, now time.Time, cutoff int) bool {
	return !b.Attended && original.StartsAt.After(now) && !now.After(original.StartsAt.Add(-time.Duration(cutoff)*time.Hour))
}

func (a *API) lessonChangeOptions(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "parent" {
		fail(w, 403, "forbidden", "Guardian access is required")
		return
	}
	start, end, err := dateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		fail(w, 422, "validation_error", "Provide valid Hong Kong dates spanning 1–93 days")
		return
	}
	ctx := r.Context()
	tx, _, ok := a.beginStudentBooking(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	b, err := loadChangeBooking(ctx, tx, r.PathValue("bookingId"), r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Booking not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	cutoff, _, err := changeWindow(ctx, tx)
	if err != nil {
		internal(w, err)
		return
	}
	original, err := loadChangeClass(ctx, tx, b.ClassID)
	if err != nil {
		internal(w, err)
		return
	}
	expiry, err := lockBookingPackage(ctx, tx, b.PackageID, b.StudentID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	items := []ClassSession{}
	now := a.now().UTC().Truncate(time.Microsecond)
	if err == nil && canRequestLessonChange(b, original, now, cutoff) {
		var reserved, pending bool
		reserved, err = hasBookingReservation(ctx, tx, b.PackageID, b.ID)
		if err == nil {
			err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM lesson_change_requests WHERE booking_id=$1 AND status='pending')", b.ID).Scan(&pending)
		}
		if err != nil {
			internal(w, err)
			return
		}
		if !reserved || pending {
			financeCommit(w, r, tx, 200, map[string]any{"items": items, "cutoffHours": cutoff})
			return
		}
		settings, err := effectiveBookingValidity(ctx, tx)
		if err != nil {
			internal(w, err)
			return
		}
		rows, err := tx.Query(ctx, "SELECT "+classColumns+classJoin+`
			WHERE c.starts_at>=$1 AND c.starts_at<$2 AND c.starts_at>$3 AND c.id<>$4 AND t.level=$5
			AND v.active AND t.active AND u.active AND u.role='coach'
			AND (SELECT count(*) FROM bookings WHERE class_id=c.id AND status='reserved')<c.capacity
			AND NOT EXISTS (SELECT 1 FROM bookings b JOIN class_sessions booked ON booked.id=b.class_id
				WHERE b.student_id=$6 AND b.id<>$7 AND b.status='reserved'
				AND booked.starts_at<c.ends_at AND booked.ends_at>c.starts_at)
			ORDER BY c.starts_at,c.id`, start, end, now, original.ID, b.Level, b.StudentID, b.ID)
		if err != nil {
			internal(w, err)
			return
		}
		classes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ClassSession, error) { return scanClass(row) })
		if err != nil {
			internal(w, err)
			return
		}
		for _, class := range classes {
			if bookingCreditCovers(settings.ValidityRule, class, expiry, now) {
				items = append(items, class)
			}
		}
	}
	financeCommit(w, r, tx, 200, map[string]any{"items": items, "cutoffHours": cutoff})
}

func (a *API) requestLessonChange(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "parent" {
		fail(w, 403, "forbidden", "Guardian access is required")
		return
	}
	var in struct {
		TargetClassID      string `json:"targetClassId"`
		TargetClassVersion int    `json:"targetClassVersion"`
		IdempotencyKey     string `json:"idempotencyKey"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validFinanceKey(in.IdempotencyKey) || !validFinanceKey(in.TargetClassID) || in.TargetClassVersion < 1 {
		fail(w, 422, "validation_error", "Provide a target class, its version and a request key")
		return
	}
	ctx := r.Context()
	tx, _, ok := a.beginStudentBooking(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	studentID, bookingID := r.PathValue("id"), r.PathValue("bookingId")
	x, err := scanLessonChange(tx.QueryRow(ctx, lessonChangeSelect+" WHERE r.requested_by=$1 AND r.idempotency_key=$2", s.User.ID, in.IdempotencyKey))
	if err == nil {
		if x.StudentID != studentID || x.BookingID != bookingID || x.TargetClass.ID != in.TargetClassID || x.TargetClass.Version != in.TargetClassVersion {
			fail(w, 409, "conflict", "Request key already used with different lesson change details")
			return
		}
		financeCommit(w, r, tx, 200, x)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	b, err := loadChangeBooking(ctx, tx, bookingID, studentID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Booking not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	var pending bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM lesson_change_requests WHERE booking_id=$1 AND status='pending')", b.ID).Scan(&pending); err != nil {
		internal(w, err)
		return
	}
	if pending {
		fail(w, 409, "change_pending", "This booking already has a pending change request")
		return
	}
	cutoff, version, err := changeWindow(ctx, tx)
	if err != nil {
		internal(w, err)
		return
	}
	original, err := loadChangeClass(ctx, tx, b.ClassID)
	if err != nil {
		internal(w, err)
		return
	}
	target, err := loadChangeClass(ctx, tx, in.TargetClassID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Target class not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if target.Version != in.TargetClassVersion {
		fail(w, 409, "target_changed", "Refresh the options to review the current replacement lesson")
		return
	}
	expiry, err := lockBookingPackage(ctx, tx, b.PackageID, b.StudentID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 409, "credit_unavailable", "Booking credit package is unavailable")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	if !canRequestLessonChange(b, original, now, cutoff) {
		fail(w, 409, "change_cutoff", "The original lesson is past the change request cutoff")
		return
	}
	reserved, err := hasBookingReservation(ctx, tx, b.PackageID, b.ID)
	if !reserved {
		changeTargetResult(w, "credit_unavailable", err)
		return
	}
	settings, err := effectiveBookingValidity(ctx, tx)
	if err != nil {
		internal(w, err)
		return
	}
	code, err := lessonChangeTarget(ctx, tx, b, target, expiry, settings.ValidityRule, now)
	if !changeTargetResult(w, code, err) {
		return
	}
	id, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	// Snapshot both lessons before the coach makes a decision.
	_, err = tx.Exec(ctx, `INSERT INTO lesson_change_requests(id,booking_id,original_class_id,target_class_id,original_class,target_class,
		requested_by,requested_at,settings_version,idempotency_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		id, b.ID, original.ID, target.ID, original, target, s.User.ID, now, version, in.IdempotencyKey)
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "lesson_change.requested", id, map[string]any{"bookingId": b.ID, "studentId": b.StudentID,
			"originalClassId": original.ID, "targetClassId": target.ID, "cutoffHours": cutoff, "settingsVersion": version})
	}
	if err != nil {
		internal(w, err)
		return
	}
	a.respondLessonChange(w, r, tx, id, 201)
}

func (a *API) respondLessonChange(w http.ResponseWriter, r *http.Request, tx pgx.Tx, id string, status int) {
	x, err := scanLessonChange(tx.QueryRow(r.Context(), lessonChangeSelect+" WHERE r.id=$1", id))
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, status, x)
}

func (a *API) decideLessonChange(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "coach" {
		fail(w, 403, "forbidden", "The original class coach decides lesson changes")
		return
	}
	var in struct {
		Decision string  `json:"decision"`
		Reason   *string `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Reason != nil {
		reason := strings.TrimSpace(*in.Reason)
		in.Reason = &reason
	}
	if (in.Decision != "approved" && in.Decision != "rejected") ||
		(in.Reason != nil && (*in.Reason == "" || !utf8.ValidString(*in.Reason) || utf8.RuneCountInString(*in.Reason) > 500 || strings.ContainsRune(*in.Reason, 0))) {
		fail(w, 422, "validation_error", "Provide an approved or rejected decision and an optional reason of 1–500 characters")
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
	var active bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='coach' AND active FOR SHARE)", s.User.ID).Scan(&active); err != nil {
		internal(w, err)
		return
	}
	if !active {
		fail(w, 401, "unauthenticated", "Sign in again")
		return
	}
	id := r.PathValue("id")
	x, err := scanLessonChange(tx.QueryRow(ctx, lessonChangeSelect+" WHERE r.id=$1 AND original.coach_id=$2 FOR UPDATE OF r,original", id, s.User.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Lesson change request not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if x.Status != "pending" {
		sameReason := x.DecisionReason == nil && in.Reason == nil ||
			x.DecisionReason != nil && in.Reason != nil && *x.DecisionReason == *in.Reason
		if x.Status != in.Decision || x.DecidedBy == nil || *x.DecidedBy != s.User.ID || !sameReason {
			fail(w, 409, "change_conflict", "This lesson change already has a different decision")
			return
		}
		financeCommit(w, r, tx, 200, x)
		return
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	if in.Decision == "approved" {
		var approved bool
		now, approved = a.approveLessonChange(ctx, tx, w, x)
		if !approved {
			return
		}
	}
	_, err = tx.Exec(ctx, "UPDATE lesson_change_requests SET status=$2,decided_by=$3,decided_at=$4,decision_reason=$5 WHERE id=$1",
		id, in.Decision, s.User.ID, now, in.Reason)
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "lesson_change."+in.Decision, id, map[string]any{"bookingId": x.BookingID, "studentId": x.StudentID,
			"originalClassId": x.OriginalClass.ID, "targetClassId": x.TargetClass.ID, "reason": in.Reason})
	}
	if err != nil {
		internal(w, err)
		return
	}
	a.respondLessonChange(w, r, tx, id, 200)
}

func (a *API) approveLessonChange(ctx context.Context, tx pgx.Tx, w http.ResponseWriter, x LessonChange) (time.Time, bool) {
	b, err := loadChangeBooking(ctx, tx, x.BookingID, x.StudentID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (b.Attended || b.ClassID != x.OriginalClass.ID)) {
		fail(w, 409, "change_conflict", "The original booking can no longer be moved")
		return time.Time{}, false
	}
	if err != nil {
		internal(w, err)
		return time.Time{}, false
	}
	original, err := loadChangeClass(ctx, tx, b.ClassID)
	if err != nil {
		internal(w, err)
		return time.Time{}, false
	}
	target, err := loadChangeClass(ctx, tx, x.TargetClass.ID)
	if err != nil {
		internal(w, err)
		return time.Time{}, false
	}
	reviewed := x.TargetClass
	if !target.StartsAt.Equal(reviewed.StartsAt) || !target.EndsAt.Equal(reviewed.EndsAt) ||
		target.VenueID != reviewed.VenueID || target.CoachID != reviewed.CoachID ||
		target.ClassTypeID != reviewed.ClassTypeID || target.Level != reviewed.Level {
		fail(w, 409, "target_changed", "The replacement changed after the request. The guardian must review it again")
		return time.Time{}, false
	}
	expiry, err := lockBookingPackage(ctx, tx, b.PackageID, b.StudentID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 409, "credit_unavailable", "Booking credit package is unavailable")
		return time.Time{}, false
	}
	if err != nil {
		internal(w, err)
		return time.Time{}, false
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	if !original.StartsAt.After(now) {
		fail(w, 409, "change_conflict", "The original lesson has already started")
		return time.Time{}, false
	}
	reserved, err := hasBookingReservation(ctx, tx, b.PackageID, b.ID)
	if !reserved {
		changeTargetResult(w, "credit_unavailable", err)
		return time.Time{}, false
	}
	settings, err := effectiveBookingValidity(ctx, tx)
	if err != nil {
		internal(w, err)
		return time.Time{}, false
	}
	code, err := lessonChangeTarget(ctx, tx, b, target, expiry, settings.ValidityRule, now)
	if !changeTargetResult(w, code, err) {
		return time.Time{}, false
	}
	_, err = tx.Exec(ctx, `UPDATE bookings SET class_id=$2,class_version=$3,class_type_version=$4,
		student_level=$5,class_level=$6,override_actor_id=NULL,override_reason=NULL,validity_rule=$7 WHERE id=$1`,
		b.ID, target.ID, target.Version, target.ClassTypeVersion, b.Level, target.Level, settings.ValidityRule)
	if err != nil {
		internal(w, err)
		return time.Time{}, false
	}
	return now, true
}
