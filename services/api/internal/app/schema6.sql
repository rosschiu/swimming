CREATE TABLE progress_notes (
 id text PRIMARY KEY,
 student_id text NOT NULL REFERENCES students(id),
 author_id text NOT NULL REFERENCES users(id),
 body text NOT NULL CHECK(length(btrim(body)) BETWEEN 1 AND 4000),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 updated_by_id text NOT NULL REFERENCES users(id),
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 request_id text NOT NULL CHECK(length(request_id) BETWEEN 1 AND 128),
 original_body text NOT NULL CHECK(length(btrim(original_body)) BETWEEN 1 AND 4000),
 UNIQUE(author_id,request_id)
);
CREATE INDEX progress_notes_student_time ON progress_notes(student_id,created_at DESC,id DESC);
