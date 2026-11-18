package app

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestIntegrationCancellationImpact(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	anchor := time.Date(2030, time.January, 7, 0, 0, 0, 0, hongKong)
	if err := SeedDemoSchool(ctx, db, anchor); err != nil {
		t.Fatal(err)
	}
	handler := testHandler(db)
	admin := login(t, handler, "admin@swim.test")
	const historical = "/api/v1/classes/demo-group-1-1/cancellation-impact"
	const upcoming = "/api/v1/classes/demo-group-1-3/cancellation-impact"
	const empty = "/api/v1/classes/demo-group-1-2/cancellation-impact"
	for role, code := range map[string]int{"parent": 403, "coach": 403, "student": 403} {
		account := login(t, handler, role+"@swim.test")
		expectStatus(t, request(t, handler, "GET", historical, nil, account), code)
	}
	expectStatus(t, request(t, handler, "GET", historical, nil, authState{}), 401)
	expectStatus(t, request(t, handler, "GET", "/api/v1/classes/unknown/cancellation-impact", nil, admin), 404)
	expectStatus(t, request(t, handler, "GET", historical+"?section=unknown", nil, admin), 422)
	expectStatus(t, request(t, handler, "GET", historical+"?cursor=broken", nil, admin), 422)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	now := anchor.AddDate(0, 0, 43)
	exec(`INSERT INTO lesson_change_requests(id,booking_id,original_class_id,target_class_id,original_class,target_class,
		requested_by,requested_at,settings_version,idempotency_key)
		SELECT 'impact-'||b.id,b.id,b.class_id,'demo-group-1-2','{}','{}',b.actor_id,$1,1,b.id
		FROM bookings b WHERE b.class_id='demo-group-1-3'`, anchor)
	exec(`UPDATE lesson_change_requests SET status='rejected',decided_at=$1,decided_by='coach'
		WHERE booking_id='demo-booking-amy-3'`, anchor)
	exec("UPDATE sessions SET expires_at=$1", now.Add(time.Hour))
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return now }}
	mux := http.NewServeMux()
	a.bookingRoutes(mux)
	read := func(path string) cancellationImpact {
		t.Helper()
		w := request(t, mux, "GET", path, nil, admin)
		expectStatus(t, w, 200)
		return decodeBody[cancellationImpact](t, w)
	}
	snapshot := func() string {
		t.Helper()
		var x string
		err := db.QueryRow(ctx, `SELECT concat(
			(SELECT md5(string_agg(to_jsonb(t)::text,'' ORDER BY id)) FROM credit_movements t),
			(SELECT md5(string_agg(to_jsonb(t)::text,'' ORDER BY id)) FROM bookings t),
			(SELECT md5(string_agg(to_jsonb(t)::text,'' ORDER BY booking_id)) FROM booking_attendance t),
			(SELECT md5(string_agg(to_jsonb(t)::text,'' ORDER BY id)) FROM lesson_change_requests t),
			(SELECT md5(string_agg(to_jsonb(t)::text,'' ORDER BY id)) FROM audit_events t))`).Scan(&x)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	before := snapshot()
	past := read(historical)
	future := read(upcoming)
	if past.RecordedAttendance != 7 || past.UnmarkedBookings != 0 ||
		future.RecordedAttendance != 0 || future.UnmarkedBookings != 7 {
		t.Fatalf("unexpected attendance counts: past %+v, future %+v", past, future)
	}
	findAmy := func(result cancellationImpact) cancellationImpactBooking {
		t.Helper()
		for _, booking := range result.Bookings {
			if booking.StudentID == "amy" {
				return booking
			}
		}
		t.Fatal("Amy missing from expected occurrence")
		return cancellationImpactBooking{}
	}
	attended, unmarked := findAmy(past), findAmy(future)
	if attended.PackageID != unmarked.PackageID || attended.EnrolmentID == nil || !attended.PackageExpired ||
		!attended.PackageCoversLesson || attended.AttendanceMarkedAt == nil || unmarked.AttendanceMarkedAt != nil {
		t.Fatalf("shared-package attendance and past validity: %+v, %+v", attended, unmarked)
	}
	initial := read(historical + "?limit=3")
	seen := map[string]bool{}
	for page := initial; ; page = read(historical + "?limit=3&cursor=" + url.QueryEscape(*page.NextCursor)) {
		if page.BookedSwimmers != 7 || len(page.Bookings) > 3 {
			t.Fatalf("unbounded or incorrect page %+v", page)
		}
		for _, item := range page.Bookings {
			if seen[item.ID] {
				t.Fatalf("duplicate row %s", item.ID)
			}
			seen[item.ID] = true
		}
		if page.NextCursor == nil {
			break
		}
	}
	if len(seen) != 7 {
		t.Fatalf("paged only %d of seven", len(seen))
	}
	if result := read(empty); result.BookedSwimmers != 0 || len(result.Bookings) != 0 || result.NextCursor != nil {
		t.Fatalf("expected empty occurrence: %+v", result)
	}
	outbound := read(upcoming + "?section=outbound&limit=5")
	if outbound.PendingOutbound != 6 || len(outbound.Requests) != 5 || outbound.NextCursor == nil {
		t.Fatalf("pending outbound page or count: %+v", outbound)
	}
	other := read(upcoming + "?section=outbound&limit=5&cursor=" + url.QueryEscape(*outbound.NextCursor))
	inbound := read(empty + "?section=inbound")
	if len(other.Requests) != 1 || other.NextCursor != nil || inbound.PendingInbound != 6 || len(inbound.Requests) != 6 {
		t.Fatalf("remaining outbound page or inbound count: %+v, %+v", other, inbound)
	}
	requestIDs := map[string]bool{}
	for _, pending := range append(outbound.Requests, other.Requests...) {
		if requestIDs[pending.ID] || pending.OtherClassID != "demo-group-1-2" || pending.OtherVenue != "Harbour Pool" {
			t.Fatalf("unexpected outbound request: %+v", pending)
		}
		requestIDs[pending.ID] = true
	}
	for _, pending := range inbound.Requests {
		if !requestIDs[pending.ID] || pending.BookingID == "demo-booking-amy-3" || pending.OtherClassID != "demo-group-1-3" {
			t.Fatalf("unexpected inbound request: %+v", pending)
		}
	}
	if after := snapshot(); after != before {
		t.Fatal("preview performed a write, possibly expiring available credits")
	}

	exec("UPDATE credit_packages SET expires_on=NULL WHERE id=$1", attended.PackageID)
	withoutExpiry := findAmy(read(historical))
	if withoutExpiry.ExpiresOn != nil || withoutExpiry.PackageExpired || !withoutExpiry.PackageCoversLesson {
		t.Fatalf("no-expiry package incorrectly reported: %+v", withoutExpiry)
	}
	exec("UPDATE users SET active=false WHERE id='admin'")
	expectStatus(t, request(t, mux, "GET", upcoming, nil, admin), 401)
}
