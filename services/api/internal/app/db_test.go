package app

import (
	"context"
	"testing"
)

func TestMigrateSchoolSettingsUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  string
		cutoff int
		rule   string
	}{
		{
			name: "from fixed enrolment and school settings without attendance",
			setup: `DROP TABLE booking_attendance;
				DELETE FROM schema_migrations WHERE version = 15;
				UPDATE booking_validity_settings SET validity_rule = 'lesson_start', version = 2`,
			cutoff: 24,
			rule:   "lesson_start",
		},
		{
			name: "from fixed enrolment",
			setup: `DROP TABLE booking_validity_settings;
				DELETE FROM schema_migrations WHERE version = 16;
				UPDATE lesson_settings SET parent_change_cutoff_hours = 48, version = 2`,
			cutoff: 48,
			rule:   "lesson_end",
		},
		{
			name: "from booking validity",
			setup: `DROP TABLE lesson_settings;
				DELETE FROM schema_migrations WHERE version = 12;
                ALTER TABLE bookings DROP COLUMN enrolment_id;
                ALTER TABLE bookings ALTER COLUMN idempotency_key SET NOT NULL;
                DROP TABLE enrolments;
                DELETE FROM schema_migrations WHERE version = 14;
				UPDATE booking_validity_settings SET validity_rule = 'lesson_start', version = 2`,
			cutoff: 24,
			rule:   "lesson_start",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			ctx := context.Background()
			if _, err := db.Exec(ctx, tc.setup); err != nil {
				t.Fatal(err)
			}
			if err := Migrate(ctx, db); err != nil {
				t.Fatal(err)
			}
			if err := Migrate(ctx, db); err != nil {
				t.Fatal(err)
			}
			var cutoff, recordedVersions int
			var rule string
			var attendanceTable bool
			err := db.QueryRow(ctx, `SELECT parent_change_cutoff_hours, validity_rule,
				(SELECT count(*) FROM schema_migrations WHERE version IN (12, 14, 15, 16, 17)),
				to_regclass('booking_attendance') IS NOT NULL
				FROM lesson_settings CROSS JOIN booking_validity_settings`).Scan(&cutoff, &rule, &recordedVersions, &attendanceTable)
			if err != nil {
				t.Fatal(err)
			}
			if cutoff != tc.cutoff || rule != tc.rule || recordedVersions != 5 || !attendanceTable {
				t.Fatalf("after upgrade, got cutoff %d, rule %s, %d recorded migration IDs, attendance table %t", cutoff, rule, recordedVersions, attendanceTable)
			}
		})
	}
}
