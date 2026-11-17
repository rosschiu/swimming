# Stage 02a: identity and family administration

Scope: administrator account/invitation management, guardian/coach/student-self links, audit history, invitation activation and password recovery with actual SMTP delivery to local capture. Keeps existing four-role API semantics and data. No scheduling or payment features are claimed in this slice.

Acceptance checks recorded during integration:
- 18 frontend tests and production build pass.
- 7 cumulative Playwright journeys pass against Go/PostgreSQL and SMTP capture; invitation created through UI, delivered link activates account, reset invalidates prior session, UI unlink removes student access immediately.
- Real Auth/Student/Invitation responses validated against OpenAPI in E2E; generated type drift check passes.
- Migration v2 applied to existing foundation seed without replacing records.
- Review identified a concurrent old-password login/session-reset race and revoked-invitation recovery dead end. Implementation fixes added; deterministic concurrent-login/reset regression passes and independent review approved.

Local email uses Mailpit; these tests do not send externally. External demo mail remains authorised but recipient/provider configuration is pending. All links are single-use, tokens are not returned through administrative APIs, and browser token URLs are removed from history after reading.

Final Go/PostgreSQL race suite and vet passed. Both push and pull-request CI suites passed on `30de1cf`. Merged [PR #3](https://github.com/rosschiu/swimming/pull/3) as `6c6d829`; main was clean, generated contract check passed and its tree matched the tested PR head.
