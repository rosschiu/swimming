package app

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegrationAdminActionQueueCountsAndAccess(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	h := New(Config{Origin: testOrigin, SessionTTL: time.Hour, TeachingEmailAllowlist: []string{"parent@swim.test"}}, db)
	admin := login(t, h, "admin@swim.test")
	parent := login(t, h, "parent@swim.test")
	coach := login(t, h, "coach@swim.test")
	student := login(t, h, "student@swim.test")
	path := "/api/v1/admin/action-queue"
	expectStatus(t, request(t, h, "GET", path, nil, authState{}), 401)
	for _, actor := range []authState{parent, coach, student} {
		expectStatus(t, request(t, h, "GET", path, nil, actor), 403)
	}
	assertCounts := func(purchases, deliveries int) {
		t.Helper()
		w := request(t, h, "GET", path, nil, admin)
		expectStatus(t, w, 200)
		if want, got := (AdminActionQueue{PurchasesAwaitingExpiry: purchases, FailedEmailDeliveries: deliveries}), decodeBody[AdminActionQueue](t, w); got != want {
			t.Fatalf("incorrect action counts: got %+v, want %+v", got, want)
		}
	}
	assertCounts(0, 0)
	offer := financeOffer(t, h, admin, nil)
	purchase := financePurchase(t, h, parent, offer, "queue-expiry")
	assertCounts(1, 0)
	if _, err := db.Exec(ctx, `
UPDATE users SET teaching_email_enabled=true WHERE id='parent';
INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version)
	SELECT status,'parent','amy','progress.created','queue-'||status,1
	FROM unnest(ARRAY['failed','pending','retry','sent','suppressed']) AS status;
INSERT INTO notification_deliveries(id,notification_id,status)
	SELECT id,id,id FROM notifications;
`); err != nil {
		t.Fatal(err)
	}
	assertCounts(1, 1)
	expiry := time.Now().In(hongKong).AddDate(0, 0, 1).Format("2006-01-02")
	expectStatus(t, request(t, h, "POST", "/api/v1/purchases/"+purchase.ID+"/expiry",
		map[string]any{"expiresOn": expiry, "version": purchase.Version}, admin), 200)
	assertCounts(0, 1)
	expectStatus(t, request(t, h, "POST", "/api/v1/notification-deliveries/failed/retry",
		map[string]any{"version": 1, "reason": "SMTP repaired"}, admin), 200)
	assertCounts(0, 0)

	if _, err := db.Exec(ctx, "UPDATE users SET active=false WHERE id='admin'"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "GET", path, nil, admin), 401)
	// Simulate a request authenticated just before administrator deactivation.
	w := httptest.NewRecorder()
	(&API{db: db}).getAdminActionQueue(w, httptest.NewRequest("GET", path, nil), Session{User: User{ID: "admin", Role: "admin"}})
	expectStatus(t, w, 401)
}
