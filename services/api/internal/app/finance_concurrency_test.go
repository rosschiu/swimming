package app

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestIntegrationFinanceConcurrencyAndRollback(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	admin := login(t, h, "admin@swim.test")
	parent := login(t, h, "parent@swim.test")
	policy := CreditPolicy{CarryForwardEnabled: true, EarlyCancellationRetainsCredit: true, LateCancellationRetainsCredit: true, AbsenceRetainsCredit: true, IllnessRetainsCredit: true}
	offer := financeOffer(t, h, admin, &policy)
	order := map[string]any{"offerId": offer.ID, "offerVersion": 1, "idempotencyKey": "race-order"}
	codes := make(chan int, 2)
	for range 2 {
		go func() { codes <- concurrentRequest(h, "/api/v1/students/amy/purchases", order, parent) }()
	}
	first, second := <-codes, <-codes
	if !((first == 200 && second == 201) || (first == 201 && second == 200)) {
		t.Fatal(first, second)
	}
	w := request(t, h, "POST", "/api/v1/students/amy/purchases", order, parent)
	expectStatus(t, w, 200)
	p := decodeBody[Purchase](t, w)
	attemptPath := "/api/v1/purchases/" + p.ID + "/payment-attempts"
	for range 2 {
		go func() {
			codes <- concurrentRequest(h, attemptPath, map[string]string{"idempotencyKey": "race-attempt"}, parent)
		}()
	}
	first, second = <-codes, <-codes
	if !((first == 200 && second == 201) || (first == 201 && second == 200)) {
		t.Fatal(first, second)
	}
	w = request(t, h, "POST", attemptPath, map[string]string{"idempotencyKey": "race-attempt"}, parent)
	expectStatus(t, w, 200)
	at := decodeBody[PaymentAttempt](t, w)
	path := "/api/v1/payment-attempts/" + at.ID + "/mock-outcome"
	ctx := context.Background()
	if _, err := db.Exec(ctx, "ALTER TABLE audit_events ADD CONSTRAINT reject_payment_success CHECK(action<>'payment_attempt.succeeded') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "POST", path, map[string]string{"outcome": "succeeded"}, parent), 500)
	var packages, movements, receipts int
	var status string
	if err := db.QueryRow(ctx, "SELECT (SELECT count(*) FROM credit_packages),(SELECT count(*) FROM credit_movements),(SELECT count(*) FROM receipts),(SELECT status FROM payment_attempts WHERE id=$1)", at.ID).Scan(&packages, &movements, &receipts, &status); err != nil || packages != 0 || movements != 0 || receipts != 0 || status != "pending" {
		t.Fatal("partial rollback", packages, movements, receipts, status, err)
	}
	if _, err := db.Exec(ctx, "ALTER TABLE audit_events DROP CONSTRAINT reject_payment_success"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		go func() { codes <- concurrentRequest(h, path, map[string]string{"outcome": "succeeded"}, parent) }()
	}
	first, second = <-codes, <-codes
	if first != 200 || second != 200 {
		t.Fatal(first, second)
	}
	if err := db.QueryRow(ctx, "SELECT (SELECT count(*) FROM credit_packages),(SELECT count(*) FROM credit_movements),(SELECT count(*) FROM receipts)").Scan(&packages, &movements, &receipts); err != nil || packages != 1 || movements != 1 || receipts != 1 {
		t.Fatal("duplicate fulfillment", packages, movements, receipts, err)
	}
	p = financePurchase(t, h, parent, offer, "opposing-order")
	at = financeAttempt(t, h, parent, p, "opposing-attempt")
	path = "/api/v1/payment-attempts/" + at.ID + "/mock-outcome"
	for _, outcome := range []string{"succeeded", "failed"} {
		go func(outcome string) {
			codes <- concurrentRequest(h, path, map[string]string{"outcome": outcome}, parent)
		}(outcome)
	}
	first, second = <-codes, <-codes
	if !((first == 200 && second == 409) || (first == 409 && second == 200)) {
		t.Fatal(first, second)
	}
	if err := db.QueryRow(ctx, "SELECT status FROM payment_attempts WHERE id=$1", at.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	expected := 1
	if status == "succeeded" {
		expected = 2
	}
	if err := db.QueryRow(ctx, "SELECT (SELECT count(*) FROM credit_packages),(SELECT count(*) FROM credit_movements),(SELECT count(*) FROM receipts)").Scan(&packages, &movements, &receipts); err != nil || packages != expected || movements != expected || receipts != expected {
		t.Fatal(packages, movements, receipts, status, err)
	}
}

func TestIntegrationFinanceRechecksRevocationAfterLock(t *testing.T) {
	for _, mutation := range []string{"unlink", "disable"} {
		t.Run(mutation, func(t *testing.T) {
			db := testDB(t)
			h := testHandler(db)
			admin := login(t, h, "admin@swim.test")
			parent := login(t, h, "parent@swim.test")
			policy := CreditPolicy{}
			offer := financeOffer(t, h, admin, &policy)
			p := financePurchase(t, h, parent, offer, "order")
			at := financeAttempt(t, h, parent, p, "attempt")
			ctx := context.Background()
			a := &API{db: db}
			tx, err := a.beginIdentity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			codes := make(chan int, 1)
			go func() {
				codes <- concurrentRequest(h, "/api/v1/payment-attempts/"+at.ID+"/mock-outcome", map[string]string{"outcome": "succeeded"}, parent)
			}()
			deadline := time.Now().Add(5 * time.Second)
			waiting := false
			for time.Now().Before(deadline) {
				if err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query='SELECT pg_advisory_xact_lock(829417002)')").Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("finance did not wait for identity lock")
			}
			want := 404
			if mutation == "unlink" {
				_, err = tx.Exec(ctx, "DELETE FROM student_access WHERE student_id='amy' AND user_id='parent'")
			} else {
				_, err = tx.Exec(ctx, "UPDATE users SET active=false WHERE id='parent'")
				want = 401
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case code := <-codes:
				if code != want {
					t.Fatal(code, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("finance request did not finish")
			}
			var grants int
			if err = db.QueryRow(ctx, "SELECT count(*) FROM credit_packages").Scan(&grants); err != nil || grants != 0 {
				t.Fatal("revoked guardian granted credits", grants, err)
			}
			expectStatus(t, request(t, h, http.MethodGet, "/api/v1/purchases/"+p.ID, nil, parent), want)
		})
	}
}
