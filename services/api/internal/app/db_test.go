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
			name: "from lesson timing",
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
			err := db.QueryRow(ctx, `SELECT parent_change_cutoff_hours, validity_rule,
				(SELECT count(*) FROM schema_migrations WHERE version IN (12, 16))
				FROM lesson_settings CROSS JOIN booking_validity_settings`).Scan(&cutoff, &rule, &recordedVersions)
			if err != nil {
				t.Fatal(err)
			}
			if cutoff != tc.cutoff || rule != tc.rule || recordedVersions != 2 {
				t.Fatalf("after upgrade, got cutoff %d, rule %s and %d recorded migration IDs", cutoff, rule, recordedVersions)
			}
		})
	}
}
