package app

import (
	"cmp"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestIntegrationFixedEnrolmentAtomicReservationAndReplay(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	w := request(t, h, "POST", "/api/v1/class-series", SeriesInput{
		VenueID: f.venue.ID, ClassTypeID: f.kind.ID, CoachID: "coach",
		StartDate: "2030-01-07", EndDate: "2030-01-21", Weekday: 1, StartTime: "10:00", EndTime: "11:00", Capacity: 6,
	}, f.admin)
	expectStatus(t, w, 201)
	series := decodeBody[ClassSeries](t, w)
	parent := login(t, h, "parent@swim.test")
	student := login(t, h, "student@swim.test")
	other := login(t, h, "other-parent@swim.test")
	path := "/api/v1/students/amy/enrolments"
	in := enrolmentInput{SeriesID: series.ID, CreditPackageID: "enrolment-package", IdempotencyKey: "fixed-weekly"}
	for _, c := range series.Sessions {
		in.Sessions = append(in.Sessions, enrolmentSession{c.ID, c.Version, c.ClassTypeVersion})
	}
	seedBookingCredit(t, db, in.CreditPackageID, "amy", "parent", 3, nil)

	expectStatus(t, request(t, h, "POST", path, in, student), 403)
	expectStatus(t, request(t, h, "POST", path, in, other), 404)
	duplicate := in
	duplicate.Sessions = []enrolmentSession{in.Sessions[0], in.Sessions[0]}
	expectStatus(t, request(t, h, "POST", path, duplicate, parent), 422)

	// Failure on the final reservation must roll back prior reservations, audit and notifications.
	last := slices.MaxFunc(in.Sessions, func(a, b enrolmentSession) int {
		return cmp.Compare(a.ClassID, b.ClassID)
	})
	if _, err := db.Exec(ctx, "UPDATE class_sessions SET version=2 WHERE id=$1", last.ClassID); err != nil {
		t.Fatal(err)
	}
	expectBookingError(t, request(t, h, "POST", path, in, parent), 409, "version_conflict")
	for i := range in.Sessions {
		if in.Sessions[i].ClassID == last.ClassID {
			in.Sessions[i].ClassVersion++
		}
	}
	counts := func(wantEnrolments, wantBookings, wantCredits int) {
		t.Helper()
		var enrolments, bookings, movements, audits, notes, available int
		if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM enrolments), (SELECT count(*) FROM bookings),
			(SELECT count(*) FROM credit_movements WHERE source LIKE 'booking:%'),
			(SELECT count(*) FROM audit_events WHERE action IN ('booking.created','enrolment.created')),
			(SELECT count(*) FROM notifications WHERE kind='booking.created'),
			(SELECT sum(available) FROM credit_movements WHERE package_id=$1)`, in.CreditPackageID).
			Scan(&enrolments, &bookings, &movements, &audits, &notes, &available); err != nil {
			t.Fatal(err)
		}
		if enrolments != wantEnrolments || bookings != wantBookings || movements != wantBookings ||
			audits != wantBookings+wantEnrolments || notes != 2*wantBookings || available != wantCredits {
			t.Fatalf("unexpected enrolment state: enrolments=%d bookings=%d movements=%d audits=%d notifications=%d available=%d",
				enrolments, bookings, movements, audits, notes, available)
		}
	}
	counts(0, 0, 3)
	standalone := createBookingClass(t, f, "2030-01-08", "10:00", "11:00", 6)
	wrongSeries := in
	wrongSeries.Sessions = []enrolmentSession{{standalone.ID, standalone.Version, standalone.ClassTypeVersion}}
	expectBookingError(t, request(t, h, "POST", path, wrongSeries, parent), 422, "validation_error")
	counts(0, 0, 3)
	short := in
	short.CreditPackageID = "short-enrolment"
	seedBookingCredit(t, db, short.CreditPackageID, "amy", "parent", 2, nil)
	expectBookingError(t, request(t, h, "POST", path, short, parent), 409, "credit_unavailable")
	counts(0, 0, 3)
	var shortBalance int
	if err := db.QueryRow(ctx, "SELECT sum(available) FROM credit_movements WHERE package_id=$1", short.CreditPackageID).Scan(&shortBalance); err != nil || shortBalance != 2 {
		t.Fatalf("failed enrolment changed short package: balance=%d err=%v", shortBalance, err)
	}

	// The final class starts on the expiry date and ends the next morning.
	if _, err := db.Exec(ctx, `UPDATE class_sessions SET starts_at='2030-01-21T23:30:00+08', ends_at='2030-01-22T00:30:00+08'
		WHERE id=$1`, series.Sessions[len(series.Sessions)-1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "UPDATE credit_packages SET expires_on='2030-01-21' WHERE id=$1", in.CreditPackageID); err != nil {
		t.Fatal(err)
	}
	const settingsPath = "/api/v1/settings/booking-validity"
	before := 1
	in.BookingValidityVersion = &before
	expectBookingError(t, request(t, h, "POST", path, in, parent), 409, "credit_unavailable")
	counts(0, 0, 3)
	updated := request(t, h, "PATCH", settingsPath, BookingValiditySettings{bookingValidityLessonStart, before}, f.admin)
	expectStatus(t, updated, 200)
	current := decodeBody[BookingValiditySettings](t, updated)
	expectBookingError(t, request(t, h, "POST", path, in, parent), 409, "booking_validity_changed")
	counts(0, 0, 3)
	in.BookingValidityVersion = &current.Version

	reversed := in
	reversed.Sessions = slices.Clone(in.Sessions)
	slices.Reverse(reversed.Sessions)
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, body := range []enrolmentInput{in, reversed} {
		go func() { results <- request(t, h, "POST", path, body, parent) }()
	}
	created, replay := <-results, <-results
	if replay.Code == 201 {
		created, replay = replay, created
	}
	expectStatus(t, created, 201)
	expectStatus(t, replay, 200)
	if created.Body.String() != replay.Body.String() {
		t.Fatalf("concurrent retry changed enrolment: %s / %s", created.Body, replay.Body)
	}
	enrolment := decodeBody[Enrolment](t, created)
	if enrolment.ID == "" || enrolment.StudentID != "amy" || enrolment.SeriesID != series.ID ||
		enrolment.CreatedAt.IsZero() || len(enrolment.Bookings) != len(series.Sessions) {
		t.Fatalf("wrong enrolment: %+v", enrolment)
	}
	for i, booking := range enrolment.Bookings {
		if booking.ID == "" || booking.Class.ID != series.Sessions[i].ID || booking.Status != "reserved" {
			t.Fatalf("wrong reservation: %+v", booking)
		}
	}
	counts(1, 3, 0)
	expectStatus(t, request(t, h, "PATCH", settingsPath, BookingValiditySettings{bookingValidityLessonEnd, current.Version}, f.admin), 200)
	expectStatus(t, request(t, h, "POST", path, in, parent), 200)
	var snapshots int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM bookings WHERE enrolment_id=$1 AND validity_rule=$2", enrolment.ID, bookingValidityLessonStart).
		Scan(&snapshots); err != nil || snapshots != len(in.Sessions) {
		t.Fatalf("validity snapshots: got %d, err=%v", snapshots, err)
	}
	counts(1, 3, 0)

	reversed.Sessions = reversed.Sessions[:2]
	expectBookingError(t, request(t, h, "POST", path, reversed, parent), 409, "conflict")
	expectStatus(t, request(t, h, "DELETE", "/api/v1/students/amy/relationships/parent", nil, f.admin), 204)
	expectStatus(t, request(t, h, "POST", path, in, parent), 404)
}

func TestIntegrationFixedEnrolmentClockAfterPackageLock(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	parent := login(t, h, "parent@swim.test")
	w := request(t, h, "POST", "/api/v1/class-series", SeriesInput{
		VenueID: f.venue.ID, ClassTypeID: f.kind.ID, CoachID: "coach",
		StartDate: "2030-01-07", EndDate: "2030-01-07", Weekday: 1, StartTime: "10:00", EndTime: "11:00", Capacity: 6,
	}, f.admin)
	expectStatus(t, w, 201)
	series := decodeBody[ClassSeries](t, w)
	class := series.Sessions[0]
	in := enrolmentInput{SeriesID: series.ID, CreditPackageID: "blocked-pack", IdempotencyKey: "blocked-enrolment",
		Sessions: []enrolmentSession{{class.ID, class.Version, class.ClassTypeVersion}}}
	seedBookingCredit(t, db, in.CreditPackageID, "amy", "parent", 1, nil)
	ctx := context.Background()
	if _, err := db.Exec(ctx, "UPDATE sessions SET expires_at=$1 WHERE user_id='parent'", class.StartsAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(class.StartsAt.Add(-time.Second).UnixNano())
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return time.Unix(0, clock.Load()) }}
	mux := http.NewServeMux()
	a.bookingRoutes(mux)
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var blockerPID int
	if err = tx.QueryRow(ctx, "SELECT pg_backend_pid() FROM credit_packages WHERE id=$1 FOR UPDATE", in.CreditPackageID).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	results := make(chan *httptest.ResponseRecorder, 1)
	go func() { results <- request(t, mux, "POST", "/api/v1/students/amy/enrolments", in, parent) }()
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		var blocked bool
		if err = db.QueryRow(wait, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE query LIKE '%FOR UPDATE OF cp' AND $1=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&blocked); err != nil {
			t.Fatalf("enrolment did not wait for the package lock: %v", err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	clock.Store(class.StartsAt.UnixNano())
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-results:
		expectBookingError(t, result, 409, "class_unavailable")
	case <-time.After(5 * time.Second):
		t.Fatal("enrolment did not finish after package unlock")
	}
	var records, available int
	if err = db.QueryRow(ctx, `SELECT (SELECT count(*) FROM enrolments)+(SELECT count(*) FROM bookings)
		+(SELECT count(*) FROM notifications)+(SELECT count(*) FROM audit_events WHERE action IN ('booking.created','enrolment.created')),
		(SELECT sum(available) FROM credit_movements WHERE package_id=$1)`, in.CreditPackageID).Scan(&records, &available); err != nil || records != 0 || available != 1 {
		t.Fatalf("blocked enrolment changed state: records=%d available=%d err=%v", records, available, err)
	}
}

func TestIntegrationFixedEnrolmentExplicitSubset(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	parent := login(t, h, "parent@swim.test")
	const settingsPath = "/api/v1/settings/fixed-enrolment"
	settingsResponse := request(t, h, "GET", settingsPath, nil, f.admin)
	expectStatus(t, settingsResponse, 200)
	settings := decodeBody[FixedEnrolmentSettings](t, settingsResponse)
	if settings != (FixedEnrolmentSettings{fixedAllocateAllOrNone, fixedRenewalSameOffer, 1}) {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
	expectStatus(t, request(t, h, "GET", settingsPath, nil, parent), 403)
	expectStatus(t, request(t, h, "PATCH", settingsPath, settings, parent), 403)
	invalid := settings
	invalid.AllocationPolicy = "unknown"
	expectStatus(t, request(t, h, "PATCH", settingsPath, invalid, f.admin), 422)

	seriesResponse := request(t, h, "POST", "/api/v1/class-series", SeriesInput{
		VenueID: f.venue.ID, ClassTypeID: f.kind.ID, CoachID: "coach",
		StartDate: "2030-01-07", EndDate: "2030-01-21", Weekday: 1, StartTime: "10:00", EndTime: "11:00", Capacity: 1,
	}, f.admin)
	expectStatus(t, seriesResponse, 201)
	series := decodeBody[ClassSeries](t, seriesResponse)
	first, second, third := series.Sessions[0], series.Sessions[1], series.Sessions[2]
	in := enrolmentInput{SeriesID: series.ID, CreditPackageID: "partial-package", IdempotencyKey: "fixed-subset"}
	for _, c := range series.Sessions {
		in.Sessions = append(in.Sessions, enrolmentSession{c.ID, c.Version, c.ClassTypeVersion})
	}
	seedBookingCredit(t, db, in.CreditPackageID, "amy", "parent", 2, nil)

	settings.AllocationPolicy = fixedAllocateSubset
	settings.RenewalPolicy = fixedRenewalAnyPaidPackage
	updated := request(t, h, "PATCH", settingsPath, settings, f.admin)
	expectStatus(t, updated, 200)
	expectBookingError(t, request(t, h, "PATCH", settingsPath, settings, f.admin), 409, "version_conflict")
	settings = decodeBody[FixedEnrolmentSettings](t, updated)

	const path = "/api/v1/students/amy/enrolments"
	proposed := func(w *httptest.ResponseRecorder, expected ...string) partialEnrolmentConfirmation {
		t.Helper()
		expectBookingError(t, w, 409, "partial_enrolment_confirmation_required")
		details := decodeBody[struct {
			Error struct {
				Details struct {
					AvailableSessionIDs []string `json:"availableSessionIds"`
					SettingsVersion     int      `json:"settingsVersion"`
				} `json:"details"`
			} `json:"error"`
		}](t, w).Error.Details
		if details.SettingsVersion != settings.Version || !slices.Equal(details.AvailableSessionIDs, expected) {
			t.Fatalf("unexpected subset: %+v", details)
		}
		return partialEnrolmentConfirmation{details.AvailableSessionIDs, details.SettingsVersion}
	}
	assertNoWrites := func() {
		t.Helper()
		var writes, available int
		err := db.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM enrolments) +
			(SELECT count(*) FROM bookings WHERE student_id='amy') +
			(SELECT count(*) FROM notifications WHERE student_id='amy' AND kind='booking.created') +
			(SELECT count(*) FROM audit_events WHERE action IN ('booking.created','enrolment.created') AND details->>'studentId'='amy'),
			(SELECT sum(available) FROM credit_movements WHERE package_id=$1)`, in.CreditPackageID).Scan(&writes, &available)
		if err != nil || writes != 0 || available != 2 {
			t.Fatalf("preview wrote state: writes=%d available=%d err=%v", writes, available, err)
		}
	}
	partial := proposed(request(t, h, "POST", path, in, parent), first.ID, second.ID)
	assertNoWrites()

	seedBookingCredit(t, db, "competing-package", "ben", "other-parent", 1, nil)
	expectStatus(t, request(t, h, "POST", "/api/v1/students/ben/bookings", bookingInput{
		ClassID: second.ID, ClassVersion: second.Version, ClassTypeVersion: second.ClassTypeVersion,
		CreditPackageID: "competing-package", IdempotencyKey: "competing-booking", OverrideReason: "Class placement approved",
	}, f.admin), 201)
	in.PartialConfirmation = &partial
	fresh := proposed(request(t, h, "POST", path, in, parent), first.ID, third.ID)
	assertNoWrites()

	updated = request(t, h, "PATCH", settingsPath, settings, f.admin)
	expectStatus(t, updated, 200)
	settings = decodeBody[FixedEnrolmentSettings](t, updated)
	in.PartialConfirmation = &fresh
	expectBookingError(t, request(t, h, "POST", path, in, parent), 409, "fixed_enrolment_settings_changed")
	assertNoWrites()
	in.PartialConfirmation = nil
	fresh = proposed(request(t, h, "POST", path, in, parent), first.ID, third.ID)
	in.PartialConfirmation = &fresh
	created := request(t, h, "POST", path, in, parent)
	expectStatus(t, created, 201)
	enrolment := decodeBody[Enrolment](t, created)
	if len(enrolment.Bookings) != 2 || enrolment.Bookings[0].Class.ID != first.ID || enrolment.Bookings[1].Class.ID != third.ID {
		t.Fatalf("unexpected booked subset: %+v", enrolment.Bookings)
	}
	slices.Reverse(fresh.SessionIDs)
	replay := request(t, h, "POST", path, in, parent)
	expectStatus(t, replay, 200)
	if created.Body.String() != replay.Body.String() {
		t.Fatalf("retry changed enrolment: %s / %s", created.Body, replay.Body)
	}
	var changes int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='fixed_enrolment.settings.updated'
		AND actor_id='admin' AND details->'before'->>'version' IN ('1','2')`).Scan(&changes); err != nil || changes != 2 {
		t.Fatalf("expected two audited changes: got %d err=%v", changes, err)
	}
}
