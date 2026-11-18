package app

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegrationBookingNotificationScopeReplayAndRollback(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	cfg := Config{Origin: testOrigin, SessionTTL: time.Hour, SMTPAddr: "example.invalid:25", SMTPFrom: "school@swim.test",
		TeachingEmailAllowlist: []string{"parent@swim.test", "student@swim.test", "linked@swim.test"}}
	h := New(cfg, db)
	f := prepareScheduling(t, h)
	class := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 6)
	seedBookingCredit(t, db, "notification-pack", "amy", "parent", 1, nil)
	if _, err := db.Exec(ctx, `
INSERT INTO users(id,email,name,role,password_hash,active,teaching_email_enabled)
  SELECT guardian.id,guardian.id||'@swim.test',guardian.id,'parent',u.password_hash,guardian.active,true
  FROM users u CROSS JOIN (VALUES ('linked',true),('inactive',false)) guardian(id,active) WHERE u.id='parent';
INSERT INTO student_access(student_id,user_id,kind) VALUES ('amy','linked','guardian'),('amy','inactive','guardian');
UPDATE users SET teaching_email_enabled=true WHERE id IN ('parent','student');
ALTER TABLE notifications ADD CONSTRAINT test_reject_student_booking_notification
  CHECK (kind <> 'booking.created' OR recipient_id <> 'student') NOT VALID;
`); err != nil {
		t.Fatal(err)
	}
	parent := login(t, h, "parent@swim.test")
	linked := login(t, h, "linked@swim.test")
	student := login(t, h, "student@swim.test")
	otherParent := login(t, h, "other-parent@swim.test")
	path := "/api/v1/students/amy/bookings"
	input := bookingBody(class, "notification-pack", "notify-booking")
	expectStatus(t, request(t, h, "POST", path, input, parent), 500)
	var bookings, reservations, notificationCount, audits, available int
	if err := db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM bookings),
		(SELECT count(*) FROM credit_movements WHERE source LIKE 'booking:%'),
		(SELECT count(*) FROM notifications),
		(SELECT count(*) FROM audit_events WHERE action='booking.created'),
		(SELECT sum(available) FROM credit_movements WHERE package_id='notification-pack')`).Scan(
		&bookings, &reservations, &notificationCount, &audits, &available); err != nil {
		t.Fatal(err)
	}
	if bookings != 0 || reservations != 0 || notificationCount != 0 || audits != 0 || available != 1 {
		t.Fatalf("failed fan-out left booking, reservation, notification, audit or balance changes: %d %d %d %d %d",
			bookings, reservations, notificationCount, audits, available)
	}
	if _, err := db.Exec(ctx, "ALTER TABLE notifications DROP CONSTRAINT test_reject_student_booking_notification"); err != nil {
		t.Fatal(err)
	}

	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { results <- request(t, h, "POST", path, input, parent) }()
	}
	created, replay := <-results, <-results
	if replay.Code == 201 {
		created, replay = replay, created
	}
	expectStatus(t, created, 201)
	expectStatus(t, replay, 200)
	booking := decodeBody[StudentBooking](t, created)
	if got := decodeBody[StudentBooking](t, replay); got.ID != booking.ID {
		t.Fatalf("replay returned %s, want %s", got.ID, booking.ID)
	}

	list := func(auth authState, unread int) []Notification {
		t.Helper()
		w := request(t, h, "GET", "/api/v1/notifications", nil, auth)
		expectStatus(t, w, 200)
		result := decodeBody[struct {
			Items       []Notification `json:"items"`
			UnreadCount int            `json:"unreadCount"`
		}](t, w)
		if result.UnreadCount != unread {
			t.Fatalf("wrong unread count: %d, want %d", result.UnreadCount, unread)
		}
		return result.Items
	}
	for _, actor := range []authState{parent, linked, student} {
		items := list(actor, 1)
		if len(items) != 1 || items[0].Kind != "booking.created" || items[0].SourceID != booking.ID ||
			items[0].SourceVersion != 1 || items[0].StudentID != "amy" || items[0].StudentName != "Amy" {
			t.Fatalf("wrong confirmation: %+v", items)
		}
	}
	if items := list(otherParent, 0); len(items) != 0 {
		t.Fatalf("other family can see confirmation: %+v", items)
	}
	parentNotice := list(parent, 1)[0]
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, suppression, err := (&API{cfg: cfg}).notificationEligibility(ctx, tx, parentNotice.ID)
	if err != nil || suppression != "email_unavailable" {
		t.Fatalf("booking notification met teaching email eligibility: %q %v", suppression, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	linkedNotice := list(linked, 1)[0]
	studentNotice := list(student, 1)[0]
	expectStatus(t, request(t, h, "PUT", "/api/v1/notifications/"+studentNotice.ID+"/read", nil, student), 200)
	list(student, 0)
	expectStatus(t, request(t, h, "DELETE", "/api/v1/students/amy/relationships/linked", nil, f.admin), 204)
	if items := list(linked, 0); len(items) != 0 {
		t.Fatal("revocation did not hide the previous confirmation")
	}
	expectStatus(t, request(t, h, "PUT", "/api/v1/notifications/"+linkedNotice.ID+"/read", nil, linked), 404)
	expectStatus(t, request(t, h, "POST", path, input, parent), 200)
	list(parent, 1)

	var deliveries int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications WHERE kind='booking.created'),
		(SELECT count(*) FROM notification_deliveries)`).Scan(&notificationCount, &deliveries); err != nil {
		t.Fatal(err)
	}
	if notificationCount != 3 || deliveries != 0 {
		t.Fatalf("expected three durable confirmations and no email deliveries, got %d and %d", notificationCount, deliveries)
	}
}
