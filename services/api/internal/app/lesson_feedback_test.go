package app

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestIntegrationLessonFeedback(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	class := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 2)
	later := createBookingClass(t, f, "2030-01-07", "11:00", "12:00", 2)
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
	expiry := "2030-01-07"
	seedBookingCredit(t, db, "feedback-pack", "amy", "parent", 2, &expiry)
	w := request(t, h, "POST", "/api/v1/students/amy/bookings", bookingBody(class, "feedback-pack", "feedback"), parent)
	expectStatus(t, w, 201)
	attended := decodeBody[StudentBooking](t, w)
	w = request(t, h, "POST", "/api/v1/students/amy/bookings", bookingBody(later, "feedback-pack", "feedback-later"), parent)
	expectStatus(t, w, 201)
	unmarked := decodeBody[StudentBooking](t, w)
	exec("UPDATE sessions SET expires_at=$1", class.StartsAt.Add(7*24*time.Hour))
	now := class.StartsAt.Add(20 * time.Minute)
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return now }}
	routes := http.NewServeMux()
	a.bookingRoutes(routes)
	expectStatus(t, request(t, routes, "PUT", "/api/v1/classes/"+class.ID+"/roster/"+attended.ID+"/attendance", map[string]string{"status": "present"}, coach), 200)

	path := "/api/v1/students/amy/progress-notes"
	note := func(requestID, body string, booking any) map[string]any {
		return map[string]any{"requestId": requestID, "body": body, "bookingId": booking}
	}
	w = request(t, h, "POST", path, note("lesson-1", "Strong kick today", attended.ID), coach)
	expectStatus(t, w, 201)
	first := decodeBody[ProgressNote](t, w)
	if first.Lesson == nil || first.Lesson.BookingID != attended.ID || first.Lesson.Name == "" || !first.Lesson.StartsAt.Equal(class.StartsAt) {
		t.Fatalf("lesson context missing: %+v", first.Lesson)
	}

	// Replays return the same note; reusing the request for another lesson or none conflicts.
	w = request(t, h, "POST", path, note("lesson-1", "Strong kick today", attended.ID), coach)
	expectStatus(t, w, 200)
	if replay := decodeBody[ProgressNote](t, w); replay.ID != first.ID {
		t.Fatal(replay)
	}
	expectBookingError(t, request(t, h, "POST", path, note("lesson-1", "Strong kick today", unmarked.ID), coach), 409, "request_conflict")
	expectBookingError(t, request(t, h, "POST", path, map[string]string{"requestId": "lesson-1", "body": "Strong kick today"}, coach), 409, "request_conflict")
	expectStatus(t, request(t, h, "POST", path, note("general-1", "General", nil), coach), 201)

	// Only lessons the swimmer attended, for this swimmer, taught by this coach.
	expectBookingError(t, request(t, h, "POST", path, note("lesson-2", "Not yet", unmarked.ID), coach), 409, "lesson_not_eligible")
	expectBookingError(t, request(t, h, "POST", path, note("lesson-3", "Missing", "missing"), coach), 404, "not_found")
	expectBookingError(t, request(t, h, "POST", "/api/v1/students/ben/progress-notes", note("lesson-4", "Wrong swimmer", attended.ID), f.admin), 404, "not_found")
	expectBookingError(t, request(t, h, "POST", path, note("lesson-5", "Empty", ""), coach), 422, "validation_error")
	exec("INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Second Coach','coach',password_hash FROM users WHERE id='coach'")
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/coach2", map[string]string{"kind": "coach"}, f.admin), 200)
	expectBookingError(t, request(t, h, "POST", path, note("lesson-6", "Not my class", attended.ID), login(t, h, "coach2@swim.test")), 403, "forbidden")
	for _, actor := range []authState{parent, student} {
		expectBookingError(t, request(t, h, "POST", path, note("lesson-7", "Family", attended.ID), actor), 403, "forbidden")
	}

	// Several notes per lesson, including from an administrator.
	w = request(t, h, "POST", path, note("lesson-8", "Breathing to the left", attended.ID), f.admin)
	expectStatus(t, w, 201)
	if second := decodeBody[ProgressNote](t, w); second.Lesson == nil || second.Lesson.BookingID != attended.ID {
		t.Fatal(second)
	}

	// Families see the lesson; a correction keeps it; the link cannot be rewritten.
	for _, actor := range []authState{parent, student} {
		w = request(t, h, "GET", path, nil, actor)
		expectStatus(t, w, 200)
		notes := decodeBody[struct {
			Items []ProgressNote `json:"items"`
		}](t, w)
		linked := 0
		for _, item := range notes.Items {
			if item.Lesson != nil && item.Lesson.BookingID == attended.ID && item.Lesson.Name == first.Lesson.Name {
				linked++
			}
		}
		if len(notes.Items) != 3 || linked != 2 {
			t.Fatalf("family view: %+v", notes.Items)
		}
	}
	w = request(t, h, "PATCH", path+"/"+first.ID, map[string]any{"body": "Strong kick and streamline", "reason": "Added detail", "version": 1}, coach)
	expectStatus(t, w, 200)
	if corrected := decodeBody[ProgressNote](t, w); corrected.Lesson == nil || corrected.Lesson.BookingID != attended.ID {
		t.Fatal(corrected)
	}
	if _, err := db.Exec(ctx, "UPDATE progress_notes SET booking_id=NULL,class_id=NULL,lesson_name=NULL,lesson_starts_at=NULL WHERE id=$1", first.ID); err == nil {
		t.Fatal("lesson link was rewritten")
	}
	exec("UPDATE class_types SET name='Renamed' WHERE id=(SELECT class_type_id FROM class_sessions WHERE id=$1)", class.ID)
	w = request(t, h, "GET", path, nil, parent)
	expectStatus(t, w, 200)
	for _, item := range decodeBody[struct {
		Items []ProgressNote `json:"items"`
	}](t, w).Items {
		if item.Lesson != nil && item.Lesson.Name != first.Lesson.Name {
			t.Fatal("class rename rewrote recorded lesson context")
		}
	}
	var audited string
	if err := db.QueryRow(ctx, "SELECT details->'lesson'->>'bookingId' FROM audit_events WHERE entity_id=$1 AND action='progress.created'", first.ID).Scan(&audited); err != nil || audited != attended.ID {
		t.Fatal(audited, err)
	}
}
