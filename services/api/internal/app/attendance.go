package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

const attendanceOpening = "c.starts_at + ls.attendance_open_minutes * interval '1 minute'"

type BookingAttendance struct {
	Status   string    `json:"status"`
	MarkedAt time.Time `json:"markedAt"`
}

func presentAttendance(markedAt *time.Time) *BookingAttendance {
	if markedAt == nil {
		return nil
	}
	return &BookingAttendance{Status: "present", MarkedAt: markedAt.UTC()}
}

func (a *API) markPresent(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "admin" && s.User.Role != "coach" {
		fail(w, 403, "forbidden", "Class coach or administrator access is required")
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
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role=$2 AND active FOR SHARE)", s.User.ID, s.User.Role).Scan(&active)
	if err != nil {
		internal(w, err)
		return
	}
	if !active {
		fail(w, 401, "unauthenticated", "Sign in again")
		return
	}

	classID, bookingID := r.PathValue("id"), r.PathValue("bookingId")
	var opensAt time.Time
	var studentID, packageID string
	var markedAt *time.Time
	err = tx.QueryRow(ctx, "SELECT b.student_id,b.credit_package_id,ba.marked_at,"+attendanceOpening+`
		FROM class_sessions c CROSS JOIN lesson_settings ls
		JOIN bookings b ON b.id=$2 AND b.status='reserved'
		LEFT JOIN booking_attendance ba ON ba.booking_id=b.id
		WHERE c.id=$1 AND b.class_id=c.id AND ($3='admin' OR c.coach_id=$4)
		FOR UPDATE OF c,b FOR SHARE OF ls`, classID, bookingID, s.User.Role, s.User.ID).
		Scan(&studentID, &packageID, &markedAt, &opensAt)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Booking not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if markedAt != nil {
		financeCommit(w, r, tx, 200, presentAttendance(markedAt))
		return
	}

	if _, err = lockBookingPackage(ctx, tx, packageID, studentID); errors.Is(err, pgx.ErrNoRows) {
		fail(w, 409, "credit_unavailable", "Booking credit package is unavailable")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	if now.Before(opensAt) {
		fail(w, 409, "attendance_not_open", "Attendance is not open for this class")
		return
	}

	source := "booking:" + bookingID
	reserved, err := hasBookingReservation(ctx, tx, packageID, bookingID)
	if err != nil {
		internal(w, err)
		return
	}
	if !reserved {
		fail(w, 409, "credit_unavailable", "Booking does not have a reserved credit")
		return
	}

	movementID, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
		VALUES($1,$2,$3,0,-1,1,0,$4,'Class attendance', $5)`, movementID, packageID, source+":present", s.User.ID, now)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO booking_attendance(booking_id,status,actor_id,marked_at,movement_id)
			VALUES($1,'present',$2,$3,$4)`, bookingID, s.User.ID, now, movementID)
	}
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "attendance.present", bookingID, map[string]any{
			"classId": classID, "studentId": studentID, "creditPackageId": packageID, "movementId": movementID, "markedAt": now,
		})
	}
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, presentAttendance(&now))
}

func hasBookingReservation(ctx context.Context, tx pgx.Tx, packageID, bookingID string) (bool, error) {
	var reserved bool
	err := tx.QueryRow(ctx, `SELECT COALESCE(sum(reserved),0)>0
		AND COALESCE(bool_or(source=$2 AND available=-1 AND reserved=1 AND consumed=0 AND expired=0),false)
		FROM credit_movements WHERE package_id=$1`, packageID, "booking:"+bookingID+":reserved").Scan(&reserved)
	return reserved, err
}
