package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIntegrationBookingReadsAndScope(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	// This January 7 Hong Kong class starts on January 6 UTC.
	input := f.input(f.venue.ID, "2030-01-07", "00:15", "01:00")
	w := request(t, h, "POST", "/api/v1/classes", input, f.admin)
	expectStatus(t, w, 201)
	class := decodeBody[ClassSession](t, w)
	w = request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-01-08", "10:00", "11:00"), f.admin)
	expectStatus(t, w, 201)
	emptyClass := decodeBody[ClassSession](t, w)
	if _, err := db.Exec(ctx, `
INSERT INTO users(id,email,name,role,password_hash)
  SELECT 'coach2','coach2@swim.test','Coach Two','coach',password_hash FROM users WHERE id='coach';
INSERT INTO purchases(id,student_id,created_by,idempotency_key,request,terms,status,created_at)
  VALUES ('paid-amys-purchase','amy','parent','purchase-test','{}','{}','paid',now());
INSERT INTO credit_packages(id,purchase_id,student_id,credits,policy,created_at)
  SELECT 'private-package','paid-amys-purchase','amy',2,policy,now() FROM credit_settings;
INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
  VALUES ('grant-1','private-package','grant-1',2,0,0,0,'parent','Test purchase',now());
`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO bookings(id,student_id,class_id,credit_package_id,actor_id,idempotency_key,request,class_version,class_type_version,student_level,class_level,validity_rule,created_at)
  VALUES ('booked-lesson','amy',$1,'private-package','parent','booking-test','{}',1,1,'Beginner','Beginner','lesson_end',now())
`, class.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
  VALUES ('reservation-1','private-package','booking:booked-lesson:reserved',-1,1,0,0,'parent','Test reservation',now())
`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ student, key, code string }{
		{"ben", "other-student", "23503"},
		{"amy", "other-request", "23505"},
	} {
		_, err := db.Exec(ctx, `
INSERT INTO bookings(id,student_id,class_id,credit_package_id,actor_id,idempotency_key,request,class_version,class_type_version,student_level,class_level,validity_rule,created_at)
  VALUES ('invalid-booking',$1,$2,'private-package','parent',$3,'{}',1,1,'Beginner','Beginner','lesson_end',now())
`, tc.student, class.ID, tc.key)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
			t.Fatalf("expected booking constraint %s for %s, got %v", tc.code, tc.key, err)
		}
	}
	parent := login(t, h, "parent@swim.test")
	child := login(t, h, "student@swim.test")
	otherParent := login(t, h, "other-parent@swim.test")
	coach := login(t, h, "coach@swim.test")
	otherCoach := login(t, h, "coach2@swim.test")

	type bookingList struct {
		Items []StudentBooking `json:"items"`
	}
	date := "?from=2030-01-07&to=2030-01-07"
	lessons := "/api/v1/students/amy/bookings"
	roster := "/api/v1/classes/" + class.ID + "/roster"
	for _, actor := range []authState{parent, child, f.admin} {
		w := request(t, h, "GET", lessons+date, nil, actor)
		expectStatus(t, w, 200)
		items := decodeBody[bookingList](t, w).Items
		if len(items) != 1 || items[0].ID != "booked-lesson" || items[0].StudentID != "amy" ||
			items[0].Status != "reserved" || items[0].Class.ID != class.ID || items[0].CreatedAt.IsZero() {
			t.Fatalf("wrong booked lesson: %+v", items)
		}
		if strings.Contains(w.Body.String(), "private-package") {
			t.Fatal("student booking response exposed the credit package")
		}
	}
	expectStatus(t, request(t, h, "GET", lessons+date, nil, otherParent), 404)
	expectStatus(t, request(t, h, "GET", "/api/v1/students/ben/bookings"+date, nil, child), 404)
	expectStatus(t, request(t, h, "GET", lessons+date, nil, coach), 403)
	expectStatus(t, request(t, h, "GET", lessons, nil, parent), 422)
	w = request(t, h, "GET", lessons+"?from=2030-01-06&to=2030-01-06", nil, child)
	expectStatus(t, w, 200)
	if items := decodeBody[bookingList](t, w).Items; items == nil || len(items) != 0 {
		t.Fatalf("expected empty array outside the Hong Kong range: %+v", items)
	}

	w = request(t, h, "GET", roster, nil, coach)
	expectStatus(t, w, 200)
	got := decodeBody[ClassRoster](t, w)
	expected := RosterEntry{"booked-lesson", "amy", "Amy", "Beginner", "reserved"}
	if got.ClassID != class.ID || len(got.Items) != 1 || got.Items[0] != expected {
		t.Fatalf("wrong roster: %+v", got)
	}
	if strings.Contains(w.Body.String(), "private-package") {
		t.Fatal("roster response exposed the credit package")
	}
	expectStatus(t, request(t, h, "GET", roster, nil, parent), 403)
	expectStatus(t, request(t, h, "GET", roster, nil, child), 403)
	expectStatus(t, request(t, h, "GET", roster, nil, otherCoach), 404)
	w = request(t, h, "GET", "/api/v1/classes/"+emptyClass.ID+"/roster", nil, f.admin)
	expectStatus(t, w, 200)
	if items := decodeBody[ClassRoster](t, w).Items; items == nil || len(items) != 0 {
		t.Fatalf("expected empty roster array: %+v", items)
	}

	if _, err := db.Exec(ctx, `
INSERT INTO purchases(id,student_id,created_by,idempotency_key,request,terms,status,created_at)
  VALUES ('paid-bens-purchase','ben','parent','purchase-test-ben','{}','{}','paid',now());
INSERT INTO credit_packages(id,purchase_id,student_id,credits,policy,created_at)
  SELECT 'ben-package','paid-bens-purchase','ben',2,policy,now() FROM credit_settings;
INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
  VALUES ('ben-grant','ben-package','ben-grant',2,0,0,0,'parent','Test purchase',now());
`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO bookings(id,student_id,class_id,credit_package_id,actor_id,idempotency_key,request,class_version,class_type_version,student_level,class_level,validity_rule,created_at)
  VALUES ('booked-lesson-ben','ben',$1,'ben-package','parent','booking-test-ben','{}',1,1,'Beginner','Beginner','lesson_end',now())
`, class.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `
INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
  VALUES ('ben-reservation','ben-package','booking:booked-lesson-ben:reserved',-1,1,0,0,'parent','Test reservation',now())
`); err != nil {
		t.Fatal(err)
	}
	classURL := "/api/v1/classes/" + class.ID
	edit := struct {
		ClassInput
		Version int `json:"version"`
	}{input, class.Version}
	for _, change := range []struct {
		name  string
		apply func(*ClassInput)
	}{
		{"venue", func(x *ClassInput) { x.VenueID = f.other.ID }},
		{"class type", func(x *ClassInput) { x.ClassTypeID = "other-type" }},
		{"start", func(x *ClassInput) { x.StartsAt = x.StartsAt.Add(time.Minute) }},
		{"end", func(x *ClassInput) { x.EndsAt = x.EndsAt.Add(time.Minute) }},
		{"capacity", func(x *ClassInput) { x.Capacity = 1 }},
	} {
		changed := edit
		change.apply(&changed.ClassInput)
		w := request(t, h, "PATCH", classURL, changed, f.admin)
		expectStatus(t, w, 409)
		if !strings.Contains(w.Body.String(), `"code":"booked_class_conflict"`) {
			t.Fatalf("%s edit returned wrong error: %s", change.name, w.Body)
		}
	}
	edit.CoachID = "coach2"
	edit.Capacity = 2
	expectStatus(t, request(t, h, "PATCH", classURL, edit, f.admin), 200)
	expectStatus(t, request(t, h, "GET", roster, nil, coach), 404)
	expectStatus(t, request(t, h, "GET", roster, nil, otherCoach), 200)
	expectStatus(t, request(t, h, "DELETE", "/api/v1/students/amy/relationships/parent", nil, f.admin), 204)
	expectStatus(t, request(t, h, "GET", lessons+date, nil, parent), 404)
	expectStatus(t, request(t, h, "GET", lessons+date, nil, child), 200)
}
