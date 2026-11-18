package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type StudentBooking struct {
	ID         string             `json:"id"`
	StudentID  string             `json:"studentId"`
	Status     string             `json:"status"`
	CreatedAt  time.Time          `json:"createdAt"`
	Attendance *BookingAttendance `json:"attendance,omitempty"`
	Class      ClassSession       `json:"class"`
}

type RosterEntry struct {
	BookingID   string             `json:"bookingId"`
	StudentID   string             `json:"studentId"`
	StudentName string             `json:"studentName"`
	Level       string             `json:"level"`
	Status      string             `json:"status"`
	Attendance  *BookingAttendance `json:"attendance,omitempty"`
}

type ClassRoster struct {
	AttendanceOpensAt time.Time     `json:"attendanceOpensAt"`
	ClassID           string        `json:"classId"`
	Items             []RosterEntry `json:"items"`
}

func (a *API) bookingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/students/{id}/enrolments", a.auth(a.createEnrolment))
	mux.HandleFunc("GET /api/v1/students/{id}/bookings", a.auth(a.listStudentBookings))
	mux.HandleFunc("POST /api/v1/students/{id}/bookings", a.auth(a.createBooking))
	mux.HandleFunc("GET /api/v1/classes/{id}/roster", a.auth(a.getClassRoster))
	mux.HandleFunc("PUT /api/v1/classes/{id}/roster/{bookingId}/attendance", a.auth(a.markPresent))
}

func (a *API) beginBookingRead(w http.ResponseWriter, r *http.Request, s Session) (pgx.Tx, bool) {
	ctx := r.Context()
	// Scope and records must come from the same snapshot during a reassignment or revocation.
	tx, err := a.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		internal(w, err)
		return nil, false
	}
	var active bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role=$2 AND active)", s.User.ID, s.User.Role).Scan(&active)
	if err != nil {
		tx.Rollback(ctx)
		internal(w, err)
		return nil, false
	}
	if !active {
		tx.Rollback(ctx)
		fail(w, 401, "unauthenticated", "Sign in again")
		return nil, false
	}
	return tx, true
}

func (a *API) listStudentBookings(w http.ResponseWriter, r *http.Request, s Session) {
	switch s.User.Role {
	case "admin", "parent", "student":
	default:
		fail(w, 403, "forbidden", "Guardian, student or administrator access is required")
		return
	}
	start, end, err := dateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		fail(w, 422, "validation_error", "Provide valid Hong Kong dates spanning 1–93 days")
		return
	}
	ctx := r.Context()
	tx, ok := a.beginBookingRead(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	var scoped bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM students st WHERE "+studentScope+" AND st.id=$3)", s.User.Role, s.User.ID, r.PathValue("id")).Scan(&scoped)
	if err != nil {
		internal(w, err)
		return
	}
	if !scoped {
		fail(w, 404, "not_found", "Student not found")
		return
	}
	rows, err := tx.Query(ctx, "SELECT b.id,b.student_id,b.status,b.created_at,ba.marked_at,"+classColumns+classJoin+
		"JOIN bookings b ON b.class_id=c.id LEFT JOIN booking_attendance ba ON ba.booking_id=b.id WHERE b.student_id=$1 AND c.starts_at>=$2 AND c.starts_at<$3 ORDER BY c.starts_at,c.id,b.id", r.PathValue("id"), start, end)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []StudentBooking{}
	for rows.Next() {
		booking, err := scanStudentBooking(rows)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, booking)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}

func scanStudentBooking(row pgx.Row) (StudentBooking, error) {
	var booking StudentBooking
	c := &booking.Class
	var markedAt *time.Time
	err := row.Scan(&booking.ID, &booking.StudentID, &booking.Status, &booking.CreatedAt, &markedAt,
		&c.ID, &c.VenueID, &c.ClassTypeID, &c.CoachID, &c.StartsAt, &c.EndsAt, &c.Capacity,
		&c.VenueName, &c.Region, &c.ClassTypeName, &c.Level, &c.CoachName, &c.SeriesID, &c.Version, &c.ClassTypeVersion)
	booking.Attendance = presentAttendance(markedAt)
	return booking, err
}

func (a *API) getClassRoster(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "admin" && s.User.Role != "coach" {
		fail(w, 403, "forbidden", "Class coach or administrator access is required")
		return
	}
	ctx := r.Context()
	tx, ok := a.beginBookingRead(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	id := r.PathValue("id")
	roster := ClassRoster{ClassID: id, Items: []RosterEntry{}}
	err := tx.QueryRow(ctx, "SELECT "+attendanceOpening+" FROM class_sessions c CROSS JOIN lesson_settings ls WHERE c.id=$1 AND ($2='admin' OR c.coach_id=$3)",
		id, s.User.Role, s.User.ID).Scan(&roster.AttendanceOpensAt)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Class not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	rows, err := tx.Query(ctx, `SELECT b.id,b.student_id,st.name,st.level,b.status,ba.marked_at FROM bookings b
     JOIN students st ON st.id=b.student_id LEFT JOIN booking_attendance ba ON ba.booking_id=b.id
     WHERE b.class_id=$1 ORDER BY st.name,st.id,b.id`, id)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var entry RosterEntry
		var markedAt *time.Time
		if err = rows.Scan(&entry.BookingID, &entry.StudentID, &entry.StudentName, &entry.Level, &entry.Status, &markedAt); err != nil {
			internal(w, err)
			return
		}
		entry.Attendance = presentAttendance(markedAt)
		roster.Items = append(roster.Items, entry)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, roster)
}
