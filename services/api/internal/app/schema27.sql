ALTER TABLE class_sessions ADD COLUMN cancelled_at timestamptz;
CREATE TABLE class_cancellations (
    class_id text PRIMARY KEY REFERENCES class_sessions(id),
    actor_id text NOT NULL REFERENCES users(id),
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    cancelled_at timestamptz NOT NULL,
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 100),
    request jsonb NOT NULL,
    UNIQUE (actor_id, idempotency_key)
);
CREATE TRIGGER immutable_class_cancellations BEFORE UPDATE OR DELETE ON class_cancellations FOR EACH ROW EXECUTE FUNCTION reject_finance_history_mutation();
ALTER TABLE booking_cancellations ADD COLUMN cancelled_by_school boolean NOT NULL DEFAULT false;
ALTER TABLE booking_cancellations ALTER COLUMN timing DROP NOT NULL;
ALTER TABLE booking_cancellations ADD CONSTRAINT booking_cancellation_timing CHECK (cancelled_by_school = (timing IS NULL));
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (kind IN ('progress.created','progress.updated','milestone.achieved','milestone.revoked','booking.created','lesson_change.requested','lesson_change.approved','lesson_change.rejected','booking.cancelled','class.cancelled'));
