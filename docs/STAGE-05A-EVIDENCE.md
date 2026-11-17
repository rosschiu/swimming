# Stage 05a progress notes: evidence in progress

Implementation is on stage/05a-student-progress, based on verified main c085d52. No PR or merge yet.

Sol developers implemented backend and profile UI from the lead-owned OpenAPI contract. Migration 6 adds persistent progress notes, actor-scoped creation request IDs and original submitted bodies. Author/current-assignment checks and account checks coordinate with identity changes. Corrections require version and reason, preserve original authorship and record before/after audit history. Families and students receive scoped read access only.

The UI explicitly states shared visibility, supports English/Traditional Chinese, preserves drafts after recoverable failures, and protects creation retries with the original request ID. Uncertain/stale corrections require reloading and explicit review of the current version. Profile components are keyed by account and student so drafts and requests cannot carry across profiles.

Lead source review covered routes, migration registration, current-scope checks, audit atomicity, replay after correction, and UI correction/retry state. Initial cumulative existing frontend42 tests and production build passed with the integrated component. Go vet passed. Final frontend suite passed 46 tests and the build passed. Astra implemented and passed backend tests for permission/reassignment, actor-scoped retries, concurrent creation, rollback and four unlink/disable lock races; four new frontend tests and the new real-stack Playwright journey also passed. Astra independently reviewed the integrated production code and approved with no blocking findings.

Cumulative real-stack Playwright passed 12/12 in 32.2 seconds. Pending: full cumulative Go/PostgreSQL run, final committed contract drift check, CI, PR, merge and main verification. Skill milestones and lesson-linked notes remain future slices. No checkout/credit policy is inferred here.

Throughput note: progress reads and writes use the existing identity advisory lock to serialize scope changes. This is suitable for the stated school workload; revisit lock granularity if measured contention grows. No large-scale load benchmark is claimed.
