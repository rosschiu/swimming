package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegrationFamilyCancellation(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	full := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 1)
	soon := createBookingClass(t, f, "2030-01-08", "10:00", "11:00", 4)
	strictLesson := createBookingClass(t, f, "2030-01-09", "10:00", "11:00", 4)
	expiringLesson := createBookingClass(t, f, "2030-01-10", "10:00", "11:00", 4)
	target := createBookingClass(t, f, "2030-01-11", "10:00", "11:00", 4)
	expiry := "2030-01-31"
	seedBookingCredit(t, db, "cancel-pack", "amy", "parent", 3, &expiry)
	seedBookingCredit(t, db, "strict-cancel", "amy", "parent", 1, nil)
	seedBookingCredit(t, db, "expiring-cancel", "amy", "parent", 1, &expiry)
	seedBookingCredit(t, db, "ben-pack", "ben", "other-parent", 1, nil)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE credit_packages SET policy=jsonb_set(policy,'{lateCancellationRetainsCredit}','false') WHERE id='strict-cancel'`)
	exec("UPDATE users SET service_email_enabled=false WHERE id='student'")
	exec("UPDATE students SET level='Beginner' WHERE id='ben'")
	exec("UPDATE users SET teaching_email_enabled=true WHERE id='parent'")
	parent := login(t, h, "parent@swim.test")
	other := login(t, h, "other-parent@swim.test")
	student := login(t, h, "student@swim.test")
	coach := login(t, h, "coach@swim.test")
	exec("UPDATE sessions SET expires_at='2031-01-01'")

	now := full.StartsAt.Add(-72 * time.Hour)
	a := &API{db: db, cfg: Config{Origin: testOrigin, TeachingEmailAllowlist: []string{"parent@swim.test"}}, now: func() time.Time { return now }}
	routes := http.NewServeMux()
	a.bookingRoutes(routes)
	a.cancellationRoutes(routes)
	a.lessonChangeRoutes(routes)
	a.notificationRoutes(routes)
	book := func(class ClassSession, pack, key string, actor authState, student string) StudentBooking {
		t.Helper()
		w := request(t, routes, "POST", "/api/v1/students/"+student+"/bookings", bookingBody(class, pack, key), actor)
		expectStatus(t, w, 201)
		return decodeBody[StudentBooking](t, w)
	}
	path := func(b StudentBooking) string {
		return "/api/v1/students/" + b.StudentID + "/bookings/" + b.ID + "/cancellation"
	}
	preview := func(b StudentBooking) CancellationPreview {
		t.Helper()
		w := request(t, routes, "GET", path(b), nil, parent)
		expectStatus(t, w, 200)
		return decodeBody[CancellationPreview](t, w)
	}
	cancel := func(b StudentBooking, timing, outcome, key string, actor authState) *httptest.ResponseRecorder {
		return request(t, routes, "POST", path(b), map[string]string{"classId": b.Class.ID, "timing": timing, "creditOutcome": outcome, "idempotencyKey": key}, actor)
	}
	balance := func(pack string) [4]int {
		t.Helper()
		var x [4]int
		if err := db.QueryRow(ctx, "SELECT sum(available),sum(reserved),sum(consumed),sum(expired) FROM credit_movements WHERE package_id=$1", pack).Scan(&x[0], &x[1], &x[2], &x[3]); err != nil {
			t.Fatal(err)
		}
		return x
	}

	booked := book(full, "cancel-pack", "cancel-full", parent, "amy")
	moving := book(soon, "cancel-pack", "cancel-soon", parent, "amy")
	exec(`INSERT INTO lesson_change_requests(id,booking_id,original_class_id,target_class_id,original_class,target_class,requested_by,requested_at,settings_version,idempotency_key)
		VALUES('cancel-change',$1,$2,$3,'{}','{}','parent',now(),1,'cancel-change')`, booked.ID, full.ID, target.ID)

	// Scope: only a linked guardian previews or cancels.
	expectStatus(t, request(t, routes, "GET", path(booked), nil, other), 404)
	expectBookingError(t, request(t, routes, "GET", path(booked), nil, student), 403, "forbidden")
	expectBookingError(t, cancel(booked, "early", "retained", "coach", coach), 403, "forbidden")

	got := preview(booked)
	if got.Timing != "early" || got.CreditOutcome != "retained" || !got.LateFrom.Equal(full.StartsAt.Add(-24*time.Hour)) || !got.ClosesAt.Equal(full.StartsAt) {
		t.Fatalf("unexpected early preview: %+v", got)
	}
	expectBookingError(t, cancel(booked, "late", "retained", "stale-review", parent), 409, "cancellation_changed")
	w := cancel(booked, "early", "retained", "cancel-1", parent)
	expectStatus(t, w, 200)
	cancelled := decodeBody[StudentBooking](t, w)
	if cancelled.Status != "cancelled" || cancelled.Cancellation == nil || cancelled.Cancellation.Timing != "early" || cancelled.Cancellation.Lesson.ID != full.ID {
		t.Fatalf("unexpected cancelled booking: %+v", cancelled)
	}
	expectStatus(t, cancel(booked, "early", "retained", "cancel-1", parent), 200)
	expectBookingError(t, cancel(booked, "early", "consumed", "cancel-1", parent), 409, "conflict")
	expectStatus(t, cancel(booked, "early", "retained", "cancel-again", parent), 404)
	if got := balance("cancel-pack"); got != [4]int{2, 1, 0, 0} {
		t.Fatalf("early cancellation should return one unit: %v", got)
	}
	var changeStatus string
	if err := db.QueryRow(ctx, "SELECT status FROM lesson_change_requests WHERE id='cancel-change'").Scan(&changeStatus); err != nil || changeStatus != "closed" {
		t.Fatalf("pending change not closed: %s %v", changeStatus, err)
	}

	// The freed place in a full class is bookable by another family; the roster shows only the active booking.
	ben := book(full, "ben-pack", "ben-takes-place", other, "ben")
	w = request(t, routes, "GET", "/api/v1/classes/"+full.ID+"/roster", nil, coach)
	expectStatus(t, w, 200)
	if roster := decodeBody[ClassRoster](t, w); len(roster.Items) != 1 || roster.Items[0].BookingID != ben.ID {
		t.Fatalf("roster still lists the cancelled booking: %+v", roster.Items)
	}

	// Notices reach the guardian, the student and the class coach; email follows preference and allowlist.
	var notices, pending, suppressed int
	if err := db.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE d.status='pending'), count(*) FILTER (WHERE d.status='suppressed')
		FROM notifications n LEFT JOIN notification_deliveries d ON d.notification_id=n.id WHERE n.kind='booking.cancelled' AND n.source_id=$1`, booked.ID).Scan(&notices, &pending, &suppressed); err != nil {
		t.Fatal(err)
	}
	if notices != 3 || pending != 1 || suppressed != 1 {
		t.Fatalf("expected three notices with one pending and one suppressed email, got %d/%d/%d", notices, pending, suppressed)
	}
	w = request(t, routes, "GET", "/api/v1/notifications?limit=10", nil, coach)
	expectStatus(t, w, 200)
	if list := decodeBody[struct{ Items []Notification }](t, w); len(list.Items) == 0 || list.Items[0].Kind != "booking.cancelled" {
		t.Fatalf("coach inbox missing the cancellation: %+v", list.Items)
	}

	// A class edit after cancellation leaves the reviewed lesson in history.
	moved := full
	moved.StartsAt, moved.EndsAt = full.StartsAt.Add(2*time.Hour), full.EndsAt.Add(2*time.Hour)
	exec("UPDATE class_sessions SET starts_at=$2,ends_at=$3 WHERE id=$1", full.ID, moved.StartsAt, moved.EndsAt)
	w = request(t, routes, "GET", "/api/v1/students/amy/bookings/"+booked.ID, nil, parent)
	expectStatus(t, w, 200)
	if detail := decodeBody[StudentBookingDetail](t, w); detail.Cancellation == nil || !detail.Cancellation.Lesson.StartsAt.Equal(full.StartsAt) || !detail.Class.StartsAt.Equal(moved.StartsAt) {
		t.Fatalf("cancellation history should keep the reviewed lesson: %+v", detail)
	}

	// An approval that moves the booking after review makes the cancellation stale.
	exec(`INSERT INTO lesson_change_requests(id,booking_id,original_class_id,target_class_id,original_class,target_class,requested_by,requested_at,settings_version,idempotency_key)
		VALUES('move-before-cancel',$1,$2,$3,$4,$5,'parent',now(),1,'move-before-cancel')`, moving.ID, soon.ID, target.ID, soon, target)
	expectStatus(t, request(t, routes, "PUT", "/api/v1/lesson-changes/move-before-cancel/decision", map[string]string{"decision": "approved"}, coach), 200)
	expectBookingError(t, cancel(moving, "early", "retained", "stale-class", parent), 409, "cancellation_changed")
	moving.Class = target
	expectStatus(t, cancel(moving, "early", "retained", "fresh-class", parent), 200)
	moving = book(soon, "cancel-pack", "cancel-soon-again", parent, "amy")

	// Late cancellation follows the package's late policy, and self-service closes at lesson start.
	strict := book(strictLesson, "strict-cancel", "cancel-strict", parent, "amy")
	now = strictLesson.StartsAt.Add(-time.Hour)
	if got := preview(strict); got.Timing != "late" || got.CreditOutcome != "consumed" {
		t.Fatalf("unexpected late preview: %+v", got)
	}
	expectStatus(t, cancel(strict, "late", "consumed", "cancel-strict", parent), 200)
	if got := balance("strict-cancel"); got != [4]int{0, 0, 1, 0} {
		t.Fatalf("non-retaining late cancellation should consume: %v", got)
	}
	now = soon.StartsAt
	expectBookingError(t, cancel(moving, "late", "retained", "too-late", parent), 409, "cancellation_closed")

	// A retained credit released after its package expired goes to expired history.
	now = full.StartsAt.Add(-72 * time.Hour)
	expiring := book(expiringLesson, "expiring-cancel", "cancel-expiring", parent, "amy")
	exec("UPDATE credit_packages SET expires_on='2030-01-08' WHERE id='expiring-cancel'")
	now = expiringLesson.StartsAt.Add(-30 * time.Hour)
	if got := preview(expiring); got.Timing != "early" || got.CreditOutcome != "expired" {
		t.Fatalf("expected expired outcome: %+v", got)
	}
	expectStatus(t, cancel(expiring, "early", "expired", "cancel-expired", parent), 200)
	if got := balance("expiring-cancel"); got != [4]int{0, 0, 0, 1} {
		t.Fatalf("released credit after expiry must not become available: %v", got)
	}
	if _, err := db.Exec(ctx, "UPDATE booking_cancellations SET timing='late'"); err == nil {
		t.Fatal("cancellation history must be immutable")
	}
}

func TestIntegrationServiceEmailPreferenceIsSeparate(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	parent := login(t, h, "parent@swim.test")
	if _, err := db.Exec(ctx, `
INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version) VALUES
	('teach','parent','amy','progress.created','teach',1),('service','parent','amy','booking.cancelled','service',1);
INSERT INTO notification_deliveries(id,notification_id,status,next_attempt_at) SELECT id,id,'pending','2099-01-01' FROM notifications;`); err != nil {
		t.Fatal(err)
	}
	status := func(id string) string {
		t.Helper()
		var x string
		if err := db.QueryRow(ctx, "SELECT status FROM notification_deliveries WHERE id=$1", id).Scan(&x); err != nil {
			t.Fatal(err)
		}
		return x
	}
	w := request(t, h, "PUT", "/api/v1/notification-preferences", map[string]bool{"emailEnabled": false}, parent)
	expectStatus(t, w, 200)
	if prefs := decodeBody[NotificationPreferences](t, w); prefs.EmailEnabled || !prefs.ServiceEmailEnabled {
		t.Fatalf("unexpected preferences: %+v", prefs)
	}
	if status("teach") != "suppressed" || status("service") != "pending" {
		t.Fatalf("teaching opt-out reached service email: %s %s", status("teach"), status("service"))
	}
	expectStatus(t, request(t, h, "PUT", "/api/v1/notification-preferences", map[string]bool{"serviceEmailEnabled": false}, parent), 200)
	if status("service") != "suppressed" {
		t.Fatal("service opt-out did not stop the queued cancellation email")
	}
	expectStatus(t, request(t, h, "PUT", "/api/v1/notification-preferences", map[string]any{}, parent), 422)
}
