package app

import (
	"cmp"
	"context"
	"net/http/httptest"
	"slices"
	"testing"
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
