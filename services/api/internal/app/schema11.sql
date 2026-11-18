ALTER TABLE credit_packages ADD UNIQUE (id, student_id);

CREATE TABLE bookings (
    id text PRIMARY KEY,
    student_id text NOT NULL REFERENCES students(id),
    class_id text NOT NULL REFERENCES class_sessions(id),
    credit_package_id text NOT NULL,
    actor_id text NOT NULL REFERENCES users(id),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 100),
    request jsonb NOT NULL CHECK (jsonb_typeof(request) = 'object'),
    class_version integer NOT NULL CHECK (class_version > 0),
    class_type_version integer NOT NULL CHECK (class_type_version > 0),
    student_level text NOT NULL CHECK (length(btrim(student_level)) BETWEEN 1 AND 100),
    class_level text NOT NULL CHECK (length(btrim(class_level)) BETWEEN 1 AND 100),
    override_actor_id text REFERENCES users(id),
    override_reason text,
    validity_rule text NOT NULL CHECK (length(btrim(validity_rule)) > 0),
    status text NOT NULL DEFAULT 'reserved' CHECK (status = 'reserved'),
    created_at timestamptz NOT NULL,
    FOREIGN KEY (credit_package_id, student_id) REFERENCES credit_packages(id, student_id),
    UNIQUE (actor_id, idempotency_key),
    CHECK ((override_actor_id IS NULL AND override_reason IS NULL)
        OR (override_actor_id IS NOT NULL AND override_actor_id = actor_id
            AND override_reason IS NOT NULL AND length(btrim(override_reason)) BETWEEN 1 AND 500))
);

CREATE UNIQUE INDEX bookings_student_class ON bookings(student_id, class_id) WHERE status = 'reserved';
CREATE INDEX bookings_class ON bookings(class_id);
