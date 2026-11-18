package app

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func financeOffer(t *testing.T, h http.Handler, admin authState, policy *CreditPolicy) PackageOffer {
	t.Helper()
	w := request(t, h, "POST", "/api/v1/package-offers", map[string]any{"name": "Ten lessons", "credits": 10, "priceMinor": 123400, "currency": "HKD", "active": true, "policyOverride": policy}, admin)
	expectStatus(t, w, 201)
	return decodeBody[PackageOffer](t, w)
}
func financePurchase(t *testing.T, h http.Handler, parent authState, o PackageOffer, key string) Purchase {
	t.Helper()
	w := request(t, h, "POST", "/api/v1/students/amy/purchases", map[string]any{"offerId": o.ID, "offerVersion": o.Version, "idempotencyKey": key}, parent)
	expectStatus(t, w, 201)
	return decodeBody[Purchase](t, w)
}
func financeAttempt(t *testing.T, h http.Handler, parent authState, p Purchase, key string) PaymentAttempt {
	t.Helper()
	w := request(t, h, "POST", "/api/v1/purchases/"+p.ID+"/payment-attempts", map[string]string{"idempotencyKey": key}, parent)
	expectStatus(t, w, 201)
	return decodeBody[PaymentAttempt](t, w)
}
func TestIntegrationFinanceLifecycleSnapshotsAndScope(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	admin := login(t, h, "admin@swim.test")
	parent := login(t, h, "parent@swim.test")
	offer := financeOffer(t, h, admin, nil)
	p := financePurchase(t, h, parent, offer, "order")
	if p.Status != "awaiting_expiry" || p.ExpiresOn != nil || p.ReceiptID != nil || !p.Policy.ExpiryEnabled || !p.Policy.AbsenceRetainsCredit {
		t.Fatal(p)
	}
	purchasePath := "/api/v1/purchases/" + p.ID
	attemptPath := purchasePath + "/payment-attempts"
	input := map[string]any{"offerId": offer.ID, "offerVersion": offer.Version, "idempotencyKey": "order"}
	w := request(t, h, "POST", "/api/v1/students/amy/purchases", input, parent)
	expectStatus(t, w, 200)
	if decodeBody[Purchase](t, w).ID != p.ID {
		t.Fatal("order replay duplicated")
	}
	input["offerVersion"] = 2
	expectStatus(t, request(t, h, "POST", "/api/v1/students/amy/purchases", input, parent), 409)
	expectStatus(t, request(t, h, "POST", attemptPath, map[string]string{"idempotencyKey": "attempt"}, parent), 409)
	expectStatus(t, request(t, h, "POST", purchasePath+"/expiry", map[string]any{"expiresOn": "2035-12-31", "version": 1}, parent), 403)
	for _, date := range []string{"2020-01-01", "2035-02-30", "2035-1-1"} {
		expectStatus(t, request(t, h, "POST", purchasePath+"/expiry", map[string]any{"expiresOn": date, "version": 1}, admin), 422)
	}
	expectStatus(t, request(t, h, "POST", purchasePath+"/expiry", map[string]any{"expiresOn": "2035-12-31", "version": 2}, admin), 409)
	w = request(t, h, "POST", purchasePath+"/expiry", map[string]any{"expiresOn": "2035-12-31", "version": 1}, admin)
	expectStatus(t, w, 200)
	p = decodeBody[Purchase](t, w)
	if p.Status != "pending_payment" || p.ExpiryAssignedBy == nil || *p.ExpiryAssignedBy != "admin" || p.ExpiryAssignedAt == nil {
		t.Fatal(p)
	}
	expectStatus(t, request(t, h, "POST", purchasePath+"/expiry", map[string]any{"expiresOn": "2036-12-31", "version": p.Version}, admin), 409)
	w = request(t, h, "GET", "/api/v1/settings/credits", nil, admin)
	expectStatus(t, w, 200)
	settings := decodeBody[CreditSettings](t, w)
	settings.Policy.ExpiryEnabled = false
	settings.Policy.AbsenceRetainsCredit = false
	expectStatus(t, request(t, h, "PATCH", "/api/v1/settings/credits", settings, admin), 200)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/package-offers/"+offer.ID, map[string]any{"name": "Archived changed offer", "credits": 20, "priceMinor": 99, "currency": "HKD", "active": false, "version": offer.Version}, admin), 200)
	w = request(t, h, "GET", purchasePath, nil, parent)
	expectStatus(t, w, 200)
	snapshot := decodeBody[Purchase](t, w)
	if snapshot.Credits != 10 || snapshot.PriceMinor != 123400 || snapshot.OfferName != "Ten lessons" || !snapshot.Policy.AbsenceRetainsCredit || !snapshot.Policy.ExpiryEnabled {
		t.Fatal("terms changed", snapshot)
	}
	input["offerVersion"] = 1
	expectStatus(t, request(t, h, "POST", "/api/v1/students/amy/purchases", input, parent), 200)
	input["idempotencyKey"] = "new-archived"
	expectStatus(t, request(t, h, "POST", "/api/v1/students/amy/purchases", input, parent), 409)
	at := financeAttempt(t, h, parent, p, "attempt")
	outcomePath := "/api/v1/payment-attempts/" + at.ID + "/mock-outcome"
	expectStatus(t, request(t, h, "POST", attemptPath, map[string]string{"idempotencyKey": "second-pending"}, parent), 409)
	for _, outcome := range []string{"pending", "failed", "failed"} {
		expectStatus(t, request(t, h, "POST", outcomePath, map[string]string{"outcome": outcome}, parent), 200)
		w = request(t, h, "GET", "/api/v1/students/amy/credits", nil, parent)
		expectStatus(t, w, 200)
		if len(decodeBody[struct {
			Items []CreditPackage `json:"items"`
		}](t, w).Items) != 0 {
			t.Fatal("unpaid credits")
		}
	}
	expectStatus(t, request(t, h, "POST", outcomePath, map[string]string{"outcome": "succeeded"}, parent), 409)
	w = request(t, h, "POST", attemptPath, map[string]string{"idempotencyKey": "attempt"}, parent)
	expectStatus(t, w, 200)
	if replay := decodeBody[PaymentAttempt](t, w); replay.ID != at.ID || replay.Status != "failed" {
		t.Fatal("failed replay changed", replay)
	}
	at = financeAttempt(t, h, parent, p, "retry")
	outcomePath = "/api/v1/payment-attempts/" + at.ID + "/mock-outcome"
	w = request(t, h, "POST", outcomePath, map[string]string{"outcome": "succeeded"}, parent)
	expectStatus(t, w, 200)
	paid := decodeBody[Purchase](t, w)
	if paid.Status != "paid" || paid.ReceiptID == nil || len(paid.Attempts) != 2 {
		t.Fatal(paid)
	}
	expectStatus(t, request(t, h, "POST", outcomePath, map[string]string{"outcome": "succeeded"}, parent), 200)
	expectStatus(t, request(t, h, "POST", outcomePath, map[string]string{"outcome": "failed"}, parent), 409)
	expectStatus(t, request(t, h, "POST", attemptPath, map[string]string{"idempotencyKey": "third"}, parent), 409)
	receiptPath := "/api/v1/receipts/" + *paid.ReceiptID
	w = request(t, h, "GET", receiptPath, nil, parent)
	expectStatus(t, w, 200)
	receipt := decodeBody[Receipt](t, w)
	if !receipt.Simulated || receipt.PriceMinor != 123400 || receipt.Credits != 10 || receipt.StudentName != "Amy" {
		t.Fatal(receipt)
	}
	w = request(t, h, "GET", "/api/v1/students/amy/credits", nil, parent)
	expectStatus(t, w, 200)
	credits := decodeBody[struct {
		Items []CreditPackage `json:"items"`
	}](t, w).Items
	if len(credits) != 1 || credits[0].Available != 10 || len(credits[0].Movements) != 1 || credits[0].Reserved != 0 || credits[0].Consumed != 0 {
		t.Fatal(credits)
	}
	for _, email := range []string{"coach@swim.test", "student@swim.test", "other-parent@swim.test"} {
		user := login(t, h, email)
		code := 403
		if email == "other-parent@swim.test" {
			code = 404
		}
		for _, path := range []string{purchasePath, receiptPath, "/api/v1/students/amy/purchases", "/api/v1/students/amy/credits"} {
			expectStatus(t, request(t, h, "GET", path, nil, user), code)
		}
		expectStatus(t, request(t, h, "POST", outcomePath, map[string]string{"outcome": "succeeded"}, user), code)
	}
	var packages, movements, receipts, audits int
	err := db.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM credit_packages),(SELECT count(*) FROM credit_movements),(SELECT count(*) FROM receipts),(SELECT count(*) FROM audit_events WHERE action='payment_attempt.succeeded')").Scan(&packages, &movements, &receipts, &audits)
	if err != nil || packages != 1 || movements != 1 || receipts != 1 || audits != 1 {
		t.Fatal(packages, movements, receipts, audits, err)
	}
}

func TestIntegrationFinanceExpiryBoundaryAndProjection(t *testing.T) {
	db := testDB(t)
	base := testHandler(db)
	admin := login(t, base, "admin@swim.test")
	parent := login(t, base, "parent@swim.test")
	now := time.Now()
	date := now.In(hongKong).Format("2006-01-02")
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return now }}
	h := http.NewServeMux()
	a.financeRoutes(h)
	offer := financeOffer(t, h, admin, nil)
	paid := financePurchase(t, h, parent, offer, "paid")
	late := financePurchase(t, h, parent, offer, "late")
	for _, p := range []Purchase{paid, late} {
		expectStatus(t, request(t, h, "POST", "/api/v1/purchases/"+p.ID+"/expiry", map[string]any{"expiresOn": date, "version": 1}, admin), 200)
	}
	at := financeAttempt(t, h, parent, paid, "paid-attempt")
	lateAt := financeAttempt(t, h, parent, late, "late-attempt")
	end, _ := time.ParseInLocation("2006-01-02", date, hongKong)
	now = end.Add(24*time.Hour - time.Nanosecond)
	// Extend fixture sessions so authentication does not obscure the date boundary.
	if _, err := db.Exec(context.Background(), "UPDATE sessions SET expires_at=expires_at+interval '2 days'"); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/payment-attempts/" + at.ID + "/mock-outcome"
	expectStatus(t, request(t, h, "POST", path, map[string]string{"outcome": "succeeded"}, parent), 200)
	now = now.Add(time.Nanosecond)
	expectStatus(t, request(t, h, "POST", path, map[string]string{"outcome": "succeeded"}, parent), 200)
	expectStatus(t, request(t, h, "POST", "/api/v1/payment-attempts/"+lateAt.ID+"/mock-outcome", map[string]string{"outcome": "succeeded"}, parent), 409)
	w := request(t, h, "GET", "/api/v1/students/amy/credits", nil, parent)
	expectStatus(t, w, 200)
	credits := decodeBody[struct {
		Items []CreditPackage `json:"items"`
	}](t, w).Items
	if len(credits) != 1 || credits[0].Available != 0 || credits[0].Expired != 10 || credits[0].Movements[0].Available != 10 {
		t.Fatal(credits)
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/payment-attempts/"+lateAt.ID+"/mock-outcome", map[string]string{"outcome": "failed"}, parent), 200)
	expectStatus(t, request(t, h, "POST", "/api/v1/purchases/"+late.ID+"/payment-attempts", map[string]string{"idempotencyKey": "too-late"}, parent), 409)
	policy := CreditPolicy{CarryForwardEnabled: true, EarlyCancellationRetainsCredit: true, LateCancellationRetainsCredit: true, AbsenceRetainsCredit: true, IllnessRetainsCredit: true}
	noExpiry := financeOffer(t, h, admin, &policy)
	p := financePurchase(t, h, parent, noExpiry, "no-expiry")
	if p.Status != "pending_payment" || p.ExpiresOn != nil || p.Policy.ExpiryEnabled {
		t.Fatal(p)
	}
	at = financeAttempt(t, h, parent, p, "no-expiry-attempt")
	expectStatus(t, request(t, h, "POST", "/api/v1/payment-attempts/"+at.ID+"/mock-outcome", map[string]string{"outcome": "succeeded"}, parent), 200)
}

func TestIntegrationFinanceTrustBoundaryAndKeyScope(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	admin := login(t, h, "admin@swim.test")
	parent := login(t, h, "parent@swim.test")
	for _, body := range []any{map[string]any{"version": 1}, map[string]any{"version": 1, "policy": map[string]any{"expiryEnabled": true}}} {
		w := request(t, h, "PATCH", "/api/v1/settings/credits", body, admin)
		if w.Code != 400 && w.Code != 422 {
			t.Fatalf("incomplete policy accepted: %d %s", w.Code, w.Body)
		}
	}
	for _, change := range []map[string]any{{"credits": 0}, {"priceMinor": -1}, {"currency": "USD"}, {"policyOverride": map[string]any{"expiryEnabled": true}}} {
		body := map[string]any{"name": "Offer", "credits": 10, "priceMinor": 100, "currency": "HKD", "active": true}
		for k, v := range change {
			body[k] = v
		}
		w := request(t, h, "POST", "/api/v1/package-offers", body, admin)
		if w.Code != 400 && w.Code != 422 {
			t.Fatalf("invalid offer accepted: %d %s", w.Code, w.Body)
		}
	}
	policy := CreditPolicy{}
	offer := financeOffer(t, h, admin, &policy)
	expectStatus(t, request(t, h, "POST", "/api/v1/students/amy/purchases", map[string]any{"offerId": offer.ID, "offerVersion": 1, "idempotencyKey": "tampered", "priceMinor": 1}, parent), 400)
	expectStatus(t, request(t, h, "POST", "/api/v1/students/amy/purchases", map[string]any{"offerId": offer.ID, "offerVersion": 1, "idempotencyKey": "bad\x00key"}, parent), 422)
	p := financePurchase(t, h, parent, offer, "first")
	at := financeAttempt(t, h, parent, p, "shared-attempt-key")
	other := financePurchase(t, h, parent, offer, "second")
	expectStatus(t, request(t, h, "POST", "/api/v1/purchases/"+other.ID+"/payment-attempts", map[string]string{"idempotencyKey": "shared-attempt-key"}, parent), 409)
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/ben/relationships/parent", map[string]string{"kind": "guardian"}, admin), 200)
	expectStatus(t, request(t, h, "POST", "/api/v1/students/ben/purchases", map[string]any{"offerId": offer.ID, "offerVersion": 1, "idempotencyKey": "first"}, parent), 409)
	otherParent := login(t, h, "other-parent@swim.test")
	expectStatus(t, request(t, h, "POST", "/api/v1/students/ben/purchases", map[string]any{"offerId": offer.ID, "offerVersion": 1, "idempotencyKey": "first"}, otherParent), 201)
	expectStatus(t, request(t, h, "POST", "/api/v1/payment-attempts/"+at.ID+"/mock-outcome", map[string]string{"outcome": "succeeded"}, parent), 200)
	for _, sql := range []string{"UPDATE credit_movements SET available=999", "DELETE FROM credit_movements", "UPDATE receipts SET snapshot='{}'::jsonb", "DELETE FROM receipts"} {
		if _, err := db.Exec(context.Background(), sql); err == nil {
			t.Fatal("immutable history allowed", sql)
		}
	}
}
