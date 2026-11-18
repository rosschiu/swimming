CREATE TABLE booking_validity_settings (
    id boolean PRIMARY KEY CHECK (id),
    validity_rule text NOT NULL CHECK (validity_rule IN ('lesson_end', 'lesson_start')),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0)
);

INSERT INTO booking_validity_settings(id, validity_rule) VALUES (true, 'lesson_end');
