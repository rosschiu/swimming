package app

import (
	"testing"
	"time"
)

func TestIntegrationOfferDefaultValidityAtPayment(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	admin := login(t, h, "admin@swim.test")
	parent := login(t, h, "parent@swim.test")
	expiring := &CreditPolicy{ExpiryEnabled: true, CarryForwardEnabled: true, EarlyCancellationRetainsCredit: true, LateCancellationRetainsCredit: true, AbsenceRetainsCredit: true, IllnessRetainsCredit: true}
	offer := func(name string, policy *CreditPolicy, days any) PackageOffer {
		t.Helper()
		w := request(t, h, "POST", "/api/v1/package-offers", map[string]any{"name": name, "credits": 4, "priceMinor": 40000, "currency": "HKD", "active": true, "policyOverride": policy, "validityDays": days}, admin)
		expectStatus(t, w, 201)
		return decodeBody[PackageOffer](t, w)
	}
	for _, days := range []int{0, 3651} {
		w := request(t, h, "POST", "/api/v1/package-offers", map[string]any{"name": "Invalid", "credits": 4, "priceMinor": 40000, "currency": "HKD", "active": true, "validityDays": days}, admin)
		expectStatus(t, w, 422)
	}
	pay := func(p Purchase, key string) Purchase {
		t.Helper()
		attempt := financeAttempt(t, h, parent, p, key)
		w := request(t, h, "POST", "/api/v1/payment-attempts/"+attempt.ID+"/mock-outcome", map[string]string{"outcome": "succeeded"}, parent)
		expectStatus(t, w, 200)
		return decodeBody[Purchase](t, w)
	}
	packageExpiry := func(purchaseID string) *string {
		t.Helper()
		w := request(t, h, "GET", "/api/v1/students/amy/credits", nil, parent)
		expectStatus(t, w, 200)
		for _, item := range decodeBody[struct{ Items []CreditPackage }](t, w).Items {
			if item.PurchaseID == purchaseID {
				return item.ExpiresOn
			}
		}
		t.Fatal("credit package missing for", purchaseID)
		return nil
	}

	monthly := offer("Thirty days", expiring, 30)
	if monthly.ValidityDays == nil || *monthly.ValidityDays != 30 {
		t.Fatalf("validity not saved: %+v", monthly)
	}
	purchase := financePurchase(t, h, parent, monthly, "default-validity")
	if purchase.Status != "pending_payment" || purchase.ExpiresOn != nil || purchase.ValidityDays == nil || *purchase.ValidityDays != 30 {
		t.Fatalf("default validity purchase should be payable without expiry: %+v", purchase)
	}
	before := time.Now().In(hongKong).AddDate(0, 0, 30).Format("2006-01-02")
	paid := pay(purchase, "default-validity-pay")
	after := time.Now().In(hongKong).AddDate(0, 0, 30).Format("2006-01-02")
	if paid.Status != "paid" || paid.ExpiresOn == nil || (*paid.ExpiresOn != before && *paid.ExpiresOn != after) {
		t.Fatalf("expiry not set from payment date: %+v", paid)
	}
	if got := packageExpiry(paid.ID); got == nil || *got != *paid.ExpiresOn {
		t.Fatalf("credit package expiry %v differs from purchase %s", got, *paid.ExpiresOn)
	}
	w := request(t, h, "GET", "/api/v1/receipts/"+*paid.ReceiptID, nil, parent)
	expectStatus(t, w, 200)
	if receipt := decodeBody[Receipt](t, w); receipt.ExpiresOn == nil || *receipt.ExpiresOn != *paid.ExpiresOn || receipt.ValidityDays == nil {
		t.Fatalf("receipt does not record the applied expiry: %+v", receipt)
	}

	// An administrator can still enter an individual expiry before payment starts, and it takes precedence.
	override := financePurchase(t, h, parent, monthly, "admin-override")
	w = request(t, h, "POST", "/api/v1/purchases/"+override.ID+"/expiry", map[string]any{"expiresOn": "2099-12-31", "version": override.Version}, admin)
	expectStatus(t, w, 200)
	override = decodeBody[Purchase](t, w)
	if override.Status != "pending_payment" || override.ExpiresOn == nil || *override.ExpiresOn != "2099-12-31" {
		t.Fatalf("override not applied: %+v", override)
	}
	if paid = pay(override, "admin-override-pay"); *paid.ExpiresOn != "2099-12-31" || *packageExpiry(paid.ID) != "2099-12-31" {
		t.Fatalf("payment replaced the administrator expiry: %+v", paid)
	}

	started := financePurchase(t, h, parent, monthly, "override-too-late")
	financeAttempt(t, h, parent, started, "override-too-late-pay")
	w = request(t, h, "GET", "/api/v1/purchases/"+started.ID, nil, admin)
	expectStatus(t, w, 200)
	started = decodeBody[Purchase](t, w)
	expectStatus(t, request(t, h, "POST", "/api/v1/purchases/"+started.ID+"/expiry", map[string]any{"expiresOn": "2099-12-31", "version": started.Version}, admin), 409)

	// Without expiry the default validity is ignored, and offers without one keep administrator-entered expiry.
	open := offer("No expiry", &CreditPolicy{CarryForwardEnabled: true}, 30)
	purchase = financePurchase(t, h, parent, open, "no-expiry")
	if purchase.ValidityDays != nil {
		t.Fatalf("validity recorded although expiry is disabled: %+v", purchase)
	}
	if paid = pay(purchase, "no-expiry-pay"); paid.ExpiresOn != nil || packageExpiry(paid.ID) != nil {
		t.Fatalf("expiry applied although disabled: %+v", paid)
	}
	legacy := offer("Entered expiry", expiring, nil)
	if purchase = financePurchase(t, h, parent, legacy, "legacy"); purchase.Status != "awaiting_expiry" {
		t.Fatalf("offer without default validity must await an administrator expiry: %+v", purchase)
	}

	w = request(t, h, "PATCH", "/api/v1/package-offers/"+monthly.ID, map[string]any{"name": monthly.Name, "credits": 4, "priceMinor": 40000, "currency": "HKD", "active": true, "policyOverride": expiring, "validityDays": nil, "version": monthly.Version}, admin)
	expectStatus(t, w, 200)
	if updated := decodeBody[PackageOffer](t, w); updated.ValidityDays != nil {
		t.Fatalf("validity not cleared: %+v", updated)
	}
}
