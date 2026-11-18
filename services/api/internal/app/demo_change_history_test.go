package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationDemoChangeHistoryScope(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	anchor := time.Date(2026, 11, 18, 12, 0, 0, 0, hongKong)
	now := func() time.Time { return anchor }
	run := func() error {
		return seedDemoChangeHistory(ctx, db, now, func(string) error { return nil })
	}
	if err := run(); err == nil || !strings.Contains(err.Error(), "school marker") {
		t.Fatalf("minimal school accepted: %v", err)
	}
	if err := SeedDemoSchool(ctx, db, anchor); err != nil {
		t.Fatal(err)
	}
	before := demoHistoryProtectedSnapshot(t, db)
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if after := demoHistoryProtectedSnapshot(t, db); before != after {
		t.Fatal("seed changed credits, purchase, attendance or notification history")
	}
	h := testHandler(db)
	for _, scenario := range []struct {
		account       string
		status, count int
	}{
		{"admin", 200, 3}, {"coach", 200, 3}, {"parent", 200, 1}, {"other-parent", 200, 0},
		{"demo-coach-chloe", 200, 0}, {"demo-student-01-guardian", 200, 1}, {"demo-student-04-guardian", 200, 1},
		{"student", 403, 0},
	} {
		auth := login(t, h, scenario.account+"@swim.test")
		w := request(t, h, "GET", "/api/v1/lesson-changes", nil, auth)
		expectStatus(t, w, scenario.status)
		if scenario.status == 200 {
			x := decodeBody[struct{ Items []LessonChange }](t, w)
			if len(x.Items) != scenario.count {
				t.Fatalf("%s sees %d requests, want %d", scenario.account, len(x.Items), scenario.count)
			}
		}
	}
	admin := login(t, h, "admin@swim.test")
	queue := request(t, h, "GET", "/api/v1/admin/action-queue", nil, admin)
	expectStatus(t, queue, 200)
	if got := decodeBody[AdminActionQueue](t, queue).PendingLessonChanges; got != 1 {
		t.Fatalf("pending queue got %d, want 1", got)
	}
	student := login(t, h, "student@swim.test")
	bookings := request(t, h, "GET", "/api/v1/students/amy/bookings?from=2026-11-23&to=2026-11-23", nil, student)
	expectStatus(t, bookings, 200)
	found := false
	for _, b := range decodeBody[struct{ Items []StudentBooking }](t, bookings).Items {
		if b.ID == "demo-booking-amy-3" && b.Class.ID == "demo-group-1-3" {
			found = true
		}
	}
	if !found {
		t.Fatal("student cannot see original pending booking")
	}
	var requestID string
	if err := db.QueryRow(ctx, "SELECT id FROM lesson_change_requests WHERE idempotency_key=$1",
		demoHistorySeed+"-amy").Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	coach := login(t, h, "coach@swim.test")
	expectStatus(t, request(t, h, "PUT", "/api/v1/lesson-changes/"+requestID+"/decision",
		map[string]string{"decision": "rejected", "reason": "Visitor decision"}, coach), 200)
	anchor = anchor.AddDate(0, 0, 90)
	var historyBefore, historyAfter string
	query := "SELECT md5(jsonb_agg(to_jsonb(r) ORDER BY id)::text) FROM lesson_change_requests r"
	if err := db.QueryRow(ctx, query).Scan(&historyBefore); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, query).Scan(&historyAfter); err != nil || historyBefore != historyAfter {
		t.Fatalf("completed replay changed visitor history: %v", err)
	}
}

func TestIntegrationDemoChangeHistorySingleConnection(t *testing.T) {
	db := testDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	anchor := time.Date(2026, 11, 18, 12, 0, 0, 0, hongKong)
	if err := SeedDemoSchool(ctx, db, anchor); err != nil {
		t.Fatal(err)
	}
	cfg := db.Config()
	cfg.MaxConns = 1
	single, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	if err := seedDemoChangeHistory(ctx, single, func() time.Time { return anchor }, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func demoHistoryProtectedSnapshot(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	err := db.QueryRow(context.Background(), `SELECT md5(jsonb_build_array(
		(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM users t),
		(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM class_sessions t WHERE series_id IS NOT NULL),
		(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM enrolments t),
		(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM purchases t),
		(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM credit_packages t),
		(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM credit_movements t),
		(SELECT jsonb_agg(to_jsonb(t) ORDER BY booking_id) FROM booking_attendance t),
		(SELECT count(*) FROM notifications),
		(SELECT count(*) FROM notification_deliveries))::text)`).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
