# Stage03a: individual purchases and mock payments

User decision2026-11-18: an administrator enters expiry individually per purchase. AI/voice and provider selection are deferred beyond first delivery. This stage starts from verified main2d1fca1 on stage/03a-purchases-credits.

## Complete workflow

Administrators manage versioned package offers and credit-policy defaults. Guardians choose an active offer for a linked child; purchase creation snapshots trusted terms. An expiry-enabled purchase awaits an administrator-entered inclusive Hong Kong date before payment. Expiry-disabled purchases display no expiry and can proceed immediately. Guardians see the assigned terms before starting clearly labelled simulated payment.

Payment attempts support pending, failed and succeeded outcomes. Failed attempts remain in history; explicit retry creates a new attempt on the same purchase. Success atomically creates one credit package, grant movement and simulated receipt. Duplicate success returns the same records. No real payment transport or actual monetary transaction.

## Invariants and implementation boundaries

Server-owned integer amounts and quantities; current role and guardian scope checked for every operation including replay. Student/coach access to general records does not grant financial access. Identity mutations and financial operations coordinate their locks; purchase rows and unique constraints prevent duplicate fulfillment. Terms are frozen before attempts; offer changes affect new purchases. A first settlement after expiry is rejected, while replay of an earlier success remains valid.

Use existing Go/pgx transactions, OpenAPI and React protected-request patterns. Do not introduce payment framework infrastructure. Booking reservations, recurring renewal, lifecycle corrections, recovery and expiry jobs belong to subsequent slices. Credit views must truthfully distinguish usable and expired balances without claiming jobs have run.

## Acceptance and ownership

Sol develops; Astra independently reviews and tests; lead integrates contracts, documentation and release. Agree endpoint/response shapes before parallel frontend/backend changes. Tests cover scope revocation, stale versions, snapshot preservation, date boundaries, disabled expiry, retries, duplicate/concurrent outcomes, atomic rollback, child switching and reload. Real-stack Playwright follows admin offer, guardian purchase, admin expiry, guardian failure/retry/success/replay and exactly one receipt/grant. Cumulative frontend, Go/PostgreSQL race, contracts and Playwright/CI must pass before a reviewed stage PR is merged.

No completed implementation or acceptance is claimed by this plan.
