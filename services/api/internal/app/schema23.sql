ALTER TABLE package_offers ADD COLUMN validity_days integer CHECK (validity_days BETWEEN 1 AND 3650);
