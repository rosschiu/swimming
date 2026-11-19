package app

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestIntegrationLessonReminders(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	first := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 4)
	second := createBookingClass(t, f, "2030-01-08", "10:00", "11:00", 4)
	third := createBookingClass(t, f, "2030-01-09", "10:00", "11:00", 4)
	seedBookingCredit(t, db, "reminder-pack", "amy", "parent", 3, nil)
	parent := login(t, h, "parent@swim.test")
	student := login(t, h, "student@swim.test")
	if _, err := db.Exec(ctx, "UPDATE sessions SET expires_at='2031-01-01'"); err != nil {
		t.Fatal(err)
	}
	now := first.StartsAt.Add(-72 * time.Hour)
	a := &API{db: db, cfg: Config{Origin: testOrigin, TeachingEmailAllowlist: []string{"parent@swim.test"}}, now: func() time.Time { return now }}
	routes := http.NewServeMux()
	a.bookingRoutes(routes)
	a.cancellationRoutes(routes)
	a.notificationRoutes(routes)
	book := func(class ClassSession, key string) StudentBooking {
		t.Helper()
		w := request(t, routes, "POST", "/api/v1/students/amy/bookings", bookingBody(class, "reminder-pack", key), parent)
		expectStatus(t, w, 201)
		return decodeBody[StudentBooking](t, w)
	}
	firstBooking, secondBooking, thirdBooking := book(first, "r1"), book(second, "r2"), book(third, "r3")
	schedule := func(want int) {
		t.Helper()
		created, err := a.scheduleReminders(ctx)
		if err != nil || created != want {
			t.Fatalf("scheduled %d reminders, want %d: %v", created, want, err)
		}
	}
	type delivery struct{ recipient, status, failure string }
	deliveries := func(bookingID string) map[string]delivery {
		t.Helper()
		rows, err := db.Query(ctx, `SELECT n.recipient_id,COALESCE(d.status,''),COALESCE(d.last_failure,'') FROM notifications n
			LEFT JOIN notification_deliveries d ON d.notification_id=n.id WHERE n.kind='booking.reminder' AND n.source_id=$1`, bookingID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := map[string]delivery{}
		for rows.Next() {
			var x delivery
			if err := rows.Scan(&x.recipient, &x.status, &x.failure); err != nil {
				t.Fatal(err)
			}
			got[x.recipient] = x
		}
		return got
	}

	// Nothing is due outside the 24-hour default window.
	schedule(0)

	// The first lesson enters the window: the guardian and the student each get one in-app reminder; email follows
	// the allowlist. Overlapping workers and later scans never repeat it.
	now = first.StartsAt.Add(-23 * time.Hour)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := a.scheduleReminders(ctx)
			if err != nil {
				t.Error(err)
			}
			results <- created
		}()
	}
	wg.Wait()
	if total := <-results + <-results; total != 1 {
		t.Fatalf("overlapping scans created %d reminders", total)
	}
	schedule(0)
	got := deliveries(firstBooking.ID)
	if len(got) != 2 || got["parent"].status != "pending" || got["student"].status != "suppressed" || got["student"].failure != "email_unavailable" {
		t.Fatalf("unexpected first-lesson reminders: %+v", got)
	}
	for _, user := range []authState{parent, student} {
		w := request(t, routes, "GET", "/api/v1/notifications", nil, user)
		expectStatus(t, w, 200)
		list := decodeBody[struct{ Items []Notification }](t, w)
		found := false
		for _, item := range list.Items {
			found = found || (item.Kind == "booking.reminder" && item.SourceID == firstBooking.ID && item.StudentID == "amy")
		}
		if !found {
			t.Fatalf("reminder missing from %v's notifications: %+v", user, list.Items)
		}
	}

	// A lesson cancelled before its window gets no reminder.
	w := request(t, routes, "GET", "/api/v1/students/amy/bookings/"+secondBooking.ID+"/cancellation", nil, parent)
	expectStatus(t, w, 200)
	preview := decodeBody[CancellationPreview](t, w)
	expectStatus(t, request(t, routes, "POST", "/api/v1/students/amy/bookings/"+secondBooking.ID+"/cancellation",
		map[string]string{"classId": second.ID, "timing": preview.Timing, "creditOutcome": preview.CreditOutcome, "idempotencyKey": "cancel-second"}, parent), 200)
	now = second.StartsAt.Add(-23 * time.Hour)
	schedule(0)
	if got := deliveries(secondBooking.ID); len(got) != 0 {
		t.Fatalf("cancelled lesson was reminded: %+v", got)
	}

	// A reminder whose lesson is cancelled before its email is sent is suppressed, not sent.
	now = third.StartsAt.Add(-23 * time.Hour)
	schedule(1)
	w = request(t, routes, "GET", "/api/v1/students/amy/bookings/"+thirdBooking.ID+"/cancellation", nil, parent)
	expectStatus(t, w, 200)
	preview = decodeBody[CancellationPreview](t, w)
	expectStatus(t, request(t, routes, "POST", "/api/v1/students/amy/bookings/"+thirdBooking.ID+"/cancellation",
		map[string]string{"classId": third.ID, "timing": preview.Timing, "creditOutcome": preview.CreditOutcome, "idempotencyKey": "cancel-third"}, parent), 200)
	for {
		claim, err := a.claimNotificationDelivery(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var pending int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM notification_deliveries WHERE status IN ('pending','retry')").Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if claim == nil && pending == 0 {
			break
		}
	}
	if got := deliveries(thirdBooking.ID); got["parent"].status != "suppressed" || got["parent"].failure != "reminder_outdated" {
		t.Fatalf("outdated reminder email was not suppressed: %+v", got)
	}
	if got := deliveries(firstBooking.ID); got["parent"].status != "leased" {
		t.Fatalf("current reminder email was not claimed: %+v", got)
	}

	// Moving the lesson to a new time is a new occurrence with its own reminder; email can be switched off school-wide.
	if _, err := db.Exec(ctx, "UPDATE lesson_settings SET reminder_email=false"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "UPDATE class_sessions SET starts_at=starts_at+interval '1 hour',ends_at=ends_at+interval '1 hour' WHERE id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	now = first.StartsAt.Add(-22 * time.Hour)
	schedule(1)
	var sequences, emails int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM booking_reminders WHERE booking_id=$1),
		(SELECT count(*) FROM notifications n JOIN notification_deliveries d ON d.notification_id=n.id WHERE n.source_id=$1 AND n.source_version=2)`, firstBooking.ID).Scan(&sequences, &emails); err != nil || sequences != 2 || emails != 0 {
		t.Fatalf("moved lesson: %d occurrences, %d emails, %v", sequences, emails, err)
	}

	// Reminders can be turned off, and nothing is created once a lesson has started.
	if _, err := db.Exec(ctx, "UPDATE lesson_settings SET reminder_hours=0"); err != nil {
		t.Fatal(err)
	}
	fourth := createBookingClass(t, f, "2030-01-10", "10:00", "11:00", 4)
	now = fourth.StartsAt.Add(-72 * time.Hour)
	fourthBooking := book(fourth, "r4")
	now = fourth.StartsAt.Add(-time.Hour)
	schedule(0)
	if _, err := db.Exec(ctx, "UPDATE lesson_settings SET reminder_hours=24"); err != nil {
		t.Fatal(err)
	}
	now = fourth.StartsAt.Add(time.Minute)
	schedule(0)
	if got := deliveries(fourthBooking.ID); len(got) != 0 {
		t.Fatalf("reminder created while disabled or after start: %+v", got)
	}
	if _, err := db.Exec(ctx, "DELETE FROM booking_reminders"); err == nil {
		t.Fatal("reminder history must be immutable")
	}
}
