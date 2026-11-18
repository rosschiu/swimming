package app

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegrationEnrolmentRenewal(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	h := testHandler(db)
	f := prepareScheduling(t, h)
	parent := login(t, h, "parent@swim.test")
	otherParent := login(t, h, "other-parent@swim.test")
	coach := login(t, h, "coach@swim.test")
	makeSeries := func(start, end string) ClassSeries {
		t.Helper()
		w := request(t, h, "POST", "/api/v1/class-series", SeriesInput{
			VenueID: f.venue.ID, ClassTypeID: f.kind.ID, CoachID: "coach",
			StartDate: start, EndDate: end, Weekday: 1, StartTime: "10:00", EndTime: "11:00", Capacity: 6,
		}, f.admin)
		expectStatus(t, w, 201)
		return decodeBody[ClassSeries](t, w)
	}
	first := makeSeries("2030-01-07", "2030-01-07")
	nextTerm := makeSeries("2030-01-21", "2030-01-28")
	thirdTerm := makeSeries("2030-02-04", "2030-02-04")
	offer := financeOffer(t, h, f.admin, nil)
	otherOffer := financeOffer(t, h, f.admin, &CreditPolicy{ExpiryEnabled: true})
	buy := func(offer PackageOffer, key, expiry string) (Purchase, string) {
		t.Helper()
		p := financePurchase(t, h, parent, offer, key)
		expectStatus(t, request(t, h, "POST", "/api/v1/purchases/"+p.ID+"/expiry", map[string]any{"expiresOn": expiry, "version": 1}, f.admin), 200)
		at := financeAttempt(t, h, parent, p, key+"-payment")
		w := request(t, h, "POST", "/api/v1/payment-attempts/"+at.ID+"/mock-outcome", map[string]string{"outcome": "succeeded"}, parent)
		expectStatus(t, w, 200)
		p = decodeBody[Purchase](t, w)
		var id string
		if err := db.QueryRow(ctx, "SELECT id FROM credit_packages WHERE purchase_id=$1", p.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return p, id
	}
	oldPurchase, source := buy(offer, "source", "2030-01-07")
	newPurchase, destination := buy(offer, "destination", "2030-01-28")
	_, nextDestination := buy(otherOffer, "different-offer", "2030-02-04")
	keys := []string{oldPurchase.ID, newPurchase.ID}
	readReceipts := func() string {
		t.Helper()
		var snapshots string
		if err := db.QueryRow(ctx, "SELECT jsonb_agg(snapshot ORDER BY purchase_id)::text FROM receipts WHERE purchase_id=ANY($1)", keys).Scan(&snapshots); err != nil {
			t.Fatal(err)
		}
		return snapshots
	}
	receipts := readReceipts()
	validityVersion := 1
	makeInput := func(series ClassSeries, target, key string) enrolmentInput {
		in := enrolmentInput{SeriesID: series.ID, CreditPackageID: target, IdempotencyKey: key, BookingValidityVersion: &validityVersion}
		for _, c := range series.Sessions {
			in.Sessions = append(in.Sessions, enrolmentSession{c.ID, c.Version, c.ClassTypeVersion})
		}
		return in
	}
	const enrolmentPath = "/api/v1/students/amy/enrolments"
	w := request(t, h, "POST", enrolmentPath, makeInput(first, source, "original"), parent)
	expectStatus(t, w, 201)
	original := decodeBody[Enrolment](t, w)
	w = request(t, h, "GET", enrolmentPath, nil, parent)
	expectStatus(t, w, 200)
	listing := decodeBody[struct {
		Items    []enrolmentSummary     `json:"items"`
		Settings FixedEnrolmentSettings `json:"settings"`
	}](t, w)
	if len(listing.Items) != 1 || listing.Items[0].CreditPackageID != source || listing.Items[0].OfferID != offer.ID || !listing.Items[0].LastLessonStartsAt.Equal(first.Sessions[0].StartsAt) {
		t.Fatalf("source enrolment list %+v", listing)
	}
	settings := listing.Settings
	path := enrolmentPath + "/" + original.ID + "/renewals"
	in := makeInput(nextTerm, destination, "renew")
	in.SettingsVersion, in.SourcePackageID = &settings.Version, source
	expectStatus(t, request(t, h, "POST", path, in, coach), 403)
	expectStatus(t, request(t, h, "POST", path, in, otherParent), 404)
	omittedValidity := in
	omittedValidity.BookingValidityVersion = nil
	expectBookingError(t, request(t, h, "POST", path, omittedValidity, parent), 422, "validation_error")
	staleValidity := in
	previousVersion := validityVersion
	staleValidity.BookingValidityVersion = &previousVersion
	w = request(t, h, "PATCH", "/api/v1/settings/booking-validity", BookingValiditySettings{bookingValidityLessonStart, validityVersion}, f.admin)
	expectStatus(t, w, 200)
	validityVersion = decodeBody[BookingValiditySettings](t, w).Version
	expectBookingError(t, request(t, h, "POST", path, staleValidity, parent), 409, "booking_validity_changed")
	forged := in
	if _, err := db.Exec(ctx, `INSERT INTO credit_packages(id,purchase_id,student_id,credits,expires_on,policy,created_at,source_package_id)
		SELECT 'sibling-recovery',purchase_id,student_id,1,expires_on,policy,created_at,id FROM credit_packages WHERE id=$1`, source); err != nil {
		t.Fatal(err)
	}
	forged.SourcePackageID = "sibling-recovery"
	expectBookingError(t, request(t, h, "POST", path, forged, parent), 422, "validation_error")
	forged.SourcePackageID, forged.CreditPackageID = source, "sibling-recovery"
	expectBookingError(t, request(t, h, "POST", path, forged, parent), 422, "renewal_package_ineligible")
	forged.CreditPackageID = nextDestination
	expectBookingError(t, request(t, h, "POST", path, forged, parent), 422, "renewal_package_ineligible")
	wrongVersion := in
	wrongVersion.Sessions = append([]enrolmentSession(nil), in.Sessions...)
	wrongVersion.Sessions[1].ClassVersion++
	expectBookingError(t, request(t, h, "POST", path, wrongVersion, parent), 409, "version_conflict")
	assertBalance := func(packageID string, wantAvailable, wantReserved, wantExpired int, wantExpiry string) {
		t.Helper()
		var available, reserved, expired int
		var expiry string
		if err := db.QueryRow(ctx, `SELECT sum(m.available),sum(m.reserved),sum(m.expired),p.expires_on::text
			FROM credit_movements m JOIN credit_packages p ON p.id=m.package_id WHERE p.id=$1 GROUP BY p.id`, packageID).
			Scan(&available, &reserved, &expired, &expiry); err != nil {
			t.Fatal(err)
		}
		if available != wantAvailable || reserved != wantReserved || expired != wantExpired || expiry != wantExpiry {
			t.Fatalf("%s balance got %d/%d/%d %s, want %d/%d/%d %s", packageID, available, reserved, expired, expiry, wantAvailable, wantReserved, wantExpired, wantExpiry)
		}
	}
	assertBalance(source, 9, 1, 0, "2030-01-07")
	assertBalance(destination, 10, 0, 0, "2030-01-28")
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { results <- request(t, h, "POST", path, in, parent) }()
	}
	created, replay := <-results, <-results
	if replay.Code == 201 {
		created, replay = replay, created
	}
	expectStatus(t, created, 201)
	expectStatus(t, replay, 200)
	if created.Body.String() != replay.Body.String() {
		t.Fatalf("replay %s differs from creation %s", replay.Body, created.Body)
	}
	renewed := decodeBody[Enrolment](t, created)
	if renewed.SourceEnrolmentID != original.ID || renewed.SeriesID != nextTerm.ID || renewed.CarriedForwardCredits == nil || *renewed.CarriedForwardCredits != 9 || len(renewed.Bookings) != 2 {
		t.Fatalf("renewal %+v", renewed)
	}
	assertBalance(source, 0, 1, 0, "2030-01-07")
	assertBalance(destination, 17, 2, 0, "2030-01-28")
	if got := readReceipts(); receipts != got {
		t.Fatalf("renewal changed receipt snapshots: %s", got)
	}
	var legs, audits int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM credit_movements WHERE source LIKE 'renewal:%'),
		(SELECT count(*) FROM audit_events WHERE action=$1)`, renewalCreditAction).Scan(&legs, &audits); err != nil || legs != 2 || audits != 1 {
		t.Fatalf("renewal ledger had %d movements, %d audit events, error %v", legs, audits, err)
	}
	open := settings
	open.RenewalPolicy = fixedRenewalAnyPaidPackage
	w = request(t, h, "PATCH", "/api/v1/settings/fixed-enrolment", open, f.admin)
	expectStatus(t, w, 200)
	open = decodeBody[FixedEnrolmentSettings](t, w)
	expectStatus(t, request(t, h, "POST", path, in, parent), 200)
	third := makeInput(thirdTerm, nextDestination, "third-term")
	third.SettingsVersion, third.SourcePackageID = &settings.Version, destination
	thirdPath := enrolmentPath + "/" + renewed.ID + "/renewals"
	expectBookingError(t, request(t, h, "POST", thirdPath, third, parent), 409, "fixed_enrolment_settings_changed")
	third.SettingsVersion = &open.Version
	w = request(t, h, "POST", thirdPath, third, parent)
	expectStatus(t, w, 201)
	latest := decodeBody[Enrolment](t, w)
	if latest.CarriedForwardCredits == nil || *latest.CarriedForwardCredits != 17 {
		t.Fatalf("cross-offer renewal %+v", latest)
	}
	assertBalance(destination, 0, 2, 0, "2030-01-28")
	assertBalance(nextDestination, 26, 1, 0, "2030-02-04")
	fourthTerm := makeSeries("2030-02-11", "2030-02-11")
	_, fourthDestination := buy(offer, "disabled-next", "2030-02-11")
	fourth := makeInput(fourthTerm, fourthDestination, "fourth")
	fourth.SettingsVersion, fourth.SourcePackageID = &open.Version, nextDestination
	w = request(t, h, "POST", enrolmentPath+"/"+latest.ID+"/renewals", fourth, parent)
	expectStatus(t, w, 201)
	disabled := decodeBody[Enrolment](t, w)
	if disabled.CarriedForwardCredits == nil || *disabled.CarriedForwardCredits != 0 {
		t.Fatalf("disabled carry-forward %+v", disabled)
	}
	assertBalance(nextDestination, 26, 1, 0, "2030-02-04")
	assertBalance(fourthDestination, 9, 1, 0, "2030-02-11")

	fifthTerm := makeSeries("2030-02-18", "2030-02-18")
	_, fifthDestination := buy(offer, "expired-source-next", "2030-02-18")
	if _, err := db.Exec(ctx, "UPDATE credit_packages SET expires_on='2020-01-01' WHERE id=$1", fourthDestination); err != nil {
		t.Fatal(err)
	}
	fifth := makeInput(fifthTerm, fifthDestination, "fifth")
	fifth.SettingsVersion, fifth.SourcePackageID = &open.Version, fourthDestination
	expiryResult := make(chan error, 1)
	go func() { expiryResult <- (&API{db: db, now: time.Now}).processCreditExpiryBatch(ctx) }()
	w = request(t, h, "POST", enrolmentPath+"/"+disabled.ID+"/renewals", fifth, parent)
	expectStatus(t, w, 201)
	if err := <-expiryResult; err != nil {
		t.Fatal(err)
	}
	expired := decodeBody[Enrolment](t, w)
	if expired.CarriedForwardCredits == nil || *expired.CarriedForwardCredits != 0 {
		t.Fatalf("renewal with expired balance %+v", expired)
	}
	assertBalance(fourthDestination, 0, 1, 9, "2020-01-01")
	assertBalance(fifthDestination, 9, 1, 0, "2030-02-18")
}
