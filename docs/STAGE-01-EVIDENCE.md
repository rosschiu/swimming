# Stage 01 evidence

Scope: persistent sign-in/out and four-role student directory, administrator student create/edit, synthetic seed, PostgreSQL migrations, API contracts and CI. Invitations, relationship management, scheduling and financial workflows remain subsequent stages, not claimed complete here.

Local checks (2026-11-17):
- Go `go test -race ./...` with dedicated PostgreSQL: passed, including scoped access, session lifecycle, concurrent/stale edits and transactional audit rollback.
- Frontend tests: 8 passed, including session-expiry/relogin isolation and preserved failed edits.
- Frontend TypeScript/production build: passed.
- Playwright: 6 passed against actual Go/PostgreSQL, covering invalid login, three read-only roles, denied cross-student reads/writes, administrator persistence/stale edit and mobile Traditional Chinese UI.
- Independent reviewer found retained account state after expiry and missing seed prerequisites. Both were fixed before merge review.

Operational limits: synthetic local accounts only; no deployment. The current login limiter uses direct peer addresses; a deployed proxy must have an explicit trusted-forwarding/rate-limit design. Development-tool dependencies have a reported upstream picomatch advisory; no untrusted glob patterns are passed by the application. Production dependencies audited clean. Do not treat this as a full security assessment.

CI and final review/merge links are recorded in BACKLOG.md when available.
