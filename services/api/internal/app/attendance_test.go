package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegrationMarkPresent(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	class := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 1)
	later := createBookingClass(t, f, "2030-01-07", "11:00", "12:00", 1)
	coach := login(t, h, "coach@swim.test")
	parent := login(t, h, "parent@swim.test")
	student := login(t, h, "student@swim.test")
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Second Coach','coach',password_hash FROM users WHERE id='coach'")
	otherCoach := login(t, h, "coach2@swim.test")
	expiry := "2030-01-07"
	seedBookingCredit(t, db, "attendance-pack", "amy", "parent", 1, &expiry)
	seedBookingCredit(t, db, "late-attendance-pack", "amy", "parent", 1, &expiry)
	bookingURL := "/api/v1/students/amy/bookings"
	booking := bookingBody(class, "attendance-pack", "attendance")
	w := request(t, h, "POST", bookingURL, booking, parent)
	expectStatus(t, w, 201)
	reserved := decodeBody[StudentBooking](t, w)
	w = request(t, h, "POST", bookingURL, bookingBody(later, "late-attendance-pack", "late-attendance"), parent)
	expectStatus(t, w, 201)
	lateBooking := decodeBody[StudentBooking](t, w)
	rosterURL := "/api/v1/classes/" + class.ID + "/roster"
	path := rosterURL + "/" + reserved.ID + "/attendance"
	now := class.StartsAt.Add(15*time.Minute - time.Microsecond)
	exec("UPDATE sessions SET expires_at=$1", class.StartsAt.Add(7*24*time.Hour))

	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return now }}
	routes := http.NewServeMux()
	a.bookingRoutes(routes)
	a.lessonSettingsRoutes(routes)

	w = request(t, routes, "GET", rosterURL, nil, coach)
	expectStatus(t, w, 200)
	roster := decodeBody[ClassRoster](t, w)
	if !roster.AttendanceOpensAt.Equal(class.StartsAt.Add(15*time.Minute)) || len(roster.Items) != 1 || roster.Items[0].Attendance != nil {
		t.Fatalf("expected default opening and unmarked booking: %+v", roster)
	}
	expectBookingError(t, request(t, routes, "PUT", path, nil, authState{}), 401, "unauthenticated")
	expectBookingError(t, request(t, routes, "PUT", path, nil, parent), 403, "forbidden")
	expectBookingError(t, request(t, routes, "PUT", path, nil, student), 403, "forbidden")
	expectBookingError(t, request(t, routes, "PUT", path, nil, otherCoach), 404, "not_found")
	expectBookingError(t, request(t, routes, "PUT", rosterURL+"/missing/attendance", nil, f.admin), 404, "not_found")
	expectBookingError(t, request(t, routes, "PUT", path, nil, coach), 409, "attendance_not_open")

	w = request(t, routes, "PATCH", "/api/v1/settings/lessons", LessonSettings{
		Version: 1, ParentChangeCutoffHours: 24, AttendanceOpenMinutes: 20, CorrectionWindowDays: 3,
	}, f.admin)
	expectStatus(t, w, 200)
	now = class.StartsAt.Add(15 * time.Minute)
	expectBookingError(t, request(t, routes, "PUT", path, nil, coach), 409, "attendance_not_open")
	now = class.StartsAt.Add(20 * time.Minute)

	checkState := func(expectedMarks, expectedReserved, expectedConsumed int) {
		t.Helper()
		var marks, audits, reservedCredits, consumedCredits int
		err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM booking_attendance),
			(SELECT count(*) FROM audit_events WHERE action='attendance.present' AND entity_id=$1 AND actor_id='coach'),
			sum(reserved),sum(consumed) FROM credit_movements WHERE package_id='attendance-pack'`, reserved.ID).
			Scan(&marks, &audits, &reservedCredits, &consumedCredits)
		if err != nil || marks != expectedMarks || audits != expectedMarks || reservedCredits != expectedReserved || consumedCredits != expectedConsumed {
			t.Fatalf("marks=%d audits=%d reserved=%d consumed=%d, %v", marks, audits, reservedCredits, consumedCredits, err)
		}
	}
	exec("ALTER TABLE audit_events ADD CONSTRAINT fail_attendance CHECK (action<>'attendance.present')")
	expectStatus(t, request(t, routes, "PUT", path, nil, coach), 500)
	checkState(0, 1, 0)
	exec("ALTER TABLE audit_events DROP CONSTRAINT fail_attendance")

	results := make(chan *httptest.ResponseRecorder, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			results <- request(t, routes, "PUT", path, nil, coach)
		}()
	}
	close(start)
	for range 2 {
		w = <-results
		expectStatus(t, w, 200)
		mark := decodeBody[BookingAttendance](t, w)
		if mark.Status != "present" || !mark.MarkedAt.Equal(now) {
			t.Fatalf("unexpected mark: %+v", mark)
		}
	}
	checkState(1, 0, 1)

	w = request(t, routes, "GET", rosterURL, nil, f.admin)
	expectStatus(t, w, 200)
	roster = decodeBody[ClassRoster](t, w)
	if !roster.AttendanceOpensAt.Equal(now) || len(roster.Items) != 1 || roster.Items[0].Status != "reserved" ||
		roster.Items[0].Attendance == nil || !roster.Items[0].Attendance.MarkedAt.Equal(now) {
		t.Fatalf("marked booking should still occupy its roster place: %+v", roster)
	}
	for _, actor := range []authState{parent, student} {
		w = request(t, routes, "GET", bookingURL+"?from=2030-01-07&to=2030-01-07", nil, actor)
		expectStatus(t, w, 200)
		lessons := decodeBody[struct{ Items []StudentBooking }](t, w).Items
		if len(lessons) != 2 || lessons[0].Attendance == nil || lessons[0].Attendance.Status != "present" || lessons[1].Attendance != nil {
			t.Fatalf("attendance missing from the student's lesson: %+v", lessons)
		}
	}

	exec("UPDATE class_sessions SET coach_id='coach2' WHERE id=$1", class.ID)
	expectBookingError(t, request(t, routes, "PUT", path, nil, coach), 404, "not_found")
	now = now.Add(time.Minute)
	w = request(t, routes, "PUT", path, nil, f.admin)
	expectStatus(t, w, 200)
	if mark := decodeBody[BookingAttendance](t, w); !mark.MarkedAt.Equal(roster.AttendanceOpensAt) {
		t.Fatalf("replay changed timestamp: %+v", mark)
	}
	checkState(1, 0, 1)

	now = class.StartsAt.Add(48 * time.Hour)
	latePath := "/api/v1/classes/" + later.ID + "/roster/" + lateBooking.ID + "/attendance"
	exec(`INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
		VALUES('release-fixture','late-attendance-pack','release-fixture',1,-1,0,0,'admin','Test ledger mismatch',now())`)
	expectBookingError(t, request(t, routes, "PUT", latePath, nil, f.admin), 409, "credit_unavailable")
	exec(`INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
		VALUES('restore-fixture','late-attendance-pack','restore-fixture',-1,1,0,0,'admin','Restore test ledger',now())`)
	w = request(t, routes, "PUT", latePath, nil, f.admin)
	expectStatus(t, w, 200)
	if mark := decodeBody[BookingAttendance](t, w); !mark.MarkedAt.Equal(now) {
		t.Fatalf("administrator could not record attendance on a still-reserved credit past expiry: %+v", mark)
	}
}
