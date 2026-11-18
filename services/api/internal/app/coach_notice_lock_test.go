package app

import (
	"context"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestIntegrationCoachNoticeReassignmentWhileMarkingRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	coach := login(t, h, "coach@swim.test")
	parent := login(t, h, "parent@swim.test")
	seedBookingCredit(t, db, "coach-inbox-credits", "amy", "parent", 2, nil)
	target := createBookingClass(t, f, "2030-01-18", "10:00", "11:00", 4)
	var classID, noticeID string
	for _, day := range []string{"2030-01-10", "2030-01-12"} {
		source := createBookingClass(t, f, day, "10:00", "11:00", 4)
		w := request(t, h, "POST", "/api/v1/students/amy/bookings", bookingBody(source, "coach-inbox-credits", day), parent)
		expectStatus(t, w, 201)
		booking := decodeBody[StudentBooking](t, w)
		w = request(t, h, "POST", "/api/v1/students/amy/bookings/"+booking.ID+"/change-requests", map[string]any{"targetClassId": target.ID, "targetClassVersion": target.Version, "idempotencyKey": day}, parent)
		expectStatus(t, w, 201)
		if classID == "" {
			classID = source.ID
			if err := db.QueryRow(ctx, "SELECT id FROM notifications WHERE kind='lesson_change.requested' AND source_id=$1", decodeBody[LessonChange](t, w).ID).Scan(&noticeID); err != nil {
				t.Fatal(err)
			}
		}
	}
	type inbox struct {
		Items       []Notification
		UnreadCount int
		NextCursor  *string
	}
	list := func(path string) inbox {
		w := request(t, h, "GET", path, nil, coach)
		expectStatus(t, w, 200)
		return decodeBody[inbox](t, w)
	}
	const path = "/api/v1/notifications?limit=1"
	first := list(path)
	if first.NextCursor == nil || first.UnreadCount != 2 || len(first.Items) != 1 || first.Items[0].ID == noticeID {
		t.Fatal("expected two notices with older notice on next page")
	}
	if _, err := db.Exec(ctx, "INSERT INTO users(id,email,name,role,password_hash) SELECT 'second-coach','second@swim.test','Second Coach','coach',password_hash FROM users WHERE id='coach'"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var pid int
	if err := tx.QueryRow(ctx, "UPDATE class_sessions SET coach_id='second-coach' WHERE id=$1 RETURNING pg_backend_pid()", classID).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(t, h, "PUT", "/api/v1/notifications/"+noticeID+"/read", nil, coach) }()
	for {
		var waiting bool
		if err := db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND datname=current_database())", pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case result := <-done:
			t.Fatalf("mark completed before assignment commit with HTTP %d", result.Code)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		expectStatus(t, result, 404)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var unread bool
	if err := db.QueryRow(ctx, "SELECT read_at IS NULL FROM notifications WHERE id=$1", noticeID).Scan(&unread); err != nil || !unread {
		t.Fatalf("revoked mark changed receipt: %v", err)
	}
	next := list(path + "&cursor=" + url.QueryEscape(*first.NextCursor))
	if next.UnreadCount != 1 || len(next.Items) != 0 || next.NextCursor != nil {
		t.Fatalf("stale cursor exposed reassigned notice: %+v", next)
	}
}
