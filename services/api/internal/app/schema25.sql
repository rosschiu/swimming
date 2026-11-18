ALTER TABLE booking_attendance DROP CONSTRAINT booking_attendance_status_check;
ALTER TABLE booking_attendance ADD CONSTRAINT booking_attendance_status_check CHECK (status IN ('present','absent','ill'));
ALTER TABLE booking_attendance ADD COLUMN credit_outcome text NOT NULL DEFAULT 'consumed' CHECK (credit_outcome IN ('consumed','retained','expired'));
ALTER TABLE booking_attendance ALTER COLUMN credit_outcome DROP DEFAULT;
ALTER TABLE booking_attendance ADD COLUMN revision integer NOT NULL DEFAULT 1 CHECK (revision > 0);
CREATE TABLE attendance_corrections (
    id text PRIMARY KEY,
    booking_id text NOT NULL REFERENCES booking_attendance(booking_id),
    revision integer NOT NULL CHECK (revision > 1),
    from_status text NOT NULL CHECK (from_status IN ('present','absent','ill')),
    to_status text NOT NULL CHECK (to_status IN ('present','absent','ill')),
    from_outcome text NOT NULL CHECK (from_outcome IN ('consumed','retained','expired')),
    to_outcome text NOT NULL CHECK (to_outcome IN ('consumed','retained','expired')),
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    actor_id text NOT NULL REFERENCES users(id),
    movement_id text UNIQUE REFERENCES credit_movements(id),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 100),
    request jsonb NOT NULL,
    corrected_at timestamptz NOT NULL,
    UNIQUE (booking_id, revision),
    UNIQUE (actor_id, idempotency_key)
);
CREATE TRIGGER immutable_attendance_corrections BEFORE UPDATE OR DELETE ON attendance_corrections FOR EACH ROW EXECUTE FUNCTION reject_finance_history_mutation();
ALTER TABLE lesson_change_requests DROP CONSTRAINT lesson_change_requests_status_check;
ALTER TABLE lesson_change_requests ADD CONSTRAINT lesson_change_requests_status_check CHECK (status IN ('pending','approved','rejected','withdrawn','closed'));
