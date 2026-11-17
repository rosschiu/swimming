CREATE TABLE availability_classes (
    class_id text PRIMARY KEY REFERENCES class_sessions(id),
    availability_id text NOT NULL REFERENCES coach_availability(id),
    actor_id text NOT NULL REFERENCES users(id),
    request_id text NOT NULL CHECK(length(request_id) BETWEEN 1 AND 128),
    request jsonb NOT NULL,
    window_snapshot jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(actor_id, request_id)
);
CREATE INDEX availability_classes_source ON availability_classes(availability_id);
