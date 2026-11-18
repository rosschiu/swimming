package app

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type HomeCredits struct {
	Available  int     `json:"available"`
	Reserved   int     `json:"reserved"`
	NextExpiry *string `json:"nextExpiry"`
}

type HomeStudent struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Level          string          `json:"level"`
	NextLesson     *StudentBooking `json:"nextLesson"`
	UpcomingCount  int             `json:"upcomingCount"`
	Credits        *HomeCredits    `json:"credits,omitempty"`
	PendingChanges int             `json:"pendingChanges"`
}

type HomeClass struct {
	Class    ClassSession `json:"class"`
	Booked   int          `json:"booked"`
	Unmarked int          `json:"unmarked"`
}

type Home struct {
	Role                 string            `json:"role"`
	Today                string            `json:"today"`
	Students             []HomeStudent     `json:"students"`
	Classes              []HomeClass       `json:"classes"`
	PendingLessonChanges int               `json:"pendingLessonChanges"`
	UnreadNotifications  int               `json:"unreadNotifications"`
	ActionQueue          *AdminActionQueue `json:"actionQueue,omitempty"`
}

// homeClassLimit bounds the coach and administrator lesson lists; the full schedule remains paged elsewhere.
const homeClassLimit = 20

func (a *API) getHome(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.beginBookingRead(w, r, s)
	if !ok {
		return
	}
	ctx := r.Context()
	defer tx.Rollback(ctx)
	now := a.now()
	local := now.In(hongKong)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, hongKong)
	home := Home{Role: s.User.Role, Today: today.Format("2006-01-02"), Students: []HomeStudent{}, Classes: []HomeClass{}}
	var err error
	switch s.User.Role {
	case "parent", "student":
		home.Students, err = homeStudents(ctx, tx, s, now, today)
		if err == nil {
			err = tx.QueryRow(ctx, "SELECT count(*)"+notificationJoin+" WHERE "+notificationScope+" AND n.read_at IS NULL", s.User.ID, s.User.Role).Scan(&home.UnreadNotifications)
		}
		for _, student := range home.Students {
			home.PendingLessonChanges += student.PendingChanges
		}
	case "coach":
		// Coaches see their own lessons from the start of today, including any still awaiting attendance.
		home.Classes, err = homeClasses(ctx, tx, "c.coach_id=$1 AND c.ends_at>$2", s.User.ID, today)
		if err == nil {
			err = tx.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM lesson_change_requests r JOIN class_sessions original ON original.id=r.original_class_id WHERE r.status='pending' AND original.coach_id=$1),
				(SELECT count(*)`+notificationJoin+` WHERE `+notificationScope+` AND n.read_at IS NULL)`, s.User.ID, s.User.Role).
				Scan(&home.PendingLessonChanges, &home.UnreadNotifications)
		}
	case "admin":
		home.Classes, err = homeClasses(ctx, tx, "c.ends_at>$1 AND c.starts_at<$2", today, today.AddDate(0, 0, 1))
		if err == nil {
			var queue AdminActionQueue
			err = tx.QueryRow(ctx, actionQueueCounts+" FROM users WHERE id=$1", s.User.ID, now).Scan(queue.fields()...)
			home.ActionQueue = &queue
			home.PendingLessonChanges = queue.PendingLessonChanges
		}
	default:
		fail(w, 403, "forbidden", "Account access is required")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, home)
}

func homeStudents(ctx context.Context, tx pgx.Tx, s Session, now, today time.Time) ([]HomeStudent, error) {
	rows, err := tx.Query(ctx, "SELECT st.id,st.name,st.level FROM students st WHERE "+studentScope+" ORDER BY st.name,st.id", s.User.Role, s.User.ID)
	if err != nil {
		return nil, err
	}
	students, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (HomeStudent, error) {
		var x HomeStudent
		err := row.Scan(&x.ID, &x.Name, &x.Level)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	for i := range students {
		x := &students[i]
		// An upcoming lesson is a reservation that has not ended and has no attendance outcome.
		upcoming := "FROM bookings b JOIN class_sessions c ON c.id=b.class_id LEFT JOIN booking_attendance ba ON ba.booking_id=b.id WHERE b.student_id=$1 AND b.status='reserved' AND ba.booking_id IS NULL AND c.ends_at>$2"
		if err = tx.QueryRow(ctx, "SELECT count(*) "+upcoming, x.ID, now).Scan(&x.UpcomingCount); err != nil {
			return nil, err
		}
		if x.UpcomingCount > 0 {
			booking, err := scanStudentBooking(tx.QueryRow(ctx, "SELECT "+studentBookingColumns+studentBookingJoin+
				"WHERE b.student_id=$1 AND b.status='reserved' AND ba.booking_id IS NULL AND c.ends_at>$2 ORDER BY c.starts_at,c.id,b.id LIMIT 1", x.ID, now))
			if err != nil {
				return nil, err
			}
			x.NextLesson = &booking
		}
		if s.User.Role != "parent" {
			continue
		}
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM lesson_change_requests r JOIN bookings b ON b.id=r.booking_id WHERE b.student_id=$1 AND r.status='pending'", x.ID).Scan(&x.PendingChanges); err != nil {
			return nil, err
		}
		// Balances are read without running expiry: a package past its Hong Kong expiry date counts as having no available credits.
		var credits HomeCredits
		if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN p.expires_on IS NULL OR p.expires_on>=$2::date THEN p.available ELSE 0 END),0)::int,
			COALESCE(sum(p.reserved),0)::int,
			min(p.expires_on) FILTER (WHERE p.available>0 AND p.expires_on>=$2::date)::text
			FROM (SELECT cp.expires_on,sum(m.available) AS available,sum(m.reserved) AS reserved
				FROM credit_packages cp JOIN credit_movements m ON m.package_id=cp.id WHERE cp.student_id=$1 GROUP BY cp.id) p`,
			x.ID, today.Format("2006-01-02")).Scan(&credits.Available, &credits.Reserved, &credits.NextExpiry); err != nil {
			return nil, err
		}
		x.Credits = &credits
	}
	return students, nil
}

func homeClasses(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]HomeClass, error) {
	rows, err := tx.Query(ctx, "SELECT "+classColumns+`,
		(SELECT count(*) FROM bookings b WHERE b.class_id=c.id AND b.status='reserved'),
		(SELECT count(*) FROM bookings b LEFT JOIN booking_attendance ba ON ba.booking_id=b.id WHERE b.class_id=c.id AND b.status='reserved' AND ba.booking_id IS NULL)`+
		classJoin+" WHERE "+where+" ORDER BY c.starts_at,c.id LIMIT "+strconv.Itoa(homeClassLimit), args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (HomeClass, error) {
		var x HomeClass
		c := &x.Class
		err := row.Scan(&c.ID, &c.VenueID, &c.ClassTypeID, &c.CoachID, &c.StartsAt, &c.EndsAt, &c.Capacity, &c.VenueName, &c.Region, &c.ClassTypeName, &c.Level, &c.CoachName, &c.SeriesID, &c.Version, &c.ClassTypeVersion, &x.Booked, &x.Unmarked)
		return x, err
	})
}
