CREATE TABLE booking_attendance (
    booking_id text PRIMARY KEY REFERENCES bookings(id),
    status text NOT NULL CHECK (status = 'present'),
    actor_id text NOT NULL REFERENCES users(id),
    marked_at timestamptz NOT NULL,
    movement_id text NOT NULL UNIQUE REFERENCES credit_movements(id)
);
