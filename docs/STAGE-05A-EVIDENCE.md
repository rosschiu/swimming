# Stage 05a progress notes: acceptance evidence

Merged [PR #8](https://github.com/rosschiu/swimming/pull/8) as `b88cc3d756c748926080aa5d53a169a585d604a4`, from tested head `e31049fcadca35b03f224183a99672714373c0dc`.

Sol developers implemented backend and profile UI from the lead-owned OpenAPI contract. Migration 6 adds persistent progress notes, actor-scoped creation request IDs and original submitted bodies. Author/current-assignment checks and account checks coordinate with identity changes. Corrections require version and reason, preserve original authorship and record before/after audit history. Families and students receive scoped read access only.

The UI explicitly states shared visibility, supports English/Traditional Chinese, preserves drafts after recoverable failures, and protects creation retries with the original request ID. Uncertain/stale corrections require reloading and explicit review of the current version. Profile components are keyed by account and student so drafts and requests cannot carry across profiles.

Lead source review covered routes, migration registration, current-scope checks, audit atomicity, replay after correction, and UI correction/retry state. Initial cumulative existing frontend42 tests and production build passed with the integrated component. Go vet passed. Final frontend suite passed 46 tests and the build passed. Astra implemented and passed backend tests for permission/reassignment, actor-scoped retries, concurrent creation, rollback and four unlink/disable lock races; four new frontend tests and the new real-stack Playwright journey also passed. Astra independently reviewed the integrated production code and approved with no blocking findings.

Cumulative real-stack Playwright passed 12/12 in 32.2 seconds. Full local Go/PostgreSQL race suite passed in 164.546 seconds; committed contract check passed. Push CI 44153181164 and PR CI 44153231265 passed all cumulative checks. Merged main was clean, its tree identical to the tested head, and contracts:check passed again. Skill milestones and lesson-linked notes remain future slices. No checkout/credit policy is inferred here.

Throughput note: progress reads and writes use the existing identity advisory lock to serialize scope changes. This is suitable for the stated school workload; revisit lock granularity if measured contention grows. No large-scale load benchmark is claimed.
