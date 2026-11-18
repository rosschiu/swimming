package app

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegrationHomeSummariesByRole(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	monday := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 5)
	later := createBookingClass(t, f, "2030-01-14", "10:00", "11:00", 5)
	createBookingClass(t, f, "2030-01-08", "10:00", "11:00", 5)
	expiry := "2030-01-09"
	seedBookingCredit(t, db, "amy-pack", "amy", "parent", 3, &expiry)
	seedBookingCredit(t, db, "amy-open", "amy", "parent", 1, nil)
	seedBookingCredit(t, db, "ben-pack", "ben", "other-parent", 1, nil)
	parent := login(t, h, "parent@swim.test")
	expectStatus(t, request(t, h, "POST", "/api/v1/students/amy/bookings", bookingBody(monday, "amy-pack", "home-monday"), parent), 201)
	expectStatus(t, request(t, h, "POST", "/api/v1/students/amy/bookings", bookingBody(later, "amy-open", "home-later"), parent), 201)
	if _, err := db.Exec(ctx, `
INSERT INTO lesson_change_requests(id,booking_id,original_class_id,target_class_id,original_class,target_class,requested_by,requested_at,settings_version,idempotency_key)
SELECT 'home-request',b.id,b.class_id,$1,'{}','{}','parent',now(),1,'home-request' FROM bookings b WHERE b.idempotency_key='home-monday'`, later.ID); err != nil {
		t.Fatal(err)
	}

	home := func(role, id string, now time.Time) Home {
		t.Helper()
		w := httptest.NewRecorder()
		(&API{db: db, now: func() time.Time { return now }}).getHome(w, httptest.NewRequest("GET", "/api/v1/home", nil), Session{User: User{ID: id, Role: role}})
		expectStatus(t, w, 200)
		return decodeBody[Home](t, w)
	}
	at := func(value string) time.Time {
		x, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}

	got := home("parent", "parent", at("2030-01-07T09:00:00+08:00"))
	if got.Today != "2030-01-07" || len(got.Students) != 1 || got.Students[0].ID != "amy" || got.PendingLessonChanges != 1 {
		t.Fatalf("unexpected guardian home: %+v", got)
	}
	amy := got.Students[0]
	if amy.NextLesson == nil || amy.NextLesson.Class.ID != monday.ID || amy.UpcomingCount != 2 || amy.PendingChanges != 1 {
		t.Fatalf("unexpected next lesson: %+v", amy)
	}
	if amy.Credits == nil || amy.Credits.Available != 2 || amy.Credits.Reserved != 2 || amy.Credits.NextExpiry == nil || *amy.Credits.NextExpiry != expiry {
		t.Fatalf("unexpected credits: %+v", amy.Credits)
	}

	// After the Monday lesson ends and the dated package expires, only the open-ended package remains usable.
	got = home("parent", "parent", at("2030-01-10T09:00:00+08:00"))
	amy = got.Students[0]
	if amy.NextLesson == nil || amy.NextLesson.Class.ID != later.ID || amy.UpcomingCount != 1 {
		t.Fatalf("ended lesson still upcoming: %+v", amy)
	}
	if amy.Credits.Available != 0 || amy.Credits.NextExpiry != nil {
		t.Fatalf("expired package still counted: %+v", amy.Credits)
	}

	// Students see their own lessons without guardian-only credit or request details.
	got = home("student", "student", at("2030-01-07T09:00:00+08:00"))
	if len(got.Students) != 1 || got.Students[0].Credits != nil || got.Students[0].NextLesson == nil || got.PendingLessonChanges != 0 {
		t.Fatalf("unexpected student home: %+v", got)
	}

	got = home("parent", "other-parent", at("2030-01-07T09:00:00+08:00"))
	if len(got.Students) != 1 || got.Students[0].ID != "ben" || got.Students[0].NextLesson != nil || got.Students[0].Credits.Available != 1 {
		t.Fatalf("other guardian sees unexpected records: %+v", got)
	}

	got = home("coach", "coach", at("2030-01-07T09:00:00+08:00"))
	if len(got.Classes) != 3 || got.Classes[0].Class.ID != monday.ID || got.Classes[0].Booked != 1 || got.Classes[0].Unmarked != 1 || got.PendingLessonChanges != 1 {
		t.Fatalf("unexpected coach home: %+v", got)
	}
	if _, err := db.Exec(ctx, "INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Coach Two','coach',password_hash FROM users WHERE id='coach'"); err != nil {
		t.Fatal(err)
	}
	if got = home("coach", "coach2", at("2030-01-07T09:00:00+08:00")); len(got.Classes) != 0 || got.PendingLessonChanges != 0 {
		t.Fatalf("unassigned coach sees lessons: %+v", got)
	}

	got = home("admin", "admin", at("2030-01-07T09:00:00+08:00"))
	if len(got.Classes) != 1 || got.Classes[0].Class.ID != monday.ID || got.ActionQueue == nil || got.ActionQueue.PendingLessonChanges != 1 || len(got.Students) != 0 {
		t.Fatalf("unexpected administrator home: %+v", got)
	}

	expectStatus(t, request(t, h, "GET", "/api/v1/home", nil, authState{}), 401)
	expectStatus(t, request(t, h, "GET", "/api/v1/home", nil, parent), 200)
	if _, err := db.Exec(ctx, "UPDATE users SET active=false WHERE id='parent'"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	(&API{db: db, now: time.Now}).getHome(w, httptest.NewRequest("GET", "/api/v1/home", nil), Session{User: User{ID: "parent", Role: "parent"}})
	expectStatus(t, w, 401)
}
