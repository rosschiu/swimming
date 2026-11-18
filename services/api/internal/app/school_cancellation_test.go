package app

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestIntegrationSchoolCancellation(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	lesson := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 4)
	elsewhere := createBookingClass(t, f, "2030-01-08", "10:00", "11:00", 4)
	replacement := createBookingClass(t, f, "2030-01-09", "10:00", "11:00", 4)
	expiry := "2030-01-31"
	seedBookingCredit(t, db, "school-amy", "amy", "parent", 3, &expiry)
	seedBookingCredit(t, db, "school-ben", "ben", "other-parent", 2, &expiry)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE students SET level='Beginner' WHERE id='ben'")
	parent := login(t, h, "parent@swim.test")
	other := login(t, h, "other-parent@swim.test")
	exec("UPDATE sessions SET expires_at='2031-01-01'")

	now := lesson.StartsAt.Add(-72 * time.Hour)
	a := &API{db: db, cfg: Config{Origin: testOrigin, TeachingEmailAllowlist: []string{"parent@swim.test"}}, now: func() time.Time { return now }}
	routes := http.NewServeMux()
	a.bookingRoutes(routes)
	a.schedulingRoutes(routes)
	a.lessonChangeRoutes(routes)
	a.schoolCancellationRoutes(routes)
	book := func(class ClassSession, pack, key string, actor authState, student string) StudentBooking {
		t.Helper()
		w := request(t, routes, "POST", "/api/v1/students/"+student+"/bookings", bookingBody(class, pack, key), actor)
		expectStatus(t, w, 201)
		return decodeBody[StudentBooking](t, w)
	}
	amy := book(lesson, "school-amy", "school-amy", parent, "amy")
	ben := book(lesson, "school-ben", "school-ben", other, "ben")
	moving := book(elsewhere, "school-amy", "school-moving", parent, "amy")
	exec(`INSERT INTO lesson_change_requests(id,booking_id,original_class_id,target_class_id,original_class,target_class,requested_by,requested_at,settings_version,idempotency_key) VALUES
		('outbound',$1,$2,$3,'{}','{}','parent',now(),1,'outbound'),('inbound',$4,$5,$2,'{}','{}','parent',now(),1,'inbound')`,
		amy.ID, lesson.ID, replacement.ID, moving.ID, elsewhere.ID)
	// Ben's package expires before the cancellation, so his returned credit needs a new expiry.
	exec("UPDATE credit_packages SET expires_on='2030-01-03' WHERE id='school-ben'")
	now = lesson.StartsAt.Add(-48 * time.Hour)

	path := "/api/v1/classes/" + lesson.ID + "/cancellation"
	body := func(version int, key string, expiries map[string]string) schoolCancellationInput {
		return schoolCancellationInput{Reason: "Pool maintenance", Version: version, IdempotencyKey: key, Expiries: expiries}
	}
	expectBookingError(t, request(t, routes, "POST", path, body(lesson.Version, "parent", nil), parent), 403, "forbidden")
	expectBookingError(t, request(t, routes, "POST", path, body(lesson.Version+1, "stale", nil), f.admin), 409, "version_conflict")
	w := request(t, routes, "POST", path, body(lesson.Version, "missing", nil), f.admin)
	expectBookingError(t, w, 409, "expiry_required")
	details := decodeBody[struct {
		Error struct {
			Details struct {
				BookingIDs []string `json:"bookingIds"`
			} `json:"details"`
		} `json:"error"`
	}](t, w)
	if ids := details.Error.Details.BookingIDs; len(ids) != 1 || ids[0] != ben.ID {
		t.Fatalf("expected only Ben's booking to need an expiry: %v", ids)
	}
	expectBookingError(t, request(t, routes, "POST", path, body(lesson.Version, "past", map[string]string{ben.ID: "2030-01-01"}), f.admin), 409, "expiry_required")

	cancel := body(lesson.Version, "school-cancel", map[string]string{ben.ID: "2030-03-31"})
	w = request(t, routes, "POST", path, cancel, f.admin)
	expectStatus(t, w, 200)
	result := decodeBody[SchoolCancellationResult](t, w)
	if result.CancelledBookings != 2 || result.RestoredCredits != 1 || result.ClosedRequests != 2 || result.Lesson.CancelledAt == nil || result.Reason != "Pool maintenance" {
		t.Fatalf("unexpected result: %+v", result)
	}
	expectStatus(t, request(t, routes, "POST", path, cancel, f.admin), 200)
	expectBookingError(t, request(t, routes, "POST", path, body(result.Lesson.Version, "again", nil), f.admin), 409, "class_cancelled")

	sum := func(pack string) [4]int {
		t.Helper()
		var x [4]int
		if err := db.QueryRow(ctx, "SELECT sum(available),sum(reserved),sum(consumed),sum(expired) FROM credit_movements WHERE package_id=$1", pack).Scan(&x[0], &x[1], &x[2], &x[3]); err != nil {
			t.Fatal(err)
		}
		return x
	}
	if got := sum("school-amy"); got != [4]int{2, 1, 0, 0} {
		t.Fatalf("Amy's credit should return; her other booking stays reserved: %v", got)
	}
	if got := sum("school-ben"); got != [4]int{1, 0, 0, 0} {
		t.Fatalf("Ben's expired package keeps only its unbooked unit: %v", got)
	}
	var restoredExpiry string
	var restoredAvailable int
	if err := db.QueryRow(ctx, `SELECT cp.expires_on::text,(SELECT sum(available) FROM credit_movements WHERE package_id=cp.id)
		FROM credit_packages cp WHERE cp.source_package_id='school-ben'`).Scan(&restoredExpiry, &restoredAvailable); err != nil || restoredExpiry != "2030-03-31" || restoredAvailable != 1 {
		t.Fatalf("restored package: %s %d %v", restoredExpiry, restoredAvailable, err)
	}

	var outbound, inbound, movingStatus string
	if err := db.QueryRow(ctx, `SELECT (SELECT status||':'||decision_reason FROM lesson_change_requests WHERE id='outbound'),
		(SELECT status||':'||decision_reason FROM lesson_change_requests WHERE id='inbound'),
		(SELECT status||':'||class_id FROM bookings WHERE id=$1)`, moving.ID).Scan(&outbound, &inbound, &movingStatus); err != nil {
		t.Fatal(err)
	}
	if outbound != "closed:School cancelled the lesson" || inbound != "closed:School cancelled the requested lesson" || movingStatus != "reserved:"+elsewhere.ID {
		t.Fatalf("pending changes: %s / %s / %s", outbound, inbound, movingStatus)
	}

	// The cancelled lesson accepts no bookings, edits or moves; its coach is free for the slot again.
	expectBookingError(t, request(t, routes, "POST", "/api/v1/students/amy/bookings", bookingBody(result.Lesson, "school-amy", "after-cancel"), parent), 409, "class_unavailable")
	input := f.input(f.venue.ID, "2030-01-07", "10:00", "11:00")
	edit := struct {
		ClassInput
		Version int `json:"version"`
	}{input, result.Lesson.Version}
	expectBookingError(t, request(t, routes, "PATCH", "/api/v1/classes/"+lesson.ID, edit, f.admin), 409, "class_cancelled")
	w = request(t, routes, "POST", "/api/v1/classes", input, f.admin)
	expectStatus(t, w, 201)

	w = request(t, routes, "GET", "/api/v1/students/amy/bookings/"+amy.ID, nil, parent)
	expectStatus(t, w, 200)
	if got := decodeBody[StudentBookingDetail](t, w); got.Status != "cancelled" || got.Cancellation == nil || !got.Cancellation.BySchool || got.Cancellation.Timing != "" {
		t.Fatalf("family readback: %+v", got.Cancellation)
	}
	var familyNotices, coachNotices, pending int
	if err := db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE u.role IN ('parent','student')),count(*) FILTER (WHERE u.role='coach'),
		(SELECT count(*) FROM notification_deliveries d JOIN notifications x ON x.id=d.notification_id WHERE x.kind='class.cancelled' AND d.status='pending')
		FROM notifications n JOIN users u ON u.id=n.recipient_id WHERE n.kind='class.cancelled'`).Scan(&familyNotices, &coachNotices, &pending); err != nil {
		t.Fatal(err)
	}
	if familyNotices != 3 || coachNotices != 2 || pending != 1 {
		t.Fatalf("expected Amy's guardian and student, Ben's guardian, two coach notices and one allowlisted email: %d %d %d", familyNotices, coachNotices, pending)
	}

	// A lesson that has started is left to attendance corrections.
	started := createBookingClass(t, f, "2030-01-10", "10:00", "11:00", 4)
	now = started.StartsAt
	expectBookingError(t, request(t, routes, "POST", "/api/v1/classes/"+started.ID+"/cancellation", body(started.Version, "started", nil), f.admin), 409, "class_started")
	if _, err := db.Exec(ctx, "UPDATE class_cancellations SET reason='edited'"); err == nil {
		t.Fatal("class cancellation history must be immutable")
	}
}
