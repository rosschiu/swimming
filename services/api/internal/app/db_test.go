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

func TestMigrateLessonDecisionNotifications(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, "DELETE FROM schema_migrations WHERE version IN (19,20); "+schema13+`
		INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version,read_at)
		VALUES ('existing-notice','parent','amy','booking.created','existing-booking',1,'2026-11-18')`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(ctx, `INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version)
		VALUES ('new-notice','parent','amy','lesson_change.approved','change',1)`)
	if err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notifications WHERE id='existing-notice' AND kind='booking.created'
		AND source_id='existing-booking' AND read_at IS NOT NULL)`).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("existing notification preserved: %v, %v", preserved, err)
	}
}

func TestMigrateLessonRequestNotifications(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, "DELETE FROM schema_migrations WHERE version=20; "+schema19+`
		INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version,read_at)
		VALUES ('prior-decision','parent','amy','lesson_change.approved','prior-change',1,'2026-11-18')`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(ctx, `INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version)
		VALUES ('coach-request-notice','coach','amy','lesson_change.requested','another-change',1)`)
	if err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notifications WHERE id='prior-decision' AND kind='lesson_change.approved'
		AND recipient_id='parent' AND student_id='amy' AND source_id='prior-change' AND read_at IS NOT NULL)`).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("existing decision notification preserved: %v, %v", preserved, err)
	}
}

func TestMigrateAuditEntityIndex(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_, err := db.Exec(ctx, `DELETE FROM schema_migrations WHERE version=21;
		DROP INDEX audit_events_entity_id_id;
		INSERT INTO audit_events(actor_id,action,entity_id,details)
		VALUES ('coach','attendance.present','existing-booking','{"markedAt":"2026-11-18T10:00:00Z"}')`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var indexed, preserved bool
	err = db.QueryRow(ctx, `SELECT to_regclass('audit_events_entity_id_id') IS NOT NULL,
		EXISTS(SELECT 1 FROM audit_events WHERE actor_id='coach' AND action='attendance.present'
		AND entity_id='existing-booking' AND details->>'markedAt'='2026-11-18T10:00:00Z')`).Scan(&indexed, &preserved)
	if err != nil || !indexed || !preserved {
		t.Fatalf("audit entity migration: indexed=%v preserved=%v err=%v", indexed, preserved, err)
	}
}
