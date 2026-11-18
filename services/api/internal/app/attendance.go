package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const attendanceAuditExists = "EXISTS (SELECT 1 FROM audit_events e WHERE e.entity_id=ba.booking_id AND e.action IN ('attendance.present','attendance.absent','attendance.ill','attendance.corrected'))"

const attendanceOpening = "c.starts_at + ls.attendance_open_minutes * interval '1 minute'"

// attendanceColumns pairs with attendanceScan; every booking read reports the current outcome, not only presence.
const attendanceColumns = "ba.marked_at,ba.status,ba.credit_outcome,ba.revision"

type BookingAttendance struct {
	Status        string              `json:"status"`
	MarkedAt      time.Time           `json:"markedAt"`
	CreditOutcome string              `json:"creditOutcome"`
	Revision      int                 `json:"revision"`
	Evidence      *AttendanceEvidence `json:"evidence,omitempty"`
}

type AttendanceEvidence struct {
	RecordedBy string `json:"recordedBy"`
	HasAudit   bool   `json:"hasAudit"`
}

type attendanceScan struct {
	markedAt        *time.Time
	status, outcome *string
	revision        *int
}

func (x *attendanceScan) fields() []any {
	return []any{&x.markedAt, &x.status, &x.outcome, &x.revision}
}

func (x attendanceScan) value() *BookingAttendance {
	if x.markedAt == nil {
		return nil
	}
	return &BookingAttendance{Status: *x.status, MarkedAt: x.markedAt.UTC(), CreditOutcome: *x.outcome, Revision: *x.revision}
}

func validAttendanceStatus(status string) bool {
	return status == "present" || status == "absent" || status == "ill"
}

// attendanceOutcome applies the funding package's frozen policy. A retained credit released after the
// package's Hong Kong expiry date goes straight to expired history instead of the usable balance.
func attendanceOutcome(status string, policy CreditPolicy, expiry *time.Time, now time.Time) string {
	retain := status == "absent" && policy.AbsenceRetainsCredit || status == "ill" && policy.IllnessRetainsCredit
	switch {
	case !retain:
		return "consumed"
	case expiry != nil && !now.Before(packageExpiryCutoff(*expiry)):
		return "expired"
	default:
		return "retained"
	}
}

type attendanceTarget struct {
	studentID, packageID         string
	opensAt, endsAt, correctable time.Time
	current                      attendanceScan
}

// beginAttendance serialises attendance writes with bookings, lesson changes and expiry, rechecking the actor.
func (a *API) beginAttendance(w http.ResponseWriter, r *http.Request, s Session) (pgx.Tx, attendanceTarget, bool) {
	var target attendanceTarget
	if s.User.Role != "admin" && s.User.Role != "coach" {
		fail(w, 403, "forbidden", "Class coach or administrator access is required")
		return nil, target, false
	}
	ctx := r.Context()
	tx, err := a.beginScheduling(ctx)
	if err != nil {
		internal(w, err)
		return nil, target, false
	}
	deny := func(status int, code, message string) (pgx.Tx, attendanceTarget, bool) {
		tx.Rollback(ctx)
		fail(w, status, code, message)
		return nil, target, false
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(829417002)"); err != nil {
		tx.Rollback(ctx)
		internal(w, err)
		return nil, target, false
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role=$2 AND active FOR SHARE)", s.User.ID, s.User.Role).Scan(&active); err != nil {
		tx.Rollback(ctx)
		internal(w, err)
		return nil, target, false
	}
	if !active {
		return deny(401, "unauthenticated", "Sign in again")
	}
	fields := append([]any{&target.studentID, &target.packageID}, target.current.fields()...)
	fields = append(fields, &target.opensAt, &target.endsAt, &target.correctable)
	err = tx.QueryRow(ctx, "SELECT b.student_id,b.credit_package_id,"+attendanceColumns+","+attendanceOpening+`,c.ends_at,
		c.ends_at + ls.correction_window_days * interval '1 day'
		FROM class_sessions c CROSS JOIN lesson_settings ls
		JOIN bookings b ON b.id=$2 AND b.status='reserved'
		LEFT JOIN booking_attendance ba ON ba.booking_id=b.id
		WHERE c.id=$1 AND b.class_id=c.id AND ($3='admin' OR c.coach_id=$4)
		FOR UPDATE OF c,b FOR SHARE OF ls`, r.PathValue("id"), r.PathValue("bookingId"), s.User.Role, s.User.ID).Scan(fields...)
	if errors.Is(err, pgx.ErrNoRows) {
		return deny(404, "not_found", "Booking not found")
	}
	if err != nil {
		tx.Rollback(ctx)
		internal(w, err)
		return nil, target, false
	}
	return tx, target, true
}

func lockAttendancePackage(ctx context.Context, tx pgx.Tx, packageID, studentID string) (*time.Time, CreditPolicy, error) {
	var expiry *time.Time
	var policy CreditPolicy
	var data []byte
	err := tx.QueryRow(ctx, `SELECT cp.expires_on,cp.policy FROM credit_packages cp JOIN purchases p ON p.id=cp.purchase_id
		WHERE cp.id=$1 AND cp.student_id=$2 AND p.student_id=$2 AND p.status='paid' FOR UPDATE OF cp`,
		packageID, studentID).Scan(&expiry, &data)
	if err == nil {
		err = json.Unmarshal(data, &policy)
	}
	return expiry, policy, err
}

func outcomeMovement(outcome string) (available, consumed, expired int) {
	switch outcome {
	case "retained":
		return 1, 0, 0
	case "expired":
		return 0, 0, 1
	}
	return 0, 1, 0
}

func insertMovement(ctx context.Context, tx pgx.Tx, packageID, source string, available, reserved, consumed, expired int, actorID, reason string, now time.Time) (string, error) {
	id, err := RandomToken()
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, packageID, source, available, reserved, consumed, expired, actorID, reason, now)
	}
	return id, err
}

// closePendingChanges ends a lesson change that can no longer move this booking, keeping it in history.
func closePendingChanges(ctx context.Context, tx pgx.Tx, bookingID, actorID, reason string, now time.Time) error {
	rows, err := tx.Query(ctx, `UPDATE lesson_change_requests SET status='closed',decided_by=$2,decided_at=$3,decision_reason=$4
		WHERE booking_id=$1 AND status='pending' RETURNING id`, bookingID, actorID, now, reason)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	for _, id := range ids {
		if err == nil {
			err = audit(ctx, tx, actorID, "lesson_change.closed", id, map[string]any{"bookingId": bookingID, "reason": reason})
		}
	}
	return err
}

func (a *API) markAttendance(w http.ResponseWriter, r *http.Request, s Session) {
	// An empty body keeps the original present-only clients working.
	status := "present"
	if r.ContentLength != 0 {
		var in struct {
			Status string `json:"status"`
		}
		if !decode(w, r, &in) {
			return
		}
		status = in.Status
	}
	if !validAttendanceStatus(status) {
		fail(w, 422, "validation_error", "Attendance must be present, absent or ill")
		return
	}
	tx, target, ok := a.beginAttendance(w, r, s)
	if !ok {
		return
	}
	ctx := r.Context()
	defer tx.Rollback(ctx)
	bookingID := r.PathValue("bookingId")
	if existing := target.current.value(); existing != nil {
		if existing.Status != status {
			fail(w, 409, "attendance_recorded", "Attendance is already recorded; submit a correction instead")
			return
		}
		respondAttendance(w, r, tx, s, bookingID)
		return
	}
	expiry, policy, err := lockAttendancePackage(ctx, tx, target.packageID, target.studentID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 409, "credit_unavailable", "Booking credit package is unavailable")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	// Present opens shortly after the start; a final absence waits until the lesson has ended.
	opens := target.opensAt
	if status != "present" {
		opens = target.endsAt
	}
	if now.Before(opens) {
		fail(w, 409, "attendance_not_open", "Attendance is not open for this class")
		return
	}
	reserved, err := hasBookingReservation(ctx, tx, target.packageID, bookingID)
	if err != nil {
		internal(w, err)
		return
	}
	if !reserved {
		fail(w, 409, "credit_unavailable", "Booking does not have a reserved credit")
		return
	}
	outcome := attendanceOutcome(status, policy, expiry, now)
	available, consumed, expired := outcomeMovement(outcome)
	reason := map[string]string{"present": "Class attendance", "absent": "Absence", "ill": "Illness"}[status]
	movementID, err := insertMovement(ctx, tx, target.packageID, "booking:"+bookingID+":"+status, available, -1, consumed, expired, s.User.ID, reason, now)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO booking_attendance(booking_id,status,actor_id,marked_at,movement_id,credit_outcome)
			VALUES($1,$2,$3,$4,$5,$6)`, bookingID, status, s.User.ID, now, movementID, outcome)
	}
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "attendance."+status, bookingID, map[string]any{
			"classId": r.PathValue("id"), "studentId": target.studentID, "creditPackageId": target.packageID,
			"movementId": movementID, "markedAt": now, "creditOutcome": outcome,
		})
	}
	if err == nil {
		err = closePendingChanges(ctx, tx, bookingID, s.User.ID, "Attendance recorded", now)
	}
	if err != nil {
		internal(w, err)
		return
	}
	respondAttendance(w, r, tx, s, bookingID)
}

type attendanceCorrectionInput struct {
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	Revision       int    `json:"revision"`
	IdempotencyKey string `json:"idempotencyKey"`
}

func (a *API) correctAttendance(w http.ResponseWriter, r *http.Request, s Session) {
	var in attendanceCorrectionInput
	if !decode(w, r, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !validAttendanceStatus(in.Status) || in.Revision < 1 || !validFinanceKey(in.IdempotencyKey) ||
		in.Reason == "" || !utf8.ValidString(in.Reason) || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsRune(in.Reason, 0) {
		fail(w, 422, "validation_error", "Provide an outcome, the current revision, a request key and a reason of 1–500 characters")
		return
	}
	tx, target, ok := a.beginAttendance(w, r, s)
	if !ok {
		return
	}
	ctx := r.Context()
	defer tx.Rollback(ctx)
	bookingID := r.PathValue("bookingId")
	request, _ := json.Marshal(map[string]any{"bookingId": bookingID, "status": in.Status, "reason": in.Reason, "revision": in.Revision})
	var replayBooking string
	var same bool
	err := tx.QueryRow(ctx, "SELECT booking_id,request=$3::jsonb FROM attendance_corrections WHERE actor_id=$1 AND idempotency_key=$2",
		s.User.ID, in.IdempotencyKey, request).Scan(&replayBooking, &same)
	if err == nil {
		if replayBooking != bookingID || !same {
			fail(w, 409, "conflict", "Request key already used with different correction details")
			return
		}
		respondAttendance(w, r, tx, s, bookingID)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	current := target.current.value()
	if current == nil {
		fail(w, 409, "attendance_not_recorded", "Record attendance before correcting it")
		return
	}
	if current.Revision != in.Revision {
		fail(w, 409, "attendance_changed", "Attendance changed; reload before correcting it")
		return
	}
	if current.Status == in.Status {
		fail(w, 422, "validation_error", "Choose a different outcome")
		return
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	if now.After(target.correctable) {
		fail(w, 409, "correction_closed", "The correction window for this lesson has closed")
		return
	}
	if in.Status != "present" && now.Before(target.endsAt) {
		fail(w, 409, "attendance_not_open", "Absence and illness can be recorded once the lesson has ended")
		return
	}
	expiry, policy, err := lockAttendancePackage(ctx, tx, target.packageID, target.studentID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 409, "credit_unavailable", "Booking credit package is unavailable")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	// Settle any due expiry first so the reversal below sees the package's true available and expired balances.
	if err = expireStudentCredits(ctx, tx, target.studentID, a.now()); err != nil {
		internal(w, err)
		return
	}
	from, to := current.CreditOutcome, attendanceOutcome(in.Status, policy, expiry, now)
	var movementID *string
	delta := [3]int{} // available, consumed, expired
	switch {
	case (from == "consumed") == (to == "consumed"):
		// Both outcomes keep or both use the credit: the unit stays where it already is.
		to = from
	case from == "consumed":
		available, _, expired := outcomeMovement(to)
		delta = [3]int{available, -1, expired}
	default:
		// The ledger pools a package's units, so take back one unit from the same package and never another.
		var available, expired int
		if err = tx.QueryRow(ctx, "SELECT COALESCE(sum(available),0),COALESCE(sum(expired),0) FROM credit_movements WHERE package_id=$1",
			target.packageID).Scan(&available, &expired); err != nil {
			internal(w, err)
			return
		}
		switch {
		case available > 0:
			delta = [3]int{-1, 1, 0}
		case expired > 0:
			delta = [3]int{0, 1, -1}
		default:
			fail(w, 409, "credit_not_reversible", "The returned credit has already been used, carried forward or restored")
			return
		}
	}
	revision := current.Revision + 1
	if delta != [3]int{} {
		var id string
		id, err = insertMovement(ctx, tx, target.packageID, "booking:"+bookingID+":correction:"+strconv.Itoa(revision), delta[0], 0, delta[1], delta[2], s.User.ID, "Attendance correction", now)
		movementID = &id
	}
	correctionID, idErr := RandomToken()
	if err == nil {
		err = idErr
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO attendance_corrections(id,booking_id,revision,from_status,to_status,from_outcome,to_outcome,reason,actor_id,movement_id,idempotency_key,request,corrected_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, correctionID, bookingID, revision, current.Status, in.Status, from, to,
			in.Reason, s.User.ID, movementID, in.IdempotencyKey, request, now)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE booking_attendance SET status=$2,credit_outcome=$3,revision=$4 WHERE booking_id=$1", bookingID, in.Status, to, revision)
	}
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "attendance.corrected", bookingID, map[string]any{
			"classId": r.PathValue("id"), "studentId": target.studentID, "creditPackageId": target.packageID, "revision": revision,
			"fromStatus": current.Status, "toStatus": in.Status, "fromOutcome": from, "toOutcome": to, "movementId": movementID, "reason": in.Reason,
		})
	}
	if err != nil {
		internal(w, err)
		return
	}
	respondAttendance(w, r, tx, s, bookingID)
}

func respondAttendance(w http.ResponseWriter, r *http.Request, tx pgx.Tx, s Session, bookingID string) {
	var current attendanceScan
	var evidence AttendanceEvidence
	fields := append(current.fields(), &evidence.RecordedBy, &evidence.HasAudit)
	if err := tx.QueryRow(r.Context(), "SELECT "+attendanceColumns+",marker.name,"+attendanceAuditExists+
		" FROM booking_attendance ba JOIN users marker ON marker.id=ba.actor_id WHERE ba.booking_id=$1", bookingID).Scan(fields...); err != nil {
		internal(w, err)
		return
	}
	attendance := current.value()
	if s.User.Role == "admin" {
		attendance.Evidence = &evidence
	}
	financeCommit(w, r, tx, 200, attendance)
}

func hasBookingReservation(ctx context.Context, tx pgx.Tx, packageID, bookingID string) (bool, error) {
	var reserved bool
	err := tx.QueryRow(ctx, `SELECT COALESCE(sum(reserved),0)>0
		AND COALESCE(bool_or(source=$2 AND available=-1 AND reserved=1 AND consumed=0 AND expired=0),false)
		FROM credit_movements WHERE package_id=$1`, packageID, "booking:"+bookingID+":reserved").Scan(&reserved)
	return reserved, err
}
