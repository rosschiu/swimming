CREATE TABLE class_substitutions (
    id text PRIMARY KEY,
    class_id text NOT NULL REFERENCES class_sessions(id),
    from_coach_id text NOT NULL REFERENCES users(id),
    to_coach_id text NOT NULL REFERENCES users(id),
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    actor_id text NOT NULL REFERENCES users(id),
    substituted_at timestamptz NOT NULL,
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 100),
    request jsonb NOT NULL,
    CHECK (from_coach_id <> to_coach_id),
    UNIQUE (actor_id, idempotency_key)
);
CREATE INDEX class_substitutions_class ON class_substitutions(class_id, substituted_at);
CREATE TRIGGER immutable_class_substitutions BEFORE UPDATE OR DELETE ON class_substitutions FOR EACH ROW EXECUTE FUNCTION reject_finance_history_mutation();
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK (kind IN ('progress.created','progress.updated','milestone.achieved','milestone.revoked','booking.created','lesson_change.requested','lesson_change.approved','lesson_change.rejected','booking.cancelled','class.cancelled','class.coach_changed'));
