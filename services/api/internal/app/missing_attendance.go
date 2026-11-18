package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const unmarkedReservations = `FROM bookings b WHERE b.class_id=c.id AND b.status='reserved'
	AND NOT EXISTS (SELECT 1 FROM booking_attendance ba WHERE ba.booking_id=b.id)`

type missingAttendanceClass struct {
	ID               string    `json:"id"`
	ClassTypeName    string    `json:"classTypeName"`
	CoachName        string    `json:"coachName"`
	VenueName        string    `json:"venueName"`
	StartsAt         time.Time `json:"startsAt"`
	EndsAt           time.Time `json:"endsAt"`
	UnmarkedBookings int       `json:"unmarkedBookings"`
}

func (a *API) listMissingAttendance(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	limit, cursor, ok := timestampPagingFor(w, r, func(id string) bool {
		return len(id) > 0 && len(id) <= 100 && !strings.ContainsRune(id, 0)
	})
	if !ok {
		return
	}
	ctx := r.Context()
	tx, ok := a.beginBookingRead(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT c.id,t.name,u.name,v.name,c.starts_at,c.ends_at,missing.total`+classJoin+`
		CROSS JOIN LATERAL (SELECT count(*) AS total `+unmarkedReservations+`) missing
		WHERE c.ends_at<=$1 AND ($2='' OR (c.ends_at,c.id)<($3,$2)) AND missing.total>0
		ORDER BY c.ends_at DESC,c.id DESC LIMIT $4`, a.now(), cursor.ID, cursor.CreatedAt, limit+1)
	if err != nil {
		internal(w, err)
		return
	}
	items, err := pgx.AppendRows([]missingAttendanceClass{}, rows, func(row pgx.CollectableRow) (missingAttendanceClass, error) {
		var item missingAttendanceClass
		err := row.Scan(&item.ID, &item.ClassTypeName, &item.CoachName, &item.VenueName, &item.StartsAt, &item.EndsAt, &item.UnmarkedBookings)
		return item, err
	})
	if err != nil {
		internal(w, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		value := encodeTimestampCursor(last.EndsAt, last.ID)
		next = &value
	}
	financeCommit(w, r, tx, 200, map[string]any{"items": items, "nextCursor": next})
}
