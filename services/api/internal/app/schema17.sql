CREATE TABLE fixed_enrolment_settings (
  id boolean PRIMARY KEY DEFAULT true CHECK (id),
  allocation_policy text NOT NULL CHECK (allocation_policy IN ('all_or_none', 'confirm_available_subset')),
  renewal_policy text NOT NULL CHECK (renewal_policy IN ('same_offer', 'any_paid_package')),
  version integer NOT NULL CHECK (version > 0)
);

INSERT INTO fixed_enrolment_settings (allocation_policy, renewal_policy, version)
VALUES ('all_or_none', 'same_offer', 1);
