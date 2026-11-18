package app

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestIntegrationDemoSchoolSeed(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	h := testHandler(db)
	now := time.Now()
	exec := func(sql string) {
		t.Helper()
		if _, err := db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE students SET name='Existing Amy' WHERE id='amy'")
	exec("ALTER TABLE booking_attendance ADD CONSTRAINT reject_demo CHECK (booking_id NOT LIKE 'demo-%')")
	if err := SeedDemoSchool(ctx, db, now); err == nil {
		t.Fatal("expected fixture rollback")
	}
	var count int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM students").Scan(&count); err != nil || count != 2 {
		t.Fatalf("failed seed left %d swimmers, error %v", count, err)
	}
	exec("ALTER TABLE booking_attendance DROP CONSTRAINT reject_demo")
	f := prepareScheduling(t, h)
	today := now.In(hongKong)
	conflictingDate := today.AddDate(0, 0, 7-(int(today.Weekday())+6)%7).Format("2006-01-02")
	// A different coach avoids triggering the coach overlap check first.
	exec("INSERT INTO users(id,email,name,role,password_hash) VALUES('fixture-other-coach','fixture-other-coach@swim.test','Existing Coach','coach','unused')")
	classInput := f.input(f.venue.ID, conflictingDate, "17:00", "18:00")
	classInput.CoachID = "fixture-other-coach"
	w := request(t, h, "POST", "/api/v1/classes", classInput, f.admin)
	expectStatus(t, w, http.StatusCreated)
	class := decodeBody[ClassSession](t, w)
	seedBookingCredit(t, db, "existing-pack", "amy", "parent", 1, nil)
	parent := login(t, h, "parent@swim.test")
	w = request(t, h, "POST", "/api/v1/students/amy/bookings", bookingBody(class, "existing-pack", "existing-booking"), parent)
	expectStatus(t, w, http.StatusCreated)
	if err := SeedDemoSchool(ctx, db, now); err == nil || !strings.Contains(err.Error(), "demo booking overlaps an existing booking") {
		t.Fatalf("expected overlap to abort seed, got %v", err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM bookings").Scan(&count); err != nil || count != 1 {
		t.Fatalf("overlap left %d bookings, error %v", count, err)
	}
}

func TestIntegrationDemoSchoolReplayAndScope(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	h := testHandler(db)
	now := time.Now()
	if _, err := db.Exec(ctx, "UPDATE students SET name='Existing Amy' WHERE id='amy'"); err != nil {
		t.Fatal(err)
	}
	var count int
	for _, date := range []time.Time{now, now.AddDate(0, 0, 7)} {
		if err := SeedDemoSchool(ctx, db, date); err != nil {
			t.Fatal(err)
		}
		for table, want := range map[string]int{
			"students": 20, "users": 25, "venues": 3, "class_series": 3, "class_sessions": 15,
			"bookings": 60, "booking_attendance": 20, "enrolments": 20, "purchases": 20,
			"payment_attempts": 20, "receipts": 20, "progress_notes": 20, "notification_deliveries": 0,
		} {
			if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != want {
				t.Fatalf("%s count %d, want %d, error %v", table, count, want, err)
			}
		}
	}
	var available, reserved, consumed int
	if err := db.QueryRow(ctx, `SELECT sum(m.available),sum(m.reserved),sum(m.consumed) FROM credit_movements m
		JOIN credit_packages cp ON cp.id=m.package_id WHERE cp.student_id='amy'`).Scan(&available, &reserved, &consumed); err != nil || available != 5 || reserved != 2 || consumed != 1 {
		t.Fatalf("Amy ledger %d/%d/%d, error %v", available, reserved, consumed, err)
	}

	parent := login(t, h, "parent@swim.test")
	student := login(t, h, "student@swim.test")
	coach := login(t, h, "coach@swim.test")
	admin := login(t, h, "admin@swim.test")
	for _, account := range []authState{parent, student, coach, admin} {
		w := request(t, h, "GET", "/api/v1/students/amy", nil, account)
		expectStatus(t, w, http.StatusOK)
		if got := decodeBody[Student](t, w); got.Name != "Existing Amy" {
			t.Fatalf("existing profile overwritten: %+v", got)
		}
	}
	expectStatus(t, request(t, h, "GET", "/api/v1/students/ben", nil, parent), http.StatusNotFound)
	expectStatus(t, request(t, h, "GET", "/api/v1/students/ben", nil, student), http.StatusNotFound)
	expectStatus(t, request(t, h, "GET", "/api/v1/classes/demo-group-1-3/roster", nil, coach), http.StatusOK)
	expectStatus(t, request(t, h, "GET", "/api/v1/classes/demo-group-2-3/roster", nil, coach), http.StatusNotFound)
}
