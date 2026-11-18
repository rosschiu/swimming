package app

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestIntegrationMissingAttendance(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	base := testHandler(db)
	f := prepareScheduling(t, base)
	parent := login(t, base, "parent@swim.test")
	coach := login(t, base, "coach@swim.test")
	student := login(t, base, "student@swim.test")
	earlier := createBookingClass(t, f, "2030-02-06", "10:00", "11:00", 5)
	boundary := createBookingClass(t, f, "2030-02-07", "10:00", "11:00", 5)
	ongoing := createBookingClass(t, f, "2030-02-07", "11:00", "12:00", 5)
	future := createBookingClass(t, f, "2030-02-08", "10:00", "11:00", 5)
	createBookingClass(t, f, "2030-02-05", "10:00", "11:00", 5) // Empty roster.
	seedBookingCredit(t, db, "missing-attendance-amy", "amy", "parent", 5, nil)
	seedBookingCredit(t, db, "missing-attendance-ben", "ben", "parent", 1, nil)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE students SET level='Beginner' WHERE id='ben'")
	expectStatus(t, request(t, base, "PUT", "/api/v1/students/ben/relationships/parent", map[string]string{"kind": "guardian"}, f.admin), 200)
	book := func(studentID string, class ClassSession) StudentBooking {
		t.Helper()
		w := request(t, base, "POST", "/api/v1/students/"+studentID+"/bookings", bookingBody(class, "missing-attendance-"+studentID, class.ID+"-"+studentID), parent)
		expectStatus(t, w, 201)
		return decodeBody[StudentBooking](t, w)
	}
	book("amy", earlier)
	amy := book("amy", boundary)
	ben := book("ben", boundary)
	book("amy", ongoing)
	book("amy", future)

	// A shared end instant proves that the ID breaks cursor ties. A short class can end before attendance opens.
	exec("UPDATE class_sessions SET starts_at=$2::timestamptz-interval '5 minutes',ends_at=$2 WHERE id=$1", earlier.ID, boundary.EndsAt)
	exec("UPDATE sessions SET expires_at='2031-01-01'")
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return boundary.EndsAt }}
	h := http.NewServeMux()
	a.bookingRoutes(h)
	const reportPath = "/api/v1/admin/missing-attendance"
	const summaryPath = "/api/v1/admin/action-queue"
	h.HandleFunc("GET "+reportPath, a.auth(a.listMissingAttendance))
	h.HandleFunc("GET "+summaryPath, a.auth(a.getAdminActionQueue))

	for _, path := range []string{reportPath, summaryPath} {
		expectStatus(t, request(t, h, "GET", path, nil, authState{}), 401)
		for _, denied := range []authState{parent, coach, student} {
			expectStatus(t, request(t, h, "GET", path, nil, denied), 403)
		}
	}
	expectStatus(t, request(t, h, "GET", reportPath+"?limit=101", nil, f.admin), 422)
	expectStatus(t, request(t, h, "GET", reportPath+"?cursor=bad", nil, f.admin), 422)
	count := func(want int) {
		t.Helper()
		w := request(t, h, "GET", summaryPath, nil, f.admin)
		expectStatus(t, w, 200)
		if got := decodeBody[AdminActionQueue](t, w).ClassesMissingAttendance; got != want {
			t.Fatalf("missing attendance classes got %d, want %d", got, want)
		}
	}
	type page struct {
		Items      []missingAttendanceClass `json:"items"`
		NextCursor *string                  `json:"nextCursor"`
	}
	list := func(query string) page {
		t.Helper()
		w := request(t, h, "GET", reportPath+query, nil, f.admin)
		expectStatus(t, w, 200)
		return decodeBody[page](t, w)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := db.QueryRow(ctx, `SELECT concat_ws('/',
			(SELECT count(*) FROM bookings),(SELECT count(*) FROM booking_attendance),
			(SELECT count(*) FROM credit_movements),(SELECT count(*) FROM audit_events),
			(SELECT count(*) FROM notifications))`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	count(2)
	p1 := list("?limit=1")
	if len(p1.Items) != 1 || p1.NextCursor == nil {
		t.Fatalf("first page %+v", p1)
	}
	p2 := list("?limit=1&cursor=" + url.QueryEscape(*p1.NextCursor))
	if len(p2.Items) != 1 || p2.NextCursor != nil || p1.Items[0].ID <= p2.Items[0].ID {
		t.Fatalf("tied pages %+v and %+v", p1, p2)
	}
	if after := snapshot(); after != before {
		t.Fatalf("report changed persisted state from %s to %s", before, after)
	}
	for _, item := range []missingAttendanceClass{p1.Items[0], p2.Items[0]} {
		if item.ID == earlier.ID && item.UnmarkedBookings == 1 {
			continue
		}
		if item.ID != boundary.ID || item.UnmarkedBookings != 2 {
			t.Fatalf("unexpected class %+v", item)
		}
	}
	rosterPath := "/api/v1/classes/" + earlier.ID + "/roster"
	w := request(t, h, "GET", rosterPath, nil, coach)
	expectStatus(t, w, 200)
	if roster := decodeBody[ClassRoster](t, w); !roster.AttendanceOpensAt.After(boundary.EndsAt) {
		t.Fatalf("short-ended class attendance unexpectedly open: %+v", roster)
	}
	for i, booking := range []StudentBooking{amy, ben} {
		expectStatus(t, request(t, h, "PUT", "/api/v1/classes/"+boundary.ID+"/roster/"+booking.ID+"/attendance", nil, coach), 200)
		if i == 0 {
			count(2)
			partial := list("")
			if len(partial.Items) != 2 || partial.Items[0].UnmarkedBookings != 1 || partial.Items[1].UnmarkedBookings != 1 {
				t.Fatalf("partially marked class should remain with one unmarked booking: %+v", partial)
			}
		}
	}
	count(1)
	p1 = list("")
	if len(p1.Items) != 1 || p1.Items[0].ID != earlier.ID {
		t.Fatalf("fully marked roster still in queue %+v", p1)
	}
	exec("UPDATE users SET active=false WHERE id='admin'")
	expectStatus(t, request(t, h, "GET", reportPath, nil, f.admin), 401)
	expectStatus(t, request(t, h, "GET", summaryPath, nil, f.admin), 401)
}
