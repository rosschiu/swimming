CREATE TABLE credit_grants (
    id text PRIMARY KEY,
    student_id text NOT NULL REFERENCES students(id),
    credits integer NOT NULL CHECK (credits BETWEEN 1 AND 100),
    expires_on date NOT NULL,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    actor_id text NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL,
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 100),
    request jsonb NOT NULL,
    UNIQUE (actor_id, idempotency_key)
);
CREATE TRIGGER immutable_credit_grants BEFORE UPDATE OR DELETE ON credit_grants FOR EACH ROW EXECUTE FUNCTION reject_finance_history_mutation();
ALTER TABLE credit_packages ALTER COLUMN purchase_id DROP NOT NULL;
ALTER TABLE credit_packages ADD COLUMN grant_id text REFERENCES credit_grants(id);
ALTER TABLE credit_packages ADD CONSTRAINT credit_package_origin CHECK ((purchase_id IS NULL) <> (grant_id IS NULL));
CREATE UNIQUE INDEX one_grant_credit_package ON credit_packages(grant_id) WHERE source_package_id IS NULL;
