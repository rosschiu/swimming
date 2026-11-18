package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestIntegrationLessonChangeWithdrawal(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	base := testHandler(db)
	f := prepareScheduling(t, base)
	original := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 6)
	target := createBookingClass(t, f, "2030-01-08", "10:00", "11:00", 6)
	seedBookingCredit(t, db, "withdraw-pack", "amy", "parent", 2, nil)
	parent := login(t, base, "parent@swim.test")
	other := login(t, base, "other-parent@swim.test")
	coach := login(t, base, "coach@swim.test")
	student := login(t, base, "student@swim.test")
	if _, err := db.Exec(ctx, "UPDATE sessions SET expires_at='2031-01-01'"); err != nil {
		t.Fatal(err)
	}
	now := original.StartsAt.Add(-48 * time.Hour)
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return now }}
	h := http.NewServeMux()
	a.bookingRoutes(h)
	a.lessonChangeRoutes(h)

	w := request(t, h, "POST", "/api/v1/students/amy/bookings", bookingBody(original, "withdraw-pack", "withdraw-booking"), parent)
	expectStatus(t, w, 201)
	booking := decodeBody[StudentBooking](t, w)
	requestChange := func(key string) LessonChange {
		t.Helper()
		w := request(t, h, "POST", "/api/v1/students/amy/bookings/"+booking.ID+"/change-requests",
			map[string]any{"targetClassId": target.ID, "targetClassVersion": target.Version, "idempotencyKey": key}, parent)
		expectStatus(t, w, 201)
		return decodeBody[LessonChange](t, w)
	}
	withdraw := func(id string, actor authState) *httptest.ResponseRecorder {
		return request(t, h, "POST", "/api/v1/students/amy/lesson-changes/"+id+"/withdraw", nil, actor)
	}
	bookedClass := func() string {
		t.Helper()
		var classID string
		if err := db.QueryRow(ctx, "SELECT class_id FROM bookings WHERE id=$1", booking.ID).Scan(&classID); err != nil {
			t.Fatal(err)
		}
		return classID
	}
	reserved := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(ctx, "SELECT sum(reserved) FROM credit_movements WHERE package_id='withdraw-pack'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	first := requestChange("withdraw-first")
	expectStatus(t, withdraw(first.ID, other), 404)
	expectStatus(t, withdraw(first.ID, coach), 403)
	expectStatus(t, withdraw(first.ID, student), 403)
	w = withdraw(first.ID, parent)
	expectStatus(t, w, 200)
	withdrawn := decodeBody[LessonChange](t, w)
	if withdrawn.Status != "withdrawn" || withdrawn.DecidedBy == nil || *withdrawn.DecidedBy != "parent" || withdrawn.DecidedAt == nil {
		t.Fatalf("unexpected withdrawn request: %+v", withdrawn)
	}
	expectStatus(t, withdraw(first.ID, parent), 200)
	expectBookingError(t, request(t, h, "PUT", "/api/v1/lesson-changes/"+first.ID+"/decision", map[string]string{"decision": "approved"}, coach), 409, "change_conflict")
	if bookedClass() != original.ID || reserved() != 1 {
		t.Fatalf("withdrawal moved the booking or changed its credit: class %s reserved %d", bookedClass(), reserved())
	}
	var audits int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE action='lesson_change.withdrawn' AND entity_id=$1", first.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("expected one withdrawal audit, got %d (%v)", audits, err)
	}

	// The family can ask again once the earlier request is withdrawn; a decided request cannot be withdrawn.
	second := requestChange("withdraw-second")
	expectStatus(t, request(t, h, "PUT", "/api/v1/lesson-changes/"+second.ID+"/decision", map[string]string{"decision": "rejected"}, coach), 200)
	expectBookingError(t, withdraw(second.ID, parent), 409, "change_conflict")

	// A withdrawal racing the coach's approval leaves exactly one outcome.
	race := requestChange("withdraw-race")
	var wg sync.WaitGroup
	var approval, withdrawal int
	wg.Add(2)
	go func() {
		defer wg.Done()
		approval = request(t, h, "PUT", "/api/v1/lesson-changes/"+race.ID+"/decision", map[string]string{"decision": "approved"}, coach).Code
	}()
	go func() { defer wg.Done(); withdrawal = withdraw(race.ID, parent).Code }()
	wg.Wait()
	var status string
	if err := db.QueryRow(ctx, "SELECT status FROM lesson_change_requests WHERE id=$1", race.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	switch {
	case status == "approved" && approval == 200 && withdrawal == 409 && bookedClass() == target.ID:
	case status == "withdrawn" && withdrawal == 200 && approval == 409 && bookedClass() == original.ID:
	default:
		t.Fatalf("race left status %s, approval %d, withdrawal %d, class %s", status, approval, withdrawal, bookedClass())
	}
	if reserved() != 1 {
		t.Fatalf("race changed reserved credits to %d", reserved())
	}
}
