CREATE TABLE availability_settings(id boolean PRIMARY KEY DEFAULT true CHECK(id),lock_hours integer NOT NULL DEFAULT 72 CHECK(lock_hours BETWEEN 0 AND 8760),version integer NOT NULL DEFAULT 1 CHECK(version>0));
INSERT INTO availability_settings(id) VALUES(true);
CREATE TABLE coach_availability(id text PRIMARY KEY,coach_id text NOT NULL REFERENCES users(id),venue_id text NOT NULL REFERENCES venues(id),starts_at timestamptz NOT NULL,ends_at timestamptz NOT NULL,capacity integer NOT NULL CHECK(capacity>0),status text NOT NULL DEFAULT 'submitted' CHECK(status IN ('submitted','cancelled')),version integer NOT NULL DEFAULT 1 CHECK(version>0),CHECK(ends_at>starts_at));
CREATE INDEX coach_availability_coach_time ON coach_availability(coach_id,starts_at,ends_at) WHERE status='submitted';
