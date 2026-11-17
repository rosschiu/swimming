CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE users (
 id text PRIMARY KEY,
 email text NOT NULL UNIQUE CHECK (email = lower(btrim(email))),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
 role text NOT NULL CHECK (role IN ('admin','parent','coach','student')),
 password_hash text NOT NULL,
 active boolean NOT NULL DEFAULT true
);
CREATE TABLE students (
 id text PRIMARY KEY,
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
 level text NOT NULL CHECK (length(btrim(level)) BETWEEN 1 AND 100),
 version integer NOT NULL DEFAULT 1 CHECK (version > 0)
);
CREATE TABLE student_access (
 student_id text NOT NULL REFERENCES students(id),
 user_id text NOT NULL REFERENCES users(id),
 PRIMARY KEY(student_id,user_id)
);
CREATE TABLE sessions (
 token_hash text PRIMARY KEY,
 user_id text NOT NULL REFERENCES users(id),
 csrf_token text NOT NULL,
 expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id text NOT NULL REFERENCES users(id),
 action text NOT NULL,
 entity_id text NOT NULL,
 details jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
