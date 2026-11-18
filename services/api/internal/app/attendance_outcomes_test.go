package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegrationAttendanceOutcomesAndCorrections(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	first := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 4)
	second := createBookingClass(t, f, "2030-01-07", "12:00", "13:00", 4)
	third := createBookingClass(t, f, "2030-01-08", "10:00", "11:00", 4)
	target := createBookingClass(t, f, "2030-01-09", "10:00", "11:00", 4)
	late := createBookingClass(t, f, "2030-01-10", "10:00", "11:00", 4)
	expiry, shortExpiry := "2030-01-31", "2030-01-10"
	seedBookingCredit(t, db, "outcome-pack", "amy", "parent", 3, &expiry)
	seedBookingCredit(t, db, "strict-pack", "amy", "parent", 1, nil)
	seedBookingCredit(t, db, "short-pack", "amy", "parent", 1, &shortExpiry)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE credit_packages SET policy=jsonb_set(policy,'{absenceRetainsCredit}','false') WHERE id='strict-pack'`)
	exec("INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Second Coach','coach',password_hash FROM users WHERE id='coach'")
	parent := login(t, h, "parent@swim.test")
	coach := login(t, h, "coach@swim.test")
	otherCoach := login(t, h, "coach2@swim.test")
	exec("UPDATE sessions SET expires_at='2031-01-01'")

	book := func(class ClassSession, pack, key string) StudentBooking {
		t.Helper()
		w := request(t, h, "POST", "/api/v1/students/amy/bookings", bookingBody(class, pack, key), parent)
		expectStatus(t, w, 201)
		return decodeBody[StudentBooking](t, w)
	}
	a1 := book(first, "outcome-pack", "outcome-a")
	b1 := book(second, "outcome-pack", "outcome-b")
	strict := book(third, "strict-pack", "outcome-strict")
	short := book(late, "short-pack", "outcome-short")

	now := first.StartsAt.Add(20 * time.Minute)
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return now }}
	routes := http.NewServeMux()
	a.bookingRoutes(routes)
	a.lessonChangeRoutes(routes)
	mark := func(class ClassSession, b StudentBooking, status string, actor authState) *httptest.ResponseRecorder {
		return request(t, routes, "PUT", "/api/v1/classes/"+class.ID+"/roster/"+b.ID+"/attendance", map[string]string{"status": status}, actor)
	}
	correct := func(class ClassSession, b StudentBooking, status string, revision int, key string, actor authState) *httptest.ResponseRecorder {
		return request(t, routes, "POST", "/api/v1/classes/"+class.ID+"/roster/"+b.ID+"/attendance/corrections",
			attendanceCorrectionInput{Status: status, Reason: "Register checked", Revision: revision, IdempotencyKey: key}, actor)
	}
	balance := func(pack string) [4]int {
		t.Helper()
		var x [4]int
		if err := db.QueryRow(ctx, "SELECT sum(available),sum(reserved),sum(consumed),sum(expired) FROM credit_movements WHERE package_id=$1", pack).
			Scan(&x[0], &x[1], &x[2], &x[3]); err != nil {
			t.Fatal(err)
		}
		return x
	}
	outcome := func(w *httptest.ResponseRecorder, status, credit string, revision int) {
		t.Helper()
		expectStatus(t, w, 200)
		got := decodeBody[BookingAttendance](t, w)
		if got.Status != status || got.CreditOutcome != credit || got.Revision != revision {
			t.Fatalf("expected %s/%s/%d, got %+v", status, credit, revision, got)
		}
	}

	// Absence waits for lesson end; scope and roles are rechecked.
	expectBookingError(t, mark(first, a1, "absent", coach), 409, "attendance_not_open")
	expectBookingError(t, mark(first, a1, "late", coach), 422, "validation_error")
	expectBookingError(t, mark(first, a1, "absent", parent), 403, "forbidden")
	expectBookingError(t, mark(first, a1, "absent", otherCoach), 404, "not_found")
	now = first.EndsAt
	outcome(mark(first, a1, "absent", coach), "absent", "retained", 1)
	outcome(mark(first, a1, "absent", coach), "absent", "retained", 1)
	expectBookingError(t, mark(first, a1, "present", coach), 409, "attendance_recorded")
	if got := balance("outcome-pack"); got != [4]int{2, 1, 0, 0} {
		t.Fatalf("absence with retention should return one unit: %v", got)
	}

	// Recording attendance closes a pending move for that booking.
	exec(`INSERT INTO lesson_change_requests(id,booking_id,original_class_id,target_class_id,original_class,target_class,requested_by,requested_at,settings_version,idempotency_key)
		VALUES('outcome-change',$1,$2,$3,'{}','{}','parent',now(),1,'outcome-change')`, b1.ID, second.ID, target.ID)
	now = second.StartsAt.Add(20 * time.Minute)
	outcome(request(t, routes, "PUT", "/api/v1/classes/"+second.ID+"/roster/"+b1.ID+"/attendance", nil, f.admin), "present", "consumed", 1)
	var changeStatus, changeReason string
	if err := db.QueryRow(ctx, "SELECT status,decision_reason FROM lesson_change_requests WHERE id='outcome-change'").Scan(&changeStatus, &changeReason); err != nil || changeStatus != "closed" || changeReason != "Attendance recorded" {
		t.Fatalf("pending change not closed: %s %s %v", changeStatus, changeReason, err)
	}
	expectBookingError(t, request(t, routes, "PUT", "/api/v1/lesson-changes/outcome-change/decision", map[string]string{"decision": "approved"}, coach), 409, "change_conflict")
	if got := balance("outcome-pack"); got != [4]int{2, 0, 1, 0} {
		t.Fatalf("present should consume: %v", got)
	}

	// Corrections move credit only when the credit effect changes, and replay exactly once.
	now = first.EndsAt.Add(time.Hour)
	expectBookingError(t, correct(first, a1, "present", 2, "stale", coach), 409, "attendance_changed")
	outcome(correct(first, a1, "present", 1, "to-present", coach), "present", "consumed", 2)
	outcome(correct(first, a1, "present", 1, "to-present", coach), "present", "consumed", 2)
	expectBookingError(t, request(t, routes, "POST", "/api/v1/classes/"+first.ID+"/roster/"+a1.ID+"/attendance/corrections",
		attendanceCorrectionInput{Status: "ill", Reason: "Different", Revision: 1, IdempotencyKey: "to-present"}, coach), 409, "conflict")
	if got := balance("outcome-pack"); got != [4]int{1, 0, 2, 0} {
		t.Fatalf("retained to consumed should take back one unit: %v", got)
	}
	outcome(correct(first, a1, "ill", 2, "to-ill", f.admin), "ill", "retained", 3)
	outcome(correct(first, a1, "absent", 3, "to-absent", coach), "absent", "retained", 4)
	if got := balance("outcome-pack"); got != [4]int{2, 0, 1, 0} {
		t.Fatalf("illness then absence should return exactly one unit: %v", got)
	}
	expectBookingError(t, correct(first, a1, "absent", 4, "same", coach), 422, "validation_error")

	// Once the returned units are spent elsewhere the correction cannot debit another package.
	book(target, "outcome-pack", "spend-1")
	spare := createBookingClass(t, f, "2030-01-09", "12:00", "13:00", 4)
	book(spare, "outcome-pack", "spend-2")
	expectBookingError(t, correct(first, a1, "present", 4, "irreversible", coach), 409, "credit_not_reversible")
	if got := balance("strict-pack"); got != [4]int{0, 1, 0, 0} {
		t.Fatalf("unrelated package changed: %v", got)
	}

	// The window closes at lesson end plus three days, inclusive.
	now = first.EndsAt.Add(72 * time.Hour)
	outcome(correct(second, b1, "absent", 1, "at-deadline", coach), "absent", "retained", 2)
	now = first.EndsAt.Add(72*time.Hour + time.Microsecond)
	expectBookingError(t, correct(first, a1, "ill", 4, "after-deadline", coach), 409, "correction_closed")

	// A package policy that does not retain absences uses the credit.
	now = third.EndsAt
	outcome(mark(third, strict, "absent", coach), "absent", "consumed", 1)
	if got := balance("strict-pack"); got != [4]int{0, 0, 1, 0} {
		t.Fatalf("non-retaining absence should consume: %v", got)
	}

	// A credit kept after its package expired goes to expired history, and a correction can take it back from there.
	now = late.EndsAt.Add(48 * time.Hour)
	outcome(mark(late, short, "ill", coach), "ill", "expired", 1)
	if got := balance("short-pack"); got != [4]int{0, 0, 0, 1} {
		t.Fatalf("expired retention should not become available: %v", got)
	}
	outcome(correct(late, short, "present", 1, "expired-to-present", coach), "present", "consumed", 2)
	if got := balance("short-pack"); got != [4]int{0, 0, 1, 0} {
		t.Fatalf("correction should take back the unrecovered expired unit: %v", got)
	}

	var corrections, audits int
	if err := db.QueryRow(ctx, "SELECT (SELECT count(*) FROM attendance_corrections),(SELECT count(*) FROM audit_events WHERE action='attendance.corrected')").Scan(&corrections, &audits); err != nil {
		t.Fatal(err)
	}
	if corrections != 5 || audits != 5 {
		t.Fatalf("expected five corrections and audits, got %d and %d", corrections, audits)
	}
	if _, err := db.Exec(ctx, "UPDATE attendance_corrections SET reason='edited'"); err == nil {
		t.Fatal("correction history must be immutable")
	}

	// Families read the current outcome without staff reasons.
	w := request(t, h, "GET", "/api/v1/students/amy/bookings/"+a1.ID, nil, parent)
	expectStatus(t, w, 200)
	if got := decodeBody[StudentBookingDetail](t, w); got.Attendance == nil || got.Attendance.Status != "absent" || got.Attendance.CreditOutcome != "retained" {
		t.Fatalf("family readback: %+v", got.Attendance)
	}
	w = request(t, routes, "GET", "/api/v1/classes/"+first.ID+"/roster", nil, coach)
	expectStatus(t, w, 200)
	roster := decodeBody[ClassRoster](t, w)
	if !roster.AbsenceOpensAt.Equal(first.EndsAt) || !roster.CorrectionClosesAt.Equal(first.EndsAt.Add(72*time.Hour)) {
		t.Fatalf("roster timing: %+v", roster)
	}
}
