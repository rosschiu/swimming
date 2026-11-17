CREATE TABLE skills (
 id text PRIMARY KEY,
 name text NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 100),
 description text NOT NULL CHECK(length(description)<=1000),
 active boolean NOT NULL DEFAULT true,
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 created_by_id text NOT NULL REFERENCES users(id),
 request_id text NOT NULL CHECK(length(request_id) BETWEEN 1 AND 128),
 original_name text NOT NULL,
 original_description text NOT NULL,
 UNIQUE(created_by_id,request_id)
);
CREATE TABLE milestone_events (
 id text PRIMARY KEY,
 student_id text NOT NULL REFERENCES students(id),
 skill_id text NOT NULL REFERENCES skills(id),
 actor_id text NOT NULL REFERENCES users(id),
 state text NOT NULL CHECK(state IN ('achieved','revoked')),
 reason text NOT NULL CHECK(length(reason)<=500 AND (state='achieved' OR length(btrim(reason))>0)),
 version integer NOT NULL CHECK(version>0),
 skill_version integer NOT NULL CHECK(skill_version>0),
 skill_name text NOT NULL,
 skill_description text NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 reverses_event_id text REFERENCES milestone_events(id),
 request_id text NOT NULL CHECK(length(request_id) BETWEEN 1 AND 128),
 submitted_version integer NOT NULL CHECK(submitted_version>=0),
 submitted_skill_version integer NOT NULL CHECK(submitted_skill_version>0),
 UNIQUE(actor_id,request_id),
 UNIQUE(student_id,skill_id,version),
 CHECK((state='revoked')=(reverses_event_id IS NOT NULL))
);
CREATE INDEX milestone_events_student_skill ON milestone_events(student_id,skill_id,version DESC);
