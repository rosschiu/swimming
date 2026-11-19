CREATE TABLE imports (
    id text PRIMARY KEY,
    file_sha256 text NOT NULL UNIQUE CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
    actor_id text NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL,
    students integer NOT NULL CHECK (students > 0),
    guardians_created integer NOT NULL CHECK (guardians_created >= 0),
    guardians_linked integer NOT NULL CHECK (guardians_linked >= 0),
    packages integer NOT NULL CHECK (packages >= 0),
    credits integer NOT NULL CHECK (credits >= 0)
);
CREATE TRIGGER immutable_imports BEFORE UPDATE OR DELETE ON imports FOR EACH ROW EXECUTE FUNCTION reject_finance_history_mutation();
-- A migrated balance is held like a grant, so it books, expires and recovers the same way, but is labelled as migrated.
ALTER TABLE credit_grants ADD COLUMN kind text NOT NULL DEFAULT 'complimentary' CHECK (kind IN ('complimentary','migrated'));
ALTER TABLE credit_grants ADD COLUMN import_id text REFERENCES imports(id);
ALTER TABLE credit_grants DROP CONSTRAINT credit_grants_credits_check;
ALTER TABLE credit_grants ADD CONSTRAINT credit_grants_credits_check CHECK ((kind = 'complimentary' AND credits BETWEEN 1 AND 100) OR (kind = 'migrated' AND credits BETWEEN 1 AND 1000));
ALTER TABLE credit_grants ADD CONSTRAINT credit_grants_import CHECK ((kind = 'migrated') = (import_id IS NOT NULL));
