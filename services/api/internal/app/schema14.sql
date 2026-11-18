CREATE TABLE enrolments (
    id text PRIMARY KEY,
    student_id text NOT NULL REFERENCES students(id),
    series_id text NOT NULL REFERENCES class_series(id),
    credit_package_id text NOT NULL,
    actor_id text NOT NULL REFERENCES users(id),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 100),
    request jsonb NOT NULL CHECK (jsonb_typeof(request) = 'object'),
    created_at timestamptz NOT NULL,
    FOREIGN KEY (credit_package_id, student_id) REFERENCES credit_packages(id, student_id),
    UNIQUE (actor_id, idempotency_key)
);

ALTER TABLE bookings ADD COLUMN enrolment_id text REFERENCES enrolments(id);
ALTER TABLE bookings ALTER COLUMN idempotency_key DROP NOT NULL;
ALTER TABLE bookings ADD CHECK ((enrolment_id IS NULL) = (idempotency_key IS NOT NULL));
CREATE INDEX bookings_enrolment ON bookings(enrolment_id) WHERE enrolment_id IS NOT NULL;
