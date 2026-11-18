CREATE TABLE lesson_change_requests (
    id text PRIMARY KEY,
    booking_id text NOT NULL REFERENCES bookings(id),
    original_class_id text NOT NULL REFERENCES class_sessions(id),
    target_class_id text NOT NULL REFERENCES class_sessions(id),
    original_class jsonb NOT NULL,
    target_class jsonb NOT NULL,
    requested_by text NOT NULL REFERENCES users(id),
    requested_at timestamptz NOT NULL,
    settings_version integer NOT NULL CHECK (settings_version > 0),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 100),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
    decided_by text REFERENCES users(id),
    decided_at timestamptz,
    decision_reason text CHECK (length(btrim(decision_reason)) BETWEEN 1 AND 500),
    CHECK (original_class_id <> target_class_id),
    CHECK ((status='pending' AND decided_by IS NULL AND decided_at IS NULL AND decision_reason IS NULL)
        OR (status<>'pending' AND decided_by IS NOT NULL AND decided_at IS NOT NULL)),
    UNIQUE (requested_by, idempotency_key)
);

CREATE UNIQUE INDEX lesson_change_one_pending ON lesson_change_requests(booking_id) WHERE status='pending';
CREATE INDEX lesson_change_original_class ON lesson_change_requests(original_class_id);
