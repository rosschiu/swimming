ALTER TABLE bookings DROP CONSTRAINT bookings_status_check;
ALTER TABLE bookings ADD CONSTRAINT bookings_status_check CHECK (status IN ('reserved','cancelled'));
CREATE TABLE booking_cancellations (
    booking_id text PRIMARY KEY REFERENCES bookings(id),
    actor_id text NOT NULL REFERENCES users(id),
    cancelled_at timestamptz NOT NULL,
    timing text NOT NULL CHECK (timing IN ('early','late')),
    credit_outcome text NOT NULL CHECK (credit_outcome IN ('consumed','retained','expired')),
    movement_id text NOT NULL UNIQUE REFERENCES credit_movements(id),
    class_snapshot jsonb NOT NULL,
    policy jsonb NOT NULL,
    cutoff_hours integer NOT NULL CHECK (cutoff_hours >= 0),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 100),
    request jsonb NOT NULL,
    UNIQUE (actor_id, idempotency_key)
);
CREATE TRIGGER immutable_booking_cancellations BEFORE UPDATE OR DELETE ON booking_cancellations FOR EACH ROW EXECUTE FUNCTION reject_finance_history_mutation();
ALTER TABLE users ADD COLUMN service_email_enabled boolean NOT NULL DEFAULT true;
ALTER TABLE lesson_settings ADD COLUMN cancellation_email boolean NOT NULL DEFAULT true;
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (kind IN ('progress.created','progress.updated','milestone.achieved','milestone.revoked','booking.created','lesson_change.requested','lesson_change.approved','lesson_change.rejected','booking.cancelled'));
