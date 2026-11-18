package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedBookingCredit(t *testing.T, db *pgxpool.Pool, id, student, actor string, quantity int, expiry *string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Exec(ctx, `
WITH purchase AS (
    INSERT INTO purchases(id,student_id,created_by,idempotency_key,request,terms,status,expires_on,created_at)
    VALUES ('purchase-'||$1,$2,$3,$1,'{}','{}','paid',$5::text::date,now())
    RETURNING id
), entitlement AS (
    INSERT INTO credit_packages(id,purchase_id,student_id,credits,policy,expires_on,created_at)
    SELECT $1,p.id,$2,$4,s.policy,$5::text::date,now() FROM purchase p CROSS JOIN credit_settings s
    RETURNING id
)
INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
SELECT 'grant-'||id,id,'grant-'||id,$4,0,0,0,$3,'Test purchase',now() FROM entitlement
`, id, student, actor, quantity, expiry); err != nil {
		t.Fatal(err)
	}
}

func createBookingClass(t *testing.T, f schedulingFixture, date, start, end string, capacity int) ClassSession {
	t.Helper()
	input := f.input(f.venue.ID, date, start, end)
	input.Capacity = capacity
	w := request(t, f.h, "POST", "/api/v1/classes", input, f.admin)
	expectStatus(t, w, 201)
	return decodeBody[ClassSession](t, w)
}

func bookingBody(class ClassSession, packageID, key string) bookingInput {
	return bookingInput{ClassID: class.ID, ClassVersion: class.Version, ClassTypeVersion: class.ClassTypeVersion, CreditPackageID: packageID, IdempotencyKey: key}
}

func expectBookingError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	expectStatus(t, w, status)
	result := decodeBody[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, w)
	if result.Error.Code != code {
		t.Fatalf("expected %s, got %s", code, w.Body.String())
	}
}

func TestBookingCreditHongKongExpiry(t *testing.T) {
	expiry := time.Date(2030, 1, 7, 0, 0, 0, 0, time.UTC)
	midnight := time.Date(2030, 1, 8, 0, 0, 0, 0, hongKong)
	now := midnight.Add(-time.Hour)
	if !bookingCreditCovers(bookingValidityLessonEnd, ClassSession{ClassInput: ClassInput{EndsAt: midnight}}, &expiry, now) {
		t.Fatal("expiry should cover a lesson ending at the next Hong Kong midnight")
	}
	if bookingCreditCovers(bookingValidityLessonEnd, ClassSession{ClassInput: ClassInput{EndsAt: midnight.Add(time.Second)}}, &expiry, now) {
		t.Fatal("expiry should not cover a lesson continuing past Hong Kong midnight")
	}
	if bookingCreditCovers(bookingValidityLessonEnd, ClassSession{ClassInput: ClassInput{EndsAt: midnight}}, &expiry, midnight) {
		t.Fatal("credits must already be in date when reserved")
	}
}

func TestIntegrationBookingCreateChecksAndReplay(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	first := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 5)
	second := createBookingClass(t, f, "2030-01-07", "11:00", "12:00", 5)
	coach2, err := db.Exec(ctx, "INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Second Coach','coach',password_hash FROM users WHERE id='coach'")
	if err != nil || coach2.RowsAffected() != 1 {
		t.Fatal(err)
	}
	overlap := f.input(f.venue.ID, "2030-01-07", "10:30", "11:15")
	overlap.CoachID = "coach2"
	w := request(t, h, "POST", "/api/v1/classes", overlap, f.admin)
	expectStatus(t, w, 201)
	overlapClass := decodeBody[ClassSession](t, w)
	expiry := "2030-01-06"
	seedBookingCredit(t, db, "amy-pack", "amy", "parent", 2, nil)
	seedBookingCredit(t, db, "ben-pack", "ben", "other-parent", 1, nil)
	seedBookingCredit(t, db, "expired-pack", "amy", "parent", 1, &expiry)
	parent := login(t, h, "parent@swim.test")
	student := login(t, h, "student@swim.test")
	otherParent := login(t, h, "other-parent@swim.test")
	amyURL := "/api/v1/students/amy/bookings"
	benURL := "/api/v1/students/ben/bookings"
	input := bookingBody(first, "amy-pack", "book-first")
	expectBookingError(t, request(t, h, "POST", amyURL, input, student), 403, "forbidden")
	expectBookingError(t, request(t, h, "POST", amyURL, input, otherParent), 404, "not_found")
	expectBookingError(t, request(t, h, "POST", amyURL, bookingBody(first, "ben-pack", "wrong-package"), parent), 404, "not_found")
	expectBookingError(t, request(t, h, "POST", amyURL, bookingBody(first, "expired-pack", "late"), parent), 409, "credit_unavailable")
	override := bookingBody(first, "ben-pack", "override-first")
	expectBookingError(t, request(t, h, "POST", benURL, override, otherParent), 409, "level_mismatch")
	override.OverrideReason = " Coach assessed readiness "
	expectBookingError(t, request(t, h, "POST", benURL, override, otherParent), 403, "forbidden")

	w = request(t, h, "POST", amyURL, input, parent)
	expectStatus(t, w, 201)
	firstBooking := decodeBody[StudentBooking](t, w)
	w = request(t, h, "POST", amyURL, input, parent)
	expectStatus(t, w, 200)
	if replay := decodeBody[StudentBooking](t, w); replay.ID != firstBooking.ID || !replay.CreatedAt.Equal(firstBooking.CreatedAt) {
		t.Fatalf("wrong replay: %+v", replay)
	}
	expectBookingError(t, request(t, h, "POST", amyURL, bookingBody(second, "amy-pack", input.IdempotencyKey), parent), 409, "conflict")
	expectBookingError(t, request(t, h, "POST", amyURL, bookingBody(overlapClass, "amy-pack", "overlap"), parent), 409, "student_overlap")
	expectStatus(t, request(t, h, "POST", benURL, override, f.admin), 201)

	// A class-type edit invalidates a stale selection even when the class version stays put.
	update := map[string]any{"name": "Renamed group", "level": "Beginner", "active": true, "version": 1}
	expectStatus(t, request(t, h, "PATCH", "/api/v1/class-types/"+f.kind.ID, update, f.admin), 200)
	expectBookingError(t, request(t, h, "POST", amyURL, bookingBody(second, "amy-pack", "stale"), parent), 409, "version_conflict")
	w = request(t, h, "GET", "/api/v1/classes/"+second.ID, nil, parent)
	expectStatus(t, w, 200)
	second = decodeBody[ClassSession](t, w)
	expectStatus(t, request(t, h, "POST", amyURL, bookingBody(second, "amy-pack", "adjacent"), parent), 201)
	expectStatus(t, request(t, h, "POST", amyURL, input, parent), 200)
	expectStatus(t, request(t, h, "DELETE", "/api/v1/students/amy/relationships/parent", nil, f.admin), 204)
	expectBookingError(t, request(t, h, "POST", amyURL, input, parent), 404, "not_found")

	var actor, reason, studentLevel, classLevel, rule string
	err = db.QueryRow(ctx, `SELECT override_actor_id,override_reason,student_level,class_level,validity_rule
		FROM bookings WHERE student_id='ben'`).Scan(&actor, &reason, &studentLevel, &classLevel, &rule)
	if err != nil || actor != "admin" || reason != "Coach assessed readiness" || studentLevel != "Intermediate" ||
		classLevel != "Beginner" || rule != bookingValidityLessonEnd {
		t.Fatalf("wrong override snapshot: %q %q %q %q %q, %v", actor, reason, studentLevel, classLevel, rule, err)
	}
	var bookings, reservations, audits int
	err = db.QueryRow(ctx, `SELECT (SELECT count(*) FROM bookings),
		(SELECT count(*) FROM credit_movements WHERE source LIKE 'booking:%:reserved' AND available=-1 AND reserved=1),
		(SELECT count(*) FROM audit_events WHERE action='booking.created')`).Scan(&bookings, &reservations, &audits)
	if err != nil || bookings != 3 || reservations != 3 || audits != 3 {
		t.Fatalf("expected three complete bookings: %d %d %d, %v", bookings, reservations, audits, err)
	}
}

func TestIntegrationBookingContentionAndRollback(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	first := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 1)
	second := createBookingClass(t, f, "2030-01-07", "11:00", "12:00", 3)
	third := createBookingClass(t, f, "2030-01-08", "11:00", "12:00", 3)
	if _, err := db.Exec(ctx, "UPDATE students SET level='Beginner' WHERE id='ben'"); err != nil {
		t.Fatal(err)
	}
	seedBookingCredit(t, db, "amy-pack", "amy", "parent", 1, nil)
	seedBookingCredit(t, db, "ben-pack", "ben", "other-parent", 1, nil)
	seedBookingCredit(t, db, "amy-last", "amy", "parent", 1, nil)
	seedBookingCredit(t, db, "amy-rollback", "amy", "parent", 1, nil)
	parent := login(t, h, "parent@swim.test")
	otherParent := login(t, h, "other-parent@swim.test")
	amyURL := "/api/v1/students/amy/bookings"
	benURL := "/api/v1/students/ben/bookings"
	race := func(ab func() *httptest.ResponseRecorder, cd func() *httptest.ResponseRecorder, code string) {
		t.Helper()
		goTime := make(chan struct{})
		result := make(chan *httptest.ResponseRecorder, 2)
		for _, attempt := range []func() *httptest.ResponseRecorder{ab, cd} {
			go func() {
				<-goTime
				result <- attempt()
			}()
		}
		close(goTime)
		a, b := <-result, <-result
		if b.Code == 201 {
			a, b = b, a
		}
		expectStatus(t, a, 201)
		expectBookingError(t, b, 409, code)
	}
	post := func(url string, in bookingInput, actor authState) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return request(t, h, "POST", url, in, actor) }
	}
	race(post(amyURL, bookingBody(first, "amy-pack", "place-amy"), parent),
		post(benURL, bookingBody(first, "ben-pack", "place-ben"), otherParent), "class_full")
	race(post(amyURL, bookingBody(second, "amy-last", "credit-two"), parent),
		post(amyURL, bookingBody(third, "amy-last", "credit-three"), parent), "credit_unavailable")
	fourth := createBookingClass(t, f, "2030-01-09", "11:00", "12:00", 3)
	if _, err := db.Exec(ctx, "ALTER TABLE audit_events ADD CONSTRAINT test_booking_audit_failure CHECK (action <> 'booking.created') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	rollbackInput := bookingBody(fourth, "amy-rollback", "rollback")
	expectStatus(t, request(t, h, "POST", amyURL, rollbackInput, parent), 500)
	var available, pending int
	err := db.QueryRow(ctx, `SELECT (SELECT sum(available) FROM credit_movements WHERE package_id='amy-rollback'),
		(SELECT count(*) FROM bookings WHERE idempotency_key='rollback')`).Scan(&available, &pending)
	if err != nil || available != 1 || pending != 0 {
		t.Fatalf("failed booking changed the package or class: %d %d %v", available, pending, err)
	}
	if _, err = db.Exec(ctx, "ALTER TABLE audit_events DROP CONSTRAINT test_booking_audit_failure"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "POST", amyURL, rollbackInput, parent), http.StatusCreated)

	identity, err := (&API{db: db}).beginIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer identity.Rollback(ctx)
	replay := make(chan *httptest.ResponseRecorder, 1)
	go func() { replay <- post(amyURL, rollbackInput, parent)() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		err = db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks
			WHERE locktype='advisory' AND objid=829417002 AND NOT granted
			  AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("booking replay did not wait for the identity lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = identity.Exec(ctx, "DELETE FROM student_access WHERE student_id='amy' AND user_id='parent'"); err != nil {
		t.Fatal(err)
	}
	if err = identity.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-replay:
		expectBookingError(t, result, 404, "not_found")
	case <-time.After(5 * time.Second):
		t.Fatal("booking replay did not finish")
	}
}

func TestIntegrationBookingReservesRecoveredPackageOnly(t *testing.T) {
	recovery := prepareRecovery(t)
	recovery.now = recovery.now.Add(24 * time.Hour)
	mux := recovery.h.(*http.ServeMux)
	recovery.api.schedulingRoutes(mux)
	recovery.api.bookingRoutes(mux)

	receiptURL := "/api/v1/receipts/" + *recovery.purchase.ReceiptID
	receiptBefore := request(t, mux, "GET", receiptURL, nil, recovery.parent)
	expectStatus(t, receiptBefore, 200)
	date := recovery.now.In(hongKong).AddDate(0, 0, 1).Format("2006-01-02")
	w := request(t, mux, "POST", "/api/v1/students/amy/credit-recoveries", map[string]any{
		"sourcePackageId": recovery.packageID, "quantity": 2, "reason": "Make-up exception",
		"expiresOn": date, "idempotencyKey": "recovery-for-booking",
	}, recovery.coach)
	expectStatus(t, w, 201)
	credit := decodeBody[CreditRecovery](t, w)
	scheduling := prepareScheduling(t, testHandler(recovery.db))
	class := createBookingClass(t, scheduling, date, "10:00", "11:00", 1)
	path := "/api/v1/students/amy/bookings"
	expectBookingError(t, request(t, mux, "POST", path, bookingBody(class, recovery.packageID, "old-expiry"), recovery.parent), 409, "credit_unavailable")

	input := bookingBody(class, credit.DestinationPackageID, "same-recovered-booking")
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			<-start
			results <- request(t, mux, "POST", path, input, recovery.parent)
		}()
	}
	close(start)
	created, replay := <-results, <-results
	if replay.Code == 201 {
		created, replay = replay, created
	}
	expectStatus(t, created, 201)
	expectStatus(t, replay, 200)
	if created.Body.String() != replay.Body.String() {
		t.Fatalf("concurrent replay returned a different booking: %s / %s", created.Body, replay.Body)
	}
	booking := decodeBody[StudentBooking](t, created)

	var packageID, movementPackageID, originalExpiry, purchaseExpiry string
	var bookings, reservations, originalAvailable, originalReserved, recoveredAvailable, recoveredReserved int
	err := recovery.db.QueryRow(context.Background(), `SELECT
		(SELECT credit_package_id FROM bookings WHERE id=$3),
		(SELECT package_id FROM credit_movements WHERE source='booking:'||$3||':reserved' AND available=-1 AND reserved=1),
		(SELECT expires_on::text FROM credit_packages WHERE id=$1),
		(SELECT expires_on::text FROM purchases WHERE id=$4),
		(SELECT count(*) FROM bookings),
		(SELECT count(*) FROM credit_movements WHERE source LIKE 'booking:%:reserved'),
		(SELECT sum(available) FROM credit_movements WHERE package_id=$1),
		(SELECT sum(reserved) FROM credit_movements WHERE package_id=$1),
		(SELECT sum(available) FROM credit_movements WHERE package_id=$2),
		(SELECT sum(reserved) FROM credit_movements WHERE package_id=$2)`,
		recovery.packageID, credit.DestinationPackageID, booking.ID, recovery.purchase.ID).Scan(
		&packageID, &movementPackageID, &originalExpiry, &purchaseExpiry, &bookings, &reservations,
		&originalAvailable, &originalReserved, &recoveredAvailable, &recoveredReserved)
	if err != nil {
		t.Fatal(err)
	}
	if packageID != credit.DestinationPackageID || movementPackageID != credit.DestinationPackageID ||
		bookings != 1 || reservations != 1 || originalAvailable != 0 || originalReserved != 0 ||
		recoveredAvailable != 1 || recoveredReserved != 1 {
		t.Fatalf("reservation did not debit only the recovered package once: package %s, movement %s, bookings %d, reservations %d, original %d/%d, recovered %d/%d",
			packageID, movementPackageID, bookings, reservations, originalAvailable, originalReserved, recoveredAvailable, recoveredReserved)
	}
	if originalExpiry != *recovery.purchase.ExpiresOn || purchaseExpiry != *recovery.purchase.ExpiresOn {
		t.Fatalf("original expiry changed: package %q, purchase %q", originalExpiry, purchaseExpiry)
	}
	receiptAfter := request(t, mux, "GET", receiptURL, nil, recovery.parent)
	expectStatus(t, receiptAfter, 200)
	if receiptBefore.Body.String() != receiptAfter.Body.String() {
		t.Fatal("recovery and booking changed the original receipt")
	}
}
