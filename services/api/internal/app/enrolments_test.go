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
