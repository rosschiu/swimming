ALTER TABLE users ADD COLUMN teaching_email_enabled boolean NOT NULL DEFAULT false;
CREATE TABLE notifications (
 id text PRIMARY KEY,
 recipient_id text NOT NULL REFERENCES users(id),
 student_id text NOT NULL REFERENCES students(id),
 kind text NOT NULL CHECK(kind IN ('progress.created','progress.updated','milestone.achieved','milestone.revoked')),
 source_id text NOT NULL,
 source_version integer NOT NULL CHECK(source_version>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 read_at timestamptz,
 UNIQUE(recipient_id,kind,source_id,source_version)
);
CREATE INDEX notifications_recipient ON notifications(recipient_id,created_at DESC,id DESC);
CREATE TABLE notification_deliveries (
 id text PRIMARY KEY,
 notification_id text NOT NULL UNIQUE REFERENCES notifications(id),
 status text NOT NULL CHECK(status IN ('pending','leased','sent','retry','failed','suppressed')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
 cycle_attempts integer NOT NULL DEFAULT 0 CHECK(cycle_attempts BETWEEN 0 AND 5),
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 next_attempt_at timestamptz,
 completed_at timestamptz,
 last_failure text,
 lease_token text,
 lease_until timestamptz,
 recipient text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((status='leased')=(lease_token IS NOT NULL AND lease_until IS NOT NULL))
);
CREATE INDEX notification_deliveries_due ON notification_deliveries(next_attempt_at) WHERE status IN ('pending','retry');
CREATE INDEX notification_deliveries_lease ON notification_deliveries(lease_until) WHERE status='leased';
