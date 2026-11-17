# Stage 07c: administrator revocation at transaction boundaries

The cumulative security review found that twelve student and scheduling mutations check the administrator before decoding the request, but do not recheck after acquiring their database transaction locks. An already admitted request can therefore commit after another administrator disables its actor. New requests are already rejected; the gap concerns in-flight requests.

## Change and acceptance

Reuse `currentAdminAccess` inside each affected transaction, before business reads, writes or idempotent replay. Lock the active administrator row with `FOR SHARE` until the transaction finishes. A disable that commits first must cause rejection; an already authorized transaction must finish before disabling can commit. Preserve coach availability's existing eligibility checks and avoid coupling the scheduling and identity advisory locks.

Cover student create/update; scheduling settings; venue create/update; class-type create/update; class create/update; recurring-series creation; availability settings; and creation of a class from availability, including replay. Rejected requests must leave business records and audit history unchanged, apart from the legitimate disabling operation.

Sol owns the five production files. Astra independently owns deterministic PostgreSQL regression tests and reviews lock ordering, authorization, rollback and replay. The lead owns integration, cumulative validation, contracts and release records. Implementation is isolated from the immutable security-review revision in a separate worktree.

## Gates

Demonstrate the regression against the original revision in a disposable worktree, then run the focused race tests against the fix. Run cumulative Go/PostgreSQL race tests, frontend tests/build, API contracts and real-stack Playwright; retain representative scale and restore checks in CI. Review the integrated change before a stage PR is merged. No schema, payment, provider, hosting or native-client work belongs to this correction.

The audit also identified missing CSRF header declarations for three notification operations. Correct those contract declarations and regenerate types without changing the existing runtime enforcement; verify the contract check and browser requests.
