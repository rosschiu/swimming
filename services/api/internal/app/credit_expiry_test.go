package app

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type recoveryFixture struct {
	db                   *pgxpool.Pool
	api                  *API
	h                    http.Handler
	admin, parent, coach authState
	purchase             Purchase
	packageID            string
	now                  time.Time
}

func prepareRecovery(t *testing.T) *recoveryFixture {
	t.Helper()
	db := testDB(t)
	base := testHandler(db)
	f := &recoveryFixture{db: db, admin: login(t, base, "admin@swim.test"), parent: login(t, base, "parent@swim.test"), coach: login(t, base, "coach@swim.test"), now: time.Now()}
	f.api = &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return f.now }}
	h := http.NewServeMux()
	f.api.financeRoutes(h)
	f.api.creditRecoveryRoutes(h)
	f.h = h
	offer := financeOffer(t, h, f.admin, nil)
	p := financePurchase(t, h, f.parent, offer, "expiry-fixture")
	date := f.now.In(hongKong).Format("2006-01-02")
	expectStatus(t, request(t, h, "POST", "/api/v1/purchases/"+p.ID+"/expiry", map[string]any{"expiresOn": date, "version": 1}, f.admin), 200)
	at := financeAttempt(t, h, f.parent, p, "expiry-fixture-payment")
	w := request(t, h, "POST", "/api/v1/payment-attempts/"+at.ID+"/mock-outcome", map[string]string{"outcome": "succeeded"}, f.parent)
	expectStatus(t, w, 200)
	f.purchase = decodeBody[Purchase](t, w)
	if err := db.QueryRow(context.Background(), "SELECT id FROM credit_packages WHERE purchase_id=$1", p.ID).Scan(&f.packageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(context.Background(), "UPDATE sessions SET expires_at=expires_at+interval '10 days'"); err != nil {
		t.Fatal(err)
	}
	return f
}
func TestIntegrationCreditExpiryAvailableOnlyAndLaterRelease(t *testing.T) {
	f := prepareRecovery(t)
	ctx := context.Background()
	if _, err := f.db.Exec(ctx, "INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at) VALUES('reserve-fixture',$1,'reserve-fixture',-4,3,1,0,'admin','Synthetic reservation and consumption',$2)", f.packageID, f.now); err != nil {
		t.Fatal(err)
	}
	date := f.now.In(hongKong).Format("2006-01-02")
	midnight, _ := time.ParseInLocation("2006-01-02", date, hongKong)
	f.now = midnight.Add(24*time.Hour - time.Nanosecond)
	if err := f.api.processCreditExpiryBatch(ctx); err != nil {
		t.Fatal(err)
	}
	var available, reserved, consumed, expired int
	balance := func() {
		t.Helper()
		if err := f.db.QueryRow(ctx, "SELECT sum(available),sum(reserved),sum(consumed),sum(expired) FROM credit_movements WHERE package_id=$1", f.packageID).Scan(&available, &reserved, &consumed, &expired); err != nil {
			t.Fatal(err)
		}
	}
	balance()
	if available != 6 || reserved != 3 || consumed != 1 || expired != 0 {
		t.Fatal(available, reserved, consumed, expired)
	}
	f.now = f.now.Add(time.Nanosecond)
	if err := f.api.processCreditExpiryBatch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.api.processCreditExpiryBatch(ctx); err != nil {
		t.Fatal(err)
	}
	balance()
	if available != 0 || reserved != 3 || consumed != 1 || expired != 6 {
		t.Fatal(available, reserved, consumed, expired)
	}
	var count int
	if err := f.db.QueryRow(ctx, "SELECT count(*) FROM credit_movements WHERE package_id=$1 AND actor_id IS NULL", f.packageID).Scan(&count); err != nil || count != 1 {
		t.Fatal("expiry duplicate or forged actor", count, err)
	}
	if _, err := f.db.Exec(ctx, "INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at) VALUES('release-fixture',$1,'release-fixture',2,-2,0,0,'admin','Synthetic released reservation',$2)", f.packageID, f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.api.processCreditExpiryBatch(ctx); err != nil {
		t.Fatal(err)
	}
	balance()
	if available != 0 || reserved != 1 || consumed != 1 || expired != 8 {
		t.Fatal("late release remained usable", available, reserved, consumed, expired)
	}
	w := request(t, f.h, "GET", "/api/v1/students/amy/credits", nil, f.parent)
	expectStatus(t, w, 200)
	items := decodeBody[struct {
		Items []CreditPackage `json:"items"`
	}](t, w).Items
	if len(items) != 1 || items[0].Available != 0 || items[0].Expired != 8 || items[0].Reserved != 1 {
		t.Fatal(items)
	}
}

func TestIntegrationCreditExpiryBatchDrainsBeyondLimit(t *testing.T) {
	f := prepareRecovery(t)
	ctx := context.Background()
	f.now = f.now.Add(24 * time.Hour)
	if _, err := f.db.Exec(ctx, `INSERT INTO credit_packages(id,purchase_id,student_id,credits,expires_on,policy,created_at,source_package_id) SELECT 'batch-'||n,purchase_id,student_id,1,expires_on,policy,created_at,id FROM credit_packages CROSS JOIN generate_series(1,101) n WHERE id=$1`, f.packageID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at) SELECT 'grant-'||id,id,'grant-'||id,1,0,0,0,'admin','Synthetic batch fixture',created_at FROM credit_packages WHERE source_package_id=$1`, f.packageID); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{2, 0, 0} {
		if err := f.api.processCreditExpiryBatch(ctx); err != nil {
			t.Fatal(err)
		}
		var remaining int
		if err := f.db.QueryRow(ctx, "SELECT count(*) FROM (SELECT package_id FROM credit_movements GROUP BY package_id HAVING sum(available)>0) x").Scan(&remaining); err != nil || remaining != want {
			t.Fatal("expiry batch starvation", remaining, want, err)
		}
	}
}
