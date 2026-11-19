package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type schoolCancellationInput struct {
	Reason         string            `json:"reason"`
	Version        int               `json:"version"`
	IdempotencyKey string            `json:"idempotencyKey"`
	Expiries       map[string]string `json:"expiries"`
}

// SchoolCancellationResult reports what one cancellation changed.
type SchoolCancellationResult struct {
	Lesson            ClassSession `json:"lesson"`
	Reason            string       `json:"reason"`
	CancelledAt       time.Time    `json:"cancelledAt"`
	CancelledBookings int          `json:"cancelledBookings"`
	RestoredCredits   int          `json:"restoredCredits"`
	ClosedRequests    int          `json:"closedRequests"`
}

func (a *API) schoolCancellationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/classes/{id}/cancellation", a.auth(a.cancelClass))
}

type cancelledReservation struct {
	bookingID, studentID, packageID string
	expiry                          *time.Time
	policy                          []byte
}

// cancelClass cancels one upcoming occurrence for the whole school. Every reservation returns its credit to the
// funding package; when that package has already expired the administrator supplies a new expiry and the credit is
// restored into a new package, keeping the original ledger trace. Started lessons belong to attendance corrections.
func (a *API) cancelClass(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var in schoolCancellationInput
	if !decode(w, r, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || !utf8.ValidString(in.Reason) || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsRune(in.Reason, 0) || in.Version < 1 || !validFinanceKey(in.IdempotencyKey) {
		fail(w, 422, "validation_error", "Provide a reason of 1–500 characters, the reviewed class version and a request key")
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
	request, _ := json.Marshal(map[string]any{"classId": classID, "reason": in.Reason, "version": in.Version, "expiries": in.Expiries})
	var replayClass string
	var same bool
	err = tx.QueryRow(ctx, "SELECT class_id,request=$3::jsonb FROM class_cancellations WHERE actor_id=$1 AND idempotency_key=$2", s.User.ID, in.IdempotencyKey, request).Scan(&replayClass, &same)
	if err == nil {
		if replayClass != classID || !same {
			fail(w, 409, "conflict", "Request key already used with different cancellation details")
			return
		}
		a.respondClassCancellation(w, r, tx, classID)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	lesson, err := scanClass(tx.QueryRow(ctx, "SELECT "+classColumns+classJoin+" WHERE c.id=$1 FOR UPDATE OF c", classID))
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
	case lesson.CancelledAt != nil:
		fail(w, 409, "class_cancelled", "This lesson is already cancelled")
		return
	case lesson.Version != in.Version:
		fail(w, 409, "version_conflict", "The lesson changed; review the impact again")
		return
	case !now.Before(lesson.StartsAt):
		fail(w, 409, "class_started", "Started lessons are handled through attendance corrections")
		return
	}
	rows, err := tx.Query(ctx, `SELECT b.id,b.student_id,b.credit_package_id,cp.expires_on,cp.policy FROM bookings b
		JOIN credit_packages cp ON cp.id=b.credit_package_id AND cp.student_id=b.student_id
		WHERE b.class_id=$1 AND b.status='reserved' ORDER BY b.id FOR UPDATE OF b,cp`, classID)
	if err != nil {
		internal(w, err)
		return
	}
	reservations, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (cancelledReservation, error) {
		var x cancelledReservation
		err := row.Scan(&x.bookingID, &x.studentID, &x.packageID, &x.expiry, &x.policy)
		return x, err
	})
	if err != nil {
		internal(w, err)
		return
	}
	today := now.In(hongKong).Format("2006-01-02")
	missing := []string{}
	for _, x := range reservations {
		expired := x.expiry != nil && !now.Before(packageExpiryCutoff(*x.expiry))
		value, given := in.Expiries[x.bookingID]
		if expired && (!given || !validFutureDate(value, today)) {
			missing = append(missing, x.bookingID)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		respond(w, 409, map[string]any{"error": map[string]any{"code": "expiry_required",
			"message": "Enter a new expiry, today or later, for each returned credit whose package has expired",
			"details": map[string]any{"bookingIds": missing}}})
		return
	}
	snapshot, _ := json.Marshal(lesson)
	restored := 0
	for _, x := range reservations {
		if err = a.releaseForSchoolCancellation(ctx, tx, s.User.ID, in.Reason, x, in.Expiries[x.bookingID], snapshot, now); err != nil {
			internal(w, err)
			return
		}
		if x.expiry != nil && !now.Before(packageExpiryCutoff(*x.expiry)) {
			restored++
		}
	}
	// Requests moving into this lesson keep their original booking; requests out of it lose their booking.
	closed, err := tx.Query(ctx, `UPDATE lesson_change_requests SET status='closed',decided_by=$2,decided_at=$3,
		decision_reason=CASE WHEN target_class_id=$1 THEN 'School cancelled the requested lesson' ELSE 'School cancelled the lesson' END
		WHERE status='pending' AND (target_class_id=$1 OR original_class_id=$1) RETURNING id`, classID, s.User.ID, now)
	if err != nil {
		internal(w, err)
		return
	}
	closedIDs, err := pgx.CollectRows(closed, pgx.RowTo[string])
	for _, id := range closedIDs {
		if err == nil {
			err = audit(ctx, tx, s.User.ID, "lesson_change.closed", id, map[string]any{"classId": classID, "reason": "School cancellation"})
		}
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE class_sessions SET cancelled_at=$2,version=version+1 WHERE id=$1", classID, now)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "INSERT INTO class_cancellations(class_id,actor_id,reason,cancelled_at,idempotency_key,request) VALUES($1,$2,$3,$4,$5,$6)",
			classID, s.User.ID, in.Reason, now, in.IdempotencyKey, request)
	}
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "class.cancelled", classID, map[string]any{"reason": in.Reason, "bookings": len(reservations),
			"restoredCredits": restored, "closedRequests": len(closedIDs), "expiries": in.Expiries})
	}
	if err == nil {
		err = a.createClassCancellationNotices(ctx, tx, lesson, reservations)
	}
	if err != nil {
		internal(w, err)
		return
	}
	a.respondClassCancellation(w, r, tx, classID)
}

func validFutureDate(value, today string) bool {
	date, err := time.ParseInLocation("2006-01-02", value, hongKong)
	return err == nil && date.Format("2006-01-02") == value && value >= today
}

func (a *API) releaseForSchoolCancellation(ctx context.Context, tx pgx.Tx, actorID, reason string, x cancelledReservation, newExpiry string, snapshot []byte, now time.Time) error {
	// School cancellation always returns the entitlement; an expired package's unit is restored with a new expiry.
	expired := x.expiry != nil && !now.Before(packageExpiryCutoff(*x.expiry))
	outcome := "retained"
	if expired {
		outcome = "expired"
	}
	available, _, expiredUnits := outcomeMovement(outcome)
	movementID, err := insertMovement(ctx, tx, x.packageID, "booking:"+x.bookingID+":school-cancelled", available, -1, 0, expiredUnits, actorID, "Lesson cancelled by the school", now)
	if err == nil && expired {
		// Restore the returned unit into a new package with the administrator's expiry, as an audited recovery.
		err = restoreCredit(ctx, tx, actorID, x.studentID, x.packageID, "School cancellation: "+reason, newExpiry, x.policy, "school-cancel:"+x.bookingID, now)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE bookings SET status='cancelled' WHERE id=$1", x.bookingID)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO booking_cancellations(booking_id,actor_id,cancelled_at,timing,credit_outcome,movement_id,class_snapshot,policy,cutoff_hours,idempotency_key,request,cancelled_by_school)
			VALUES($1,$2,$3,NULL,'retained',$4,$5,$6,0,$7,'{}',true)`, x.bookingID, actorID, now, movementID, snapshot, x.policy, "class:"+x.bookingID)
	}
	if err == nil {
		err = audit(ctx, tx, actorID, "booking.cancelled", x.bookingID, map[string]any{"studentId": x.studentID, "creditPackageId": x.packageID,
			"movementId": movementID, "bySchool": true, "creditOutcome": outcome, "restored": expired})
	}
	return err
}

// restoreCredit moves one expired unit into a new package with a new expiry, recorded like a coach recovery.
func restoreCredit(ctx context.Context, tx pgx.Tx, actorID, studentID, sourceID, reason, expiresOn string, policy []byte, key string, now time.Time) error {
	id, err := RandomToken()
	if err != nil {
		return err
	}
	destination, err := RandomToken()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_packages(id,purchase_id,grant_id,student_id,credits,expires_on,policy,created_at,source_package_id)
		SELECT $1,purchase_id,grant_id,student_id,1,$3,$4,$5,id FROM credit_packages WHERE id=$2`, destination, sourceID, expiresOn, policy, now); err != nil {
		return err
	}
	request, _ := json.Marshal(map[string]string{"source": key})
	if _, err = tx.Exec(ctx, `INSERT INTO credit_recoveries(id,student_id,source_package_id,destination_package_id,quantity,reason,expires_on,actor_id,idempotency_key,request,created_at)
		VALUES($1,$2,$3,$4,1,$5,$6,$7,$8,$9,$10)`, id, studentID, sourceID, destination, reason, expiresOn, actorID, key, request, now); err != nil {
		return err
	}
	if _, err = insertMovement(ctx, tx, sourceID, "recovery:"+id+":out", 0, 0, 0, -1, actorID, reason, now); err != nil {
		return err
	}
	_, err = insertMovement(ctx, tx, destination, "recovery:"+id+":in", 1, 0, 0, 0, actorID, reason, now)
	return err
}

func (a *API) respondClassCancellation(w http.ResponseWriter, r *http.Request, tx pgx.Tx, classID string) {
	ctx := r.Context()
	var x SchoolCancellationResult
	lesson, err := scanClass(tx.QueryRow(ctx, "SELECT "+classColumns+classJoin+" WHERE c.id=$1", classID))
	if err == nil {
		x.Lesson = lesson
		err = tx.QueryRow(ctx, `SELECT cc.reason,cc.cancelled_at,
			(SELECT count(*) FROM booking_cancellations bc JOIN bookings b ON b.id=bc.booking_id WHERE b.class_id=cc.class_id AND bc.cancelled_by_school),
			(SELECT count(*) FROM credit_recoveries cr JOIN bookings b ON cr.idempotency_key='school-cancel:'||b.id WHERE b.class_id=cc.class_id),
			(SELECT count(*) FROM lesson_change_requests l WHERE l.status='closed' AND l.decided_at=cc.cancelled_at AND (l.target_class_id=cc.class_id OR l.original_class_id=cc.class_id))
			FROM class_cancellations cc WHERE cc.class_id=$1`, classID).Scan(&x.Reason, &x.CancelledAt, &x.CancelledBookings, &x.RestoredCredits, &x.ClosedRequests)
	}
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, x)
}

// createClassCancellationNotices tells each affected family in-app, with generic email when the school enables it and
// the recipient allows service email, and gives the class coach one in-app notice per affected swimmer.
func (a *API) createClassCancellationNotices(ctx context.Context, tx pgx.Tx, lesson ClassSession, reservations []cancelledReservation) error {
	for _, x := range reservations {
		if err := a.createServiceNotices(ctx, tx, "class.cancelled", x.studentID, x.bookingID, lesson.CoachID); err != nil {
			return err
		}
	}
	return nil
}
