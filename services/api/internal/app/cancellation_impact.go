package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type cancellationImpactBooking struct {
	CreatedAt           time.Time  `json:"-"`
	ID                  string     `json:"bookingId"`
	StudentID           string     `json:"studentId"`
	StudentName         string     `json:"studentName"`
	EnrolmentID         *string    `json:"enrolmentId,omitempty"`
	PackageID           string     `json:"packageId"`
	ExpiresOn           *string    `json:"expiresOn"`
	ValidityRule        string     `json:"bookingValidityRule"`
	PackageExpired      bool       `json:"packageExpired"`
	PackageCoversLesson bool       `json:"packageCoversLesson"`
	AttendanceMarkedAt  *time.Time `json:"attendanceMarkedAt,omitempty"`
}

type cancellationImpactRequest struct {
	ID            string    `json:"id"`
	BookingID     string    `json:"bookingId"`
	StudentName   string    `json:"studentName"`
	OtherClassID  string    `json:"otherClassId"`
	OtherStartsAt time.Time `json:"otherStartsAt"`
	OtherEndsAt   time.Time `json:"otherEndsAt"`
	OtherVenue    string    `json:"otherVenue"`
	RequestedAt   time.Time `json:"requestedAt"`
}

type cancellationImpact struct {
	Lesson             ClassSession                `json:"lesson"`
	ReviewedAt         time.Time                   `json:"reviewedAt"`
	BookedSwimmers     int                         `json:"bookedSwimmers"`
	UnmarkedBookings   int                         `json:"unmarkedBookings"`
	RecordedAttendance int                         `json:"recordedAttendance"`
	PendingInbound     int                         `json:"pendingInbound"`
	PendingOutbound    int                         `json:"pendingOutbound"`
	Bookings           []cancellationImpactBooking `json:"bookings"`
	Requests           []cancellationImpactRequest `json:"requests"`
	NextCursor         *string                     `json:"nextCursor"`
}

func (a *API) getCancellationImpact(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "admin" {
		fail(w, 403, "forbidden", "Administrator access is required")
		return
	}
	section := r.URL.Query().Get("section")
	if section != "" && section != "bookings" && section != "inbound" && section != "outbound" {
		fail(w, 422, "validation_error", "Choose bookings, inbound or outbound")
		return
	}
	limit, cursor, ok := timestampPagingFor(w, r, validFinanceKey)
	if !ok {
		return
	}
	tx, ok := a.beginBookingRead(w, r, s)
	if !ok {
		return
	}
	ctx := r.Context()
	defer tx.Rollback(ctx)
	id := r.PathValue("id")
	result := cancellationImpact{
		ReviewedAt: a.now().UTC(),
		Bookings:   []cancellationImpactBooking{},
		Requests:   []cancellationImpactRequest{},
	}
	var err error
	result.Lesson, err = scanClass(tx.QueryRow(ctx, "SELECT "+classColumns+classJoin+" WHERE c.id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Class not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	err = tx.QueryRow(ctx, `SELECT count(*),count(a.booking_id),
		(SELECT count(*) FROM lesson_change_requests WHERE target_class_id=$1 AND status='pending'),
		(SELECT count(*) FROM lesson_change_requests WHERE original_class_id=$1 AND status='pending')
		FROM bookings b LEFT JOIN booking_attendance a ON a.booking_id=b.id WHERE b.class_id=$1 AND b.status='reserved'`,
		id).Scan(&result.BookedSwimmers, &result.RecordedAttendance, &result.PendingInbound, &result.PendingOutbound)
	if err != nil {
		internal(w, err)
		return
	}
	result.UnmarkedBookings = result.BookedSwimmers - result.RecordedAttendance
	if section == "" || section == "bookings" {
		rows, err := tx.Query(ctx, `SELECT b.id,b.student_id,s.name,b.enrolment_id,b.credit_package_id,
			p.expires_on,b.validity_rule,a.marked_at,b.created_at
			FROM bookings b JOIN students s ON s.id=b.student_id
			JOIN credit_packages p ON p.id=b.credit_package_id AND p.student_id=b.student_id
			LEFT JOIN booking_attendance a ON a.booking_id=b.id
			WHERE b.class_id=$1 AND b.status='reserved' AND ($2='' OR (b.created_at,b.id)<($3,$2))
			ORDER BY b.created_at DESC,b.id DESC LIMIT $4`, id, cursor.ID, cursor.CreatedAt, limit+1)
		if err != nil {
			internal(w, err)
			return
		}
		result.Bookings, err = pgx.AppendRows(result.Bookings, rows, func(row pgx.CollectableRow) (cancellationImpactBooking, error) {
			var item cancellationImpactBooking
			var expiry *time.Time
			if err := row.Scan(&item.ID, &item.StudentID, &item.StudentName, &item.EnrolmentID, &item.PackageID,
				&expiry, &item.ValidityRule, &item.AttendanceMarkedAt, &item.CreatedAt); err != nil {
				return item, err
			}
			if expiry != nil {
				date := expiry.Format("2006-01-02")
				item.ExpiresOn = &date
				item.PackageExpired = !result.ReviewedAt.Before(packageExpiryCutoff(*expiry))
			}
			item.PackageCoversLesson = bookingPackageCoversLesson(item.ValidityRule, result.Lesson, expiry)
			return item, nil
		})
		if err != nil {
			internal(w, err)
			return
		}
		if len(result.Bookings) > limit {
			result.Bookings = result.Bookings[:limit]
			last := result.Bookings[limit-1]
			value := encodeTimestampCursor(last.CreatedAt, last.ID)
			result.NextCursor = &value
		}
	} else {
		selected, other := "original_class_id", "target_class_id"
		if section == "inbound" {
			selected, other = other, selected
		}
		rows, err := tx.Query(ctx, `SELECT r.id,b.id,s.name,r.`+other+`,c.starts_at,c.ends_at,v.name,r.requested_at
			FROM lesson_change_requests r JOIN bookings b ON b.id=r.booking_id JOIN students s ON s.id=b.student_id
			JOIN class_sessions c ON c.id=r.`+other+` JOIN venues v ON v.id=c.venue_id
			WHERE r.`+selected+`=$1 AND r.status='pending' AND ($2='' OR (r.requested_at,r.id)<($3,$2))
			ORDER BY r.requested_at DESC,r.id DESC LIMIT $4`, id, cursor.ID, cursor.CreatedAt, limit+1)
		if err != nil {
			internal(w, err)
			return
		}
		result.Requests, err = pgx.AppendRows(result.Requests, rows, func(row pgx.CollectableRow) (cancellationImpactRequest, error) {
			var item cancellationImpactRequest
			err := row.Scan(&item.ID, &item.BookingID, &item.StudentName, &item.OtherClassID, &item.OtherStartsAt, &item.OtherEndsAt, &item.OtherVenue, &item.RequestedAt)
			return item, err
		})
		if err != nil {
			internal(w, err)
			return
		}
		if len(result.Requests) > limit {
			result.Requests = result.Requests[:limit]
			last := result.Requests[limit-1]
			value := encodeTimestampCursor(last.RequestedAt, last.ID)
			result.NextCursor = &value
		}
	}
	respond(w, 200, result)
}
