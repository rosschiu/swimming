package app

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestIntegrationCoachSubstitution(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	lesson := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 4)
	target := createBookingClass(t, f, "2030-01-08", "10:00", "11:00", 4)
	busyInput := f.input(f.venue.ID, "2030-01-07", "10:30", "11:30")
	seedBookingCredit(t, db, "sub-pack", "amy", "parent", 2, nil)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Second Coach','coach',password_hash FROM users WHERE id='coach';
		INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach3','coach3@swim.test','Busy Coach','coach',password_hash FROM users WHERE id='coach'`)
	parent := login(t, h, "parent@swim.test")
	coach := login(t, h, "coach@swim.test")
	substitute := login(t, h, "coach2@swim.test")
	exec("UPDATE sessions SET expires_at='2031-01-01'")
	busyInput.CoachID = "coach3"
	w := request(t, h, "POST", "/api/v1/classes", busyInput, f.admin)
	expectStatus(t, w, 201)

	now := lesson.StartsAt.Add(-48 * time.Hour)
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return now }}
	routes := http.NewServeMux()
	a.bookingRoutes(routes)
	a.schedulingRoutes(routes)
	a.lessonChangeRoutes(routes)
	a.notificationRoutes(routes)
	a.substitutionRoutes(routes)
	w = request(t, routes, "POST", "/api/v1/students/amy/bookings", bookingBody(lesson, "sub-pack", "sub-book"), parent)
	expectStatus(t, w, 201)
	booking := decodeBody[StudentBooking](t, w)
	exec(`INSERT INTO lesson_change_requests(id,booking_id,original_class_id,target_class_id,original_class,target_class,requested_by,requested_at,settings_version,idempotency_key)
		VALUES('sub-change',$1,$2,$3,$4,$5,'parent',now(),1,'sub-change')`, booking.ID, lesson.ID, target.ID, lesson, target)

	// A booked class no longer changes coach silently through an edit.
	edit := struct {
		ClassInput
		Version int `json:"version"`
	}{lesson.ClassInput, lesson.Version}
	edit.CoachID = "coach2"
	expectBookingError(t, request(t, routes, "PATCH", "/api/v1/classes/"+lesson.ID, edit, f.admin), 409, "substitution_required")

	path := "/api/v1/classes/" + lesson.ID + "/substitution"
	body := func(coachID string, version int, key string) map[string]any {
		return map[string]any{"coachId": coachID, "reason": "Coach unwell", "version": version, "idempotencyKey": key}
	}
	expectBookingError(t, request(t, routes, "POST", path, body("coach2", lesson.Version, "parent"), parent), 403, "forbidden")
	expectBookingError(t, request(t, routes, "POST", path, body("coach", lesson.Version, "same"), f.admin), 422, "validation_error")
	expectBookingError(t, request(t, routes, "POST", path, body("coach3", lesson.Version, "busy"), f.admin), 409, "scheduling_conflict")
	expectBookingError(t, request(t, routes, "POST", path, body("coach2", lesson.Version+1, "stale"), f.admin), 409, "version_conflict")
	w = request(t, routes, "POST", path, body("coach2", lesson.Version, "sub-1"), f.admin)
	expectStatus(t, w, 200)
	sub := decodeBody[ClassSubstitution](t, w)
	if sub.FromCoachID != "coach" || sub.ToCoachID != "coach2" || sub.Lesson.CoachID != "coach2" || sub.Lesson.Version != lesson.Version+1 {
		t.Fatalf("unexpected substitution: %+v", sub)
	}
	expectStatus(t, request(t, routes, "POST", path, body("coach2", lesson.Version, "sub-1"), f.admin), 200)
	expectBookingError(t, request(t, routes, "POST", path, body("coach", lesson.Version, "sub-1"), f.admin), 409, "conflict")

	// Bookings, credits and times stay; roster and pending-decision access move to the substitute.
	var status, classID string
	var reserved int
	if err := db.QueryRow(ctx, "SELECT b.status,b.class_id,(SELECT sum(reserved) FROM credit_movements WHERE package_id='sub-pack') FROM bookings b WHERE b.id=$1", booking.ID).Scan(&status, &classID, &reserved); err != nil {
		t.Fatal(err)
	}
	if status != "reserved" || classID != lesson.ID || reserved != 1 {
		t.Fatalf("substitution changed the booking: %s %s %d", status, classID, reserved)
	}
	rosterPath := "/api/v1/classes/" + lesson.ID + "/roster"
	expectStatus(t, request(t, routes, "GET", rosterPath, nil, coach), 404)
	expectStatus(t, request(t, routes, "GET", rosterPath, nil, substitute), 200)
	expectStatus(t, request(t, routes, "PUT", "/api/v1/lesson-changes/sub-change/decision", map[string]string{"decision": "rejected"}, coach), 404)
	expectStatus(t, request(t, routes, "PUT", "/api/v1/lesson-changes/sub-change/decision", map[string]string{"decision": "rejected"}, substitute), 200)

	// Families and both coaches are told; the outgoing coach can still read their notice.
	var family, coaches int
	if err := db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE u.role IN ('parent','student')),count(*) FILTER (WHERE u.role='coach')
		FROM notifications n JOIN users u ON u.id=n.recipient_id WHERE n.kind='class.coach_changed'`).Scan(&family, &coaches); err != nil {
		t.Fatal(err)
	}
	if family != 2 || coaches != 2 {
		t.Fatalf("expected guardian, student and both coaches notified: %d %d", family, coaches)
	}
	for _, actor := range []authState{coach, substitute} {
		w = request(t, routes, "GET", "/api/v1/notifications?limit=5", nil, actor)
		expectStatus(t, w, 200)
		if list := decodeBody[struct{ Items []Notification }](t, w); len(list.Items) == 0 || list.Items[0].Kind != "class.coach_changed" {
			t.Fatalf("coach inbox missing substitution notice: %+v", list.Items)
		}
	}

	now = lesson.StartsAt
	expectBookingError(t, request(t, routes, "POST", path, body("coach", sub.Lesson.Version, "started"), f.admin), 409, "class_started")
	if _, err := db.Exec(ctx, "UPDATE class_substitutions SET reason='edited'"); err == nil {
		t.Fatal("substitution history must be immutable")
	}
}
