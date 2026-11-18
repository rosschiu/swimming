ALTER TABLE credit_packages ADD COLUMN source_package_id text REFERENCES credit_packages(id);
ALTER TABLE credit_packages DROP CONSTRAINT credit_packages_purchase_id_key;
CREATE UNIQUE INDEX one_purchase_credit_grant ON credit_packages(purchase_id) WHERE source_package_id IS NULL;
CREATE INDEX credit_packages_student ON credit_packages(student_id);
ALTER TABLE credit_movements ALTER COLUMN actor_id DROP NOT NULL;
CREATE INDEX credit_movements_package ON credit_movements(package_id);
CREATE TABLE credit_recoveries (
 id text PRIMARY KEY,
 student_id text NOT NULL REFERENCES students(id),
 source_package_id text NOT NULL REFERENCES credit_packages(id),
 destination_package_id text NOT NULL UNIQUE REFERENCES credit_packages(id),
 quantity integer NOT NULL CHECK(quantity BETWEEN 1 AND 10000),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 expires_on date NOT NULL,
 actor_id text NOT NULL REFERENCES users(id),
 idempotency_key text NOT NULL,
 request jsonb NOT NULL,
 created_at timestamptz NOT NULL,
 UNIQUE(actor_id,idempotency_key),
 CHECK(source_package_id<>destination_package_id)
);
CREATE TRIGGER immutable_credit_recoveries BEFORE UPDATE OR DELETE ON credit_recoveries FOR EACH ROW EXECUTE FUNCTION reject_finance_history_mutation();
