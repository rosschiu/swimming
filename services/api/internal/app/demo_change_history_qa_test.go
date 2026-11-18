package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Fingerprint complete financial and communication rows, including attribution and
// timestamps. Count-only checks would miss changes to pre-existing receipts or balances.
func historyProtectedRows(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var fingerprint string
	for _, table := range []string{"users", "students", "credit_packages", "credit_movements", "purchases", "payment_attempts", "receipts", "notifications", "notification_deliveries", "booking_attendance", "enrolments"} {
		var digest string
		if err := db.QueryRow(context.Background(), "SELECT md5(coalesce(string_agg(row_to_json(r)::text,'' ORDER BY row_to_json(r)::text),'')) FROM "+table+" r").Scan(&digest); err != nil {
			t.Fatal(err)
		}
		fingerprint += digest
	}
	return fingerprint
}

func TestIntegrationDemoHistoryConcurrentCutoff(t *testing.T) {
	anchor := time.Date(2026, 11, 16, 0, 0, 0, 0, hongKong)
	for _, tc := range []struct {
		name   string
		at     time.Time
		source string
	}{
		{"cutoff equality", anchor.AddDate(0, 0, 6).Add(17 * time.Hour), "demo-group-1-3"},
		{"one microsecond later", anchor.AddDate(0, 0, 6).Add(17*time.Hour + time.Microsecond), "demo-group-1-4"},
		{"both windows closed", anchor.AddDate(0, 0, 13).Add(17*time.Hour + time.Microsecond), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := testDB(t)
			if err := SeedDemoSchool(ctx, db, anchor); err != nil {
				t.Fatal(err)
			}
			before := historyProtectedRows(t, db)
			start := make(chan struct{})
			results := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func() {
					<-start
					results <- seedDemoChangeHistory(ctx, db, func() time.Time { return tc.at }, func(string) error { return nil })
				}()
			}
			close(start)
			for i := 0; i < 2; i++ {
				if err := <-results; (err == nil) != (tc.source != "") {
					t.Fatalf("seed outcome %v", err)
				}
			}
			var classes, requests, markers int
			err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM class_sessions),
				(SELECT count(*) FROM lesson_change_requests),
				(SELECT count(*) FROM audit_events WHERE action='demo.change_history.seeded')`).Scan(&classes, &requests, &markers)
			if err != nil {
				t.Fatal(err)
			}
			if tc.source == "" {
				if classes != 15 || requests != 0 || markers != 0 {
					t.Fatalf("expired preflight left %d classes, %d requests, %d markers", classes, requests, markers)
				}
			} else {
				var matching int
				if err := db.QueryRow(ctx, "SELECT count(*) FROM lesson_change_requests WHERE original_class_id=$1", tc.source).Scan(&matching); err != nil {
					t.Fatal(err)
				}
				if classes != 16 || requests != 3 || matching != 3 || markers != 1 {
					t.Fatalf("concurrent seed yielded %d classes, %d requests, %d for expected source, %d markers", classes, requests, matching, markers)
				}
			}
			if historyProtectedRows(t, db) != before {
				t.Fatal("financial, attendance, account or communication rows changed")
			}
		})
	}
}

func TestIntegrationDemoHistoryResumeEveryCommit(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, hongKong)
	if err := SeedDemoSchool(ctx, db, now); err != nil {
		t.Fatal(err)
	}
	before := historyProtectedRows(t, db)
	stop := errors.New("simulated interruption after committed mutation")
	for _, want := range []string{"target", "request:amy", "request:demo-student-01", "decision:demo-student-01", "request:demo-student-04", "decision:demo-student-04", "marker"} {
		var got string
		err := seedDemoChangeHistory(ctx, db, func() time.Time { return now }, func(step string) error { got = step; return stop })
		if !errors.Is(err, stop) || got != want {
			t.Fatalf("resume expected %q, saw %q with error %v", want, got, err)
		}
		if historyProtectedRows(t, db) != before {
			t.Fatalf("protected rows changed after %s", want)
		}
	}
	err := seedDemoChangeHistory(ctx, db, func() time.Time { return now.AddDate(1, 0, 0) }, func(step string) error { t.Fatalf("completed replay wrote %s", step); return nil })
	if err != nil {
		t.Fatal(err)
	}
}
