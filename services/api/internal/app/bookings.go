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
	// Cancellation keeps the lesson as reviewed when the guardian cancelled, even if the class is later edited.
	Cancellation *BookingCancellation `json:"cancellation,omitempty"`
	Class        ClassSession         `json:"class"`
}

type StudentBookingDetail struct {
	StudentBooking
	StudentName string `json:"studentName"`
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
	AttendanceOpensAt  time.Time     `json:"attendanceOpensAt"`
	AbsenceOpensAt     time.Time     `json:"absenceOpensAt"`
	CorrectionClosesAt time.Time     `json:"correctionClosesAt"`
	ClassID            string        `json:"classId"`
	Items              []RosterEntry `json:"items"`
}

func (a *API) bookingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings/booking-validity", a.auth(a.bookingValiditySettings))
	mux.HandleFunc("PATCH /api/v1/settings/booking-validity", a.auth(a.bookingValiditySettings))
	mux.HandleFunc("POST /api/v1/students/{id}/enrolments", a.auth(a.createEnrolment))
	mux.HandleFunc("GET /api/v1/students/{id}/enrolments", a.auth(a.listStudentEnrolments))
	mux.HandleFunc("POST /api/v1/students/{id}/enrolments/{enrolmentId}/renewals", a.auth(a.createEnrolment))
	mux.HandleFunc("GET /api/v1/students/{id}/bookings", a.auth(a.listStudentBookings))
	mux.HandleFunc("POST /api/v1/students/{id}/bookings", a.auth(a.createBooking))
	mux.HandleFunc("GET /api/v1/students/{id}/bookings/{bookingId}", a.auth(a.getStudentBooking))
	mux.HandleFunc("GET /api/v1/classes/{id}/cancellation-impact", a.auth(a.getCancellationImpact))
	mux.HandleFunc("GET /api/v1/home", a.auth(a.getHome))
	mux.HandleFunc("GET /api/v1/classes/{id}/roster", a.auth(a.getClassRoster))
	mux.HandleFunc("PUT /api/v1/classes/{id}/roster/{bookingId}/attendance", a.auth(a.markAttendance))
	mux.HandleFunc("POST /api/v1/classes/{id}/roster/{bookingId}/attendance/corrections", a.auth(a.correctAttendance))
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
	rows, err := tx.Query(ctx, "SELECT "+studentBookingColumns+studentBookingJoin+
		"WHERE b.student_id=$1 AND c.starts_at>=$2 AND c.starts_at<$3 ORDER BY c.starts_at,c.id,b.id", r.PathValue("id"), start, end)
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
	rows.Close()
	for i := range items {
		if items[i].Status == "cancelled" {
			if items[i].Cancellation, err = loadBookingCancellation(ctx, tx, items[i].ID); err != nil {
				internal(w, err)
				return
			}
		}
	}
	respond(w, 200, map[string]any{"items": items})
}

const studentBookingColumns = "b.id,b.student_id,b.status,b.created_at," + attendanceColumns + "," + classColumns
const studentBookingJoin = classJoin + "JOIN bookings b ON b.class_id=c.id LEFT JOIN booking_attendance ba ON ba.booking_id=b.id "

func (a *API) getStudentBooking(w http.ResponseWriter, r *http.Request, s Session) {
	switch s.User.Role {
	case "admin", "parent", "student":
	default:
		fail(w, 403, "forbidden", "Guardian, student or administrator access is required")
		return
	}
	ctx := r.Context()
	tx, ok := a.beginBookingRead(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	var detail StudentBookingDetail
	booking, err := scanStudentBooking(tx.QueryRow(ctx, "SELECT "+studentBookingColumns+",st.name"+studentBookingJoin+
		"JOIN students st ON st.id=b.student_id WHERE "+studentScope+" AND st.id=$3 AND b.id=$4",
		s.User.Role, s.User.ID, r.PathValue("id"), r.PathValue("bookingId")), &detail.StudentName)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Booking not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if booking.Status == "cancelled" {
		if booking.Cancellation, err = loadBookingCancellation(ctx, tx, booking.ID); err != nil {
			internal(w, err)
			return
		}
	}
	detail.StudentBooking = booking
	respond(w, 200, detail)
}

func scanStudentBooking(row pgx.Row, extra ...any) (StudentBooking, error) {
	var booking StudentBooking
	c := &booking.Class
	var attendance attendanceScan
	fields := append([]any{&booking.ID, &booking.StudentID, &booking.Status, &booking.CreatedAt}, attendance.fields()...)
	fields = append(fields, &c.ID, &c.VenueID, &c.ClassTypeID, &c.CoachID, &c.StartsAt, &c.EndsAt, &c.Capacity,
		&c.VenueName, &c.Region, &c.ClassTypeName, &c.Level, &c.CoachName, &c.SeriesID, &c.Version, &c.ClassTypeVersion)
	err := row.Scan(append(fields, extra...)...)
	booking.Attendance = attendance.value()
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
	err := tx.QueryRow(ctx, "SELECT "+attendanceOpening+",c.ends_at,c.ends_at + ls.correction_window_days * interval '1 day' FROM class_sessions c CROSS JOIN lesson_settings ls WHERE c.id=$1 AND ($2='admin' OR c.coach_id=$3)",
		id, s.User.Role, s.User.ID).Scan(&roster.AttendanceOpensAt, &roster.AbsenceOpensAt, &roster.CorrectionClosesAt)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Class not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	rows, err := tx.Query(ctx, `SELECT b.id,b.student_id,st.name,st.level,b.status,`+attendanceColumns+`,marker.name,
		CASE WHEN marker.id IS NOT NULL THEN `+attendanceAuditExists+` ELSE false END
		FROM bookings b JOIN students st ON st.id=b.student_id
		LEFT JOIN booking_attendance ba ON ba.booking_id=b.id
		LEFT JOIN users marker ON $2 AND marker.id=ba.actor_id
		WHERE b.class_id=$1 AND b.status='reserved' ORDER BY st.name,st.id,b.id`, id, s.User.Role == "admin")
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var entry RosterEntry
		var attendance attendanceScan
		var recordedBy *string
		var hasAudit bool
		fields := append([]any{&entry.BookingID, &entry.StudentID, &entry.StudentName, &entry.Level, &entry.Status}, attendance.fields()...)
		if err = rows.Scan(append(fields, &recordedBy, &hasAudit)...); err != nil {
			internal(w, err)
			return
		}
		entry.Attendance = attendance.value()
		if recordedBy != nil {
			entry.Attendance.Evidence = &AttendanceEvidence{RecordedBy: *recordedBy, HasAudit: hasAudit}
		}
		roster.Items = append(roster.Items, entry)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, roster)
}
