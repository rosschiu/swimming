package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIntegrationCreditRecoveryLineageScopeAndReplay(t *testing.T) {
	f := prepareRecovery(t)
	f.now = f.now.Add(24 * time.Hour).Truncate(time.Microsecond).Add(789 * time.Nanosecond).In(hongKong)
	ctx := context.Background()
	path := "/api/v1/students/amy/credit-recoveries"
	input := map[string]any{"sourcePackageId": f.packageID, "quantity": 4, "reason": "Illness exception", "expiresOn": f.now.Add(24 * time.Hour).In(hongKong).Format("2006-01-02"), "idempotencyKey": "recover"}
	receiptPath := "/api/v1/receipts/" + *f.purchase.ReceiptID
	before := request(t, f.h, "GET", receiptPath, nil, f.parent)
	expectStatus(t, before, 200)
	student := login(t, testHandler(f.db), "student@swim.test")
	other := login(t, testHandler(f.db), "other-parent@swim.test")
	if _, err := f.db.Exec(ctx, "UPDATE sessions SET expires_at=expires_at+interval '10 days'"); err != nil {
		t.Fatal(err)
	}
	for _, user := range []authState{f.parent, student} {
		expectStatus(t, request(t, f.h, "POST", path, input, user), 403)
	}
	expectStatus(t, request(t, f.h, "GET", "/api/v1/students/amy/credit-history", nil, other), 404)
	expectStatus(t, request(t, f.h, "GET", "/api/v1/students/ben/credit-history", nil, f.coach), 404)
	w := request(t, f.h, "POST", path, input, f.coach)
	expectStatus(t, w, 201)
	createdBody := w.Body.String()
	r := decodeBody[CreditRecovery](t, w)
	if r.Quantity != 4 || r.SourcePackageID != f.packageID || r.ActorID != "coach" || r.DestinationPackageID == r.SourcePackageID {
		t.Fatal(r)
	}
	w = request(t, f.h, "POST", path, input, f.coach)
	expectStatus(t, w, 200)
	if w.Body.String() != createdBody {
		t.Fatalf("replay response differs from creation: got %s, want %s", w.Body, createdBody)
	}
	input["quantity"] = 5
	expectStatus(t, request(t, f.h, "POST", path, input, f.coach), 409)
	input["quantity"] = 4
	w = request(t, f.h, "GET", "/api/v1/students/amy/credit-history", nil, f.coach)
	expectStatus(t, w, 200)
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"purchaseId", "priceMinor", "receiptId", "currency", "policy"} {
		if strings.Contains(w.Body.String(), `"`+forbidden+`"`) {
			t.Fatal("coach finance leak", forbidden, w.Body)
		}
	}
	var sourceExpired, destinationAvailable, count, receipts int
	err := f.db.QueryRow(ctx, "SELECT (SELECT sum(expired) FROM credit_movements WHERE package_id=$1),(SELECT sum(available) FROM credit_movements WHERE package_id=$2),(SELECT count(*) FROM credit_recoveries),(SELECT count(*) FROM receipts)", f.packageID, r.DestinationPackageID).Scan(&sourceExpired, &destinationAvailable, &count, &receipts)
	if err != nil || sourceExpired != 6 || destinationAvailable != 4 || count != 1 || receipts != 1 {
		t.Fatal(sourceExpired, destinationAvailable, count, receipts, err)
	}
	after := request(t, f.h, "GET", receiptPath, nil, f.parent)
	expectStatus(t, after, 200)
	if before.Body.String() != after.Body.String() {
		t.Fatal("recovery changed receipt")
	}
	var origin, source string
	if err = f.db.QueryRow(ctx, "SELECT purchase_id,source_package_id FROM credit_packages WHERE id=$1", r.DestinationPackageID).Scan(&origin, &source); err != nil || origin != f.purchase.ID || source != f.packageID {
		t.Fatal(origin, source, err)
	}
	input["idempotencyKey"] = "too-many"
	input["quantity"] = 7
	expectStatus(t, request(t, f.h, "POST", path, input, f.coach), 409)
	input["sourcePackageId"] = r.DestinationPackageID
	input["quantity"] = 1
	input["idempotencyKey"] = "not-expired"
	expectStatus(t, request(t, f.h, "POST", path, input, f.coach), 409)
	// Recovery can expire again; moving it again must preserve the original purchase lineage.
	f.now = f.now.Add(48 * time.Hour)
	input["expiresOn"] = f.now.Add(24 * time.Hour).In(hongKong).Format("2006-01-02")
	input["idempotencyKey"] = "second-generation"
	w = request(t, f.h, "POST", path, input, f.coach)
	expectStatus(t, w, 201)
	next := decodeBody[CreditRecovery](t, w)
	if err = f.db.QueryRow(ctx, "SELECT purchase_id,source_package_id FROM credit_packages WHERE id=$1", next.DestinationPackageID).Scan(&origin, &source); err != nil || origin != f.purchase.ID || source != r.DestinationPackageID {
		t.Fatal(origin, source, err)
	}
	if _, err = f.db.Exec(ctx, "DELETE FROM student_access WHERE student_id='amy' AND user_id='coach'"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, f.h, "POST", path, input, f.coach), 404)
	expectStatus(t, request(t, f.h, "GET", "/api/v1/students/amy/credit-history", nil, f.coach), 404)
}

func TestIntegrationCreditRecoveryConcurrentLimitAndRollback(t *testing.T) {
	f := prepareRecovery(t)
	f.now = f.now.Add(24 * time.Hour)
	ctx := context.Background()
	path := "/api/v1/students/amy/credit-recoveries"
	input := map[string]any{"sourcePackageId": f.packageID, "quantity": 6, "reason": "Recovery test", "expiresOn": f.now.Add(24 * time.Hour).In(hongKong).Format("2006-01-02"), "idempotencyKey": "first"}
	// Failing the destination grant must roll back the recovery and any on-demand expiry together.
	if _, err := f.db.Exec(ctx, "ALTER TABLE credit_packages ADD CONSTRAINT reject_recovery CHECK(source_package_id IS NULL) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, f.h, "POST", path, input, f.coach), 500)
	var packages, recoveries, movements int
	if err := f.db.QueryRow(ctx, "SELECT (SELECT count(*) FROM credit_packages),(SELECT count(*) FROM credit_recoveries),(SELECT count(*) FROM credit_movements)").Scan(&packages, &recoveries, &movements); err != nil || packages != 1 || recoveries != 0 || movements != 1 {
		t.Fatal("partial recovery commit", packages, recoveries, movements, err)
	}
	if _, err := f.db.Exec(ctx, "ALTER TABLE credit_packages DROP CONSTRAINT reject_recovery"); err != nil {
		t.Fatal(err)
	}
	codes := make(chan int, 2)
	for _, key := range []string{"first", "second"} {
		go func(key string) {
			body := map[string]any{}
			for k, v := range input {
				body[k] = v
			}
			body["idempotencyKey"] = key
			codes <- concurrentRequest(f.h, path, body, f.coach)
		}(key)
	}
	a, b := <-codes, <-codes
	if !((a == 201 && b == 409) || (a == 409 && b == 201)) {
		t.Fatal(a, b)
	}
	var expired int
	if err := f.db.QueryRow(ctx, "SELECT sum(expired) FROM credit_movements WHERE package_id=$1", f.packageID).Scan(&expired); err != nil || expired != 4 {
		t.Fatal(expired, err)
	}
	input["quantity"] = 2
	input["idempotencyKey"] = "same"
	for range 2 {
		go func() { codes <- concurrentRequest(f.h, path, input, f.coach) }()
	}
	a, b = <-codes, <-codes
	if !((a == 201 && b == 200) || (a == 200 && b == 201)) {
		t.Fatal(a, b)
	}
	for _, sql := range []string{"UPDATE credit_recoveries SET quantity=1", "DELETE FROM credit_recoveries"} {
		if _, err := f.db.Exec(ctx, sql); err == nil {
			t.Fatal("mutable recovery history", sql)
		}
	}
}

func TestIntegrationCreditRecoveryValidation(t *testing.T) {
	f := prepareRecovery(t)
	f.now = f.now.Add(24 * time.Hour)
	path := "/api/v1/students/amy/credit-recoveries"
	for _, change := range []map[string]any{{"quantity": 0}, {"quantity": -1}, {"quantity": 10001}, {"reason": " "}, {"reason": strings.Repeat("x", 501)}, {"reason": "bad\x00reason"}, {"expiresOn": "2020-01-01"}, {"expiresOn": "2035-02-30"}, {"idempotencyKey": "bad\x00key"}} {
		input := map[string]any{"sourcePackageId": f.packageID, "quantity": 1, "reason": "Recovery", "expiresOn": f.now.Add(24 * time.Hour).In(hongKong).Format("2006-01-02"), "idempotencyKey": "validation"}
		for k, v := range change {
			input[k] = v
		}
		expectStatus(t, request(t, f.h, "POST", path, input, f.coach), 422)
	}
}

func TestIntegrationCreditRecoveryDecodesBeforeIdentityLock(t *testing.T) {
	f := prepareRecovery(t)
	tx, err := f.api.beginIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := httptest.NewRequestWithContext(ctx, "POST", "/api/v1/students/amy/credit-recoveries", strings.NewReader(`{"quantity":`))
	r.AddCookie(f.coach.cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("X-CSRF-Token", f.coach.csrf)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	expectStatus(t, w, 400)
}

func TestIntegrationCreditRecoveryAuditFailureRollsBackPairedMovements(t *testing.T) {
	f := prepareRecovery(t)
	f.now = f.now.Add(24 * time.Hour)
	ctx := context.Background()
	if err := f.api.processCreditExpiryBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, "ALTER TABLE audit_events ADD CONSTRAINT reject_recovery_audit CHECK(action<>'credits.recovered') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"sourcePackageId": f.packageID, "quantity": 4, "reason": "Recovery audit rollback", "expiresOn": f.now.Add(24 * time.Hour).In(hongKong).Format("2006-01-02"), "idempotencyKey": "audit-rollback"}
	path := "/api/v1/students/amy/credit-recoveries"
	expectStatus(t, request(t, f.h, "POST", path, input, f.coach), 500)
	var packages, recoveries, movements, expired int
	err := f.db.QueryRow(ctx, "SELECT (SELECT count(*) FROM credit_packages),(SELECT count(*) FROM credit_recoveries),(SELECT count(*) FROM credit_movements),(SELECT sum(expired) FROM credit_movements WHERE package_id=$1)", f.packageID).Scan(&packages, &recoveries, &movements, &expired)
	if err != nil || packages != 1 || recoveries != 0 || movements != 2 || expired != 10 {
		t.Fatal(packages, recoveries, movements, expired, err)
	}
	if _, err = f.db.Exec(ctx, "ALTER TABLE audit_events DROP CONSTRAINT reject_recovery_audit"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, f.h, "POST", path, input, f.coach), 201)
}
