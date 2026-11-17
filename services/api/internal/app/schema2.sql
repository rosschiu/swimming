ALTER TABLE users ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK(version>0);
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
ALTER TABLE users ADD CONSTRAINT active_password CHECK(NOT active OR password_hash IS NOT NULL);
ALTER TABLE student_access ADD COLUMN kind text;
UPDATE student_access sa SET kind=CASE u.role WHEN 'parent' THEN 'guardian' WHEN 'coach' THEN 'coach' WHEN 'student' THEN 'self' END FROM users u WHERE u.id=sa.user_id;
ALTER TABLE student_access ALTER COLUMN kind SET NOT NULL;
ALTER TABLE student_access ADD CONSTRAINT relationship_kind CHECK(kind IN ('guardian','coach','self'));
CREATE UNIQUE INDEX student_self_user ON student_access(user_id) WHERE kind='self';
CREATE UNIQUE INDEX student_self_student ON student_access(student_id) WHERE kind='self';
CREATE INDEX student_access_user ON student_access(user_id);
CREATE TABLE invitations(
 id text PRIMARY KEY,
 user_id text NOT NULL UNIQUE REFERENCES users(id),
 status text NOT NULL CHECK(status IN ('pending','accepted','revoked')),
 expires_at timestamptz NOT NULL,
 created_by text NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE auth_tokens(
 token_hash text PRIMARY KEY,
 user_id text NOT NULL REFERENCES users(id),
 purpose text NOT NULL CHECK(purpose IN ('invitation','password_reset')),
 invitation_id text REFERENCES invitations(id),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz
);
CREATE INDEX auth_tokens_user ON auth_tokens(user_id,purpose);
CREATE TABLE email_deliveries(
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id text NOT NULL REFERENCES users(id),
 invitation_id text REFERENCES invitations(id),
 purpose text NOT NULL,
 recipient text NOT NULL,
 status text NOT NULL CHECK(status IN ('pending','sent','failed')),
 attempts integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 completed_at timestamptz
);
CREATE INDEX deliveries_invitation ON email_deliveries(invitation_id,id DESC);
