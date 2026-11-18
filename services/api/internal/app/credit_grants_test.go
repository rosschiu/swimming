package app

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestIntegrationAdministratorCreditGrants(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	f := prepareScheduling(t, h)
	lesson := createBookingClass(t, f, "2030-01-07", "10:00", "11:00", 4)
	parent := login(t, h, "parent@swim.test")
	coach := login(t, h, "coach@swim.test")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE sessions SET expires_at='2031-01-01'")
	now := lesson.StartsAt.Add(-72 * time.Hour)
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return now }}
	routes := http.NewServeMux()
	a.bookingRoutes(routes)
	a.financeRoutes(routes)
	a.creditRecoveryRoutes(routes)
	a.creditGrantRoutes(routes)
	path := "/api/v1/students/amy/credit-grants"
	grant := func(credits int, expiry, key string) map[string]any {
		return map[string]any{"credits": credits, "expiresOn": expiry, "reason": "Loyalty", "idempotencyKey": key}
	}
	var purchasesBefore, receiptsBefore int
	if err := db.QueryRow(ctx, "SELECT (SELECT count(*) FROM purchases),(SELECT count(*) FROM receipts)").Scan(&purchasesBefore, &receiptsBefore); err != nil {
		t.Fatal(err)
	}

	expectBookingError(t, request(t, routes, "POST", path, grant(2, "2030-02-28", "parent"), parent), 403, "forbidden")
	expectBookingError(t, request(t, routes, "POST", path, grant(2, "2030-02-28", "coach"), coach), 403, "forbidden")
	for _, bad := range []map[string]any{grant(0, "2030-02-28", "zero"), grant(101, "2030-02-28", "many"), grant(2, "2030-01-01", "past")} {
		expectBookingError(t, request(t, routes, "POST", path, bad, f.admin), 422, "validation_error")
	}
	expectStatus(t, request(t, routes, "POST", "/api/v1/students/nobody/credit-grants", grant(2, "2030-02-28", "nobody"), f.admin), 404)

	w := request(t, routes, "POST", path, grant(2, "2030-02-28", "loyalty"), f.admin)
	expectStatus(t, w, 201)
	created := decodeBody[CreditGrant](t, w)
	if created.Credits != 2 || created.ExpiresOn != "2030-02-28" || created.PackageID == "" {
		t.Fatalf("unexpected grant: %+v", created)
	}
	expectStatus(t, request(t, routes, "POST", path, grant(2, "2030-02-28", "loyalty"), f.admin), 200)
	expectBookingError(t, request(t, routes, "POST", path, grant(3, "2030-02-28", "loyalty"), f.admin), 409, "conflict")
	var purchases, receipts int
	if err := db.QueryRow(ctx, "SELECT (SELECT count(*) FROM purchases),(SELECT count(*) FROM receipts)").Scan(&purchases, &receipts); err != nil || purchases != purchasesBefore || receipts != receiptsBefore {
		t.Fatalf("a grant must not create purchases or receipts: %d %d %v", purchases, receipts, err)
	}

	// Families see complimentary credits without purchase details.
	w = request(t, routes, "GET", "/api/v1/students/amy/credit-history", nil, parent)
	expectStatus(t, w, 200)
	history := decodeBody[struct{ Items []CreditHistoryPackage }](t, w)
	if len(history.Items) != 1 || !history.Items[0].Complimentary || history.Items[0].PurchaseID != "" || history.Items[0].Available != 2 {
		t.Fatalf("unexpected family credit history: %+v", history.Items)
	}

	// Granted credits book and are used like any other package.
	w = request(t, routes, "POST", "/api/v1/students/amy/bookings", bookingBody(lesson, created.PackageID, "granted-booking"), parent)
	expectStatus(t, w, 201)
	booking := decodeBody[StudentBooking](t, w)
	now = lesson.StartsAt.Add(20 * time.Minute)
	expectStatus(t, request(t, routes, "PUT", "/api/v1/classes/"+lesson.ID+"/roster/"+booking.ID+"/attendance", nil, coach), 200)

	// An assigned coach can recover granted credits that expired unused; the restored package stays complimentary.
	now = time.Date(2030, 3, 2, 9, 0, 0, 0, hongKong)
	expectStatus(t, request(t, routes, "GET", "/api/v1/students/amy/credits", nil, parent), 200)
	w = request(t, routes, "POST", "/api/v1/students/amy/credit-recoveries", map[string]any{"sourcePackageId": created.PackageID, "quantity": 1, "reason": "Missed while away", "expiresOn": "2030-04-30", "idempotencyKey": "recover-grant"}, coach)
	expectStatus(t, w, 201)
	var restoredGrant *string
	if err := db.QueryRow(ctx, "SELECT grant_id FROM credit_packages WHERE source_package_id=$1", created.PackageID).Scan(&restoredGrant); err != nil || restoredGrant == nil || *restoredGrant != created.ID {
		t.Fatalf("restored package lost its grant origin: %v %v", restoredGrant, err)
	}
	if _, err := db.Exec(ctx, "UPDATE credit_grants SET credits=5"); err == nil {
		t.Fatal("grant history must be immutable")
	}
}
