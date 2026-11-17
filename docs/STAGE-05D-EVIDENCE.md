# Stage 05d acceptance evidence

Existing role screens and student profiles now have stable URLs. Direct entry, refresh, sign-in and browser Back/Forward preserve the destination. Native links support keyboard navigation and opening another tab. Protected components are gated by role, and profiles use the scoped backend endpoint with generic missing/inaccessible states.

Sol implemented App, navigation helpers, notification links and styles. Astra independently authored tests and reviewed the final production code; approved with no remaining blockers. Lead reviewed the integrated source and tests. Review found an editor cancellation edge: creation is now offered only in the directory, and cancelling profile edits reloads the current scoped record, including after a version conflict.

## Local validation

- Frontend:129 tests passed (7.66s); production build passed. Navigation coverage includes all allowed role/path combinations, denied routes without protected requests, reload/sign-in, malformed paths, retry, native links/focus, access-link precedence, stale responses and replacement sessions.
- Real Go/PostgreSQL/SMTP Playwright:17 tests passed (50.8s), including all14 previous journeys plus3 navigation journeys. Notification fixtures are created through the real backend independently of suite order.
- Generated API contract check, Go vet and git diff --check passed.
- Cumulative Go/PostgreSQL race suite: pending final result.

CI, PR and merge verification will be recorded after completion. No database migration or backend domain change. Package/booking/lesson lifecycle and actual AI/voice remain outstanding; external demo inbox delivery remains unverified. See STAGE-05D-PLAN.md for scope.
