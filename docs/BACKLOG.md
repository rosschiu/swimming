# Web delivery backlog

Active goal: complete web functionality with mock payments, actual AI/voice integrations and reviewed stage PRs. No real payments, native apps, hosting selection or public deployment.

## Gates for every implementation stage

Frontend behaviour tests, Go tests, relevant PostgreSQL integration tests, API contract checks and Playwright journeys against the real application must pass. CI runs the cumulative suite. An independent review agent and the lead review the integrated change. Fix blocking findings before merging; verify merged main before advancing. Store test commands/results and PR links below. Tests using fake AI providers are not evidence of actual voice/model integration.

## Stages

| Stage | Scope and acceptance | Dependencies | Status/evidence |
|---|---|---|---|
| 00 | Record active goal, exclusions, stage gates and open decisions | None | Merged [PR #1](https://github.com/rosschiu/swimming/pull/1), `902016c`; independent review and diff check passed |
| 01 | Runnable Go/Postgres/React foundation; persistent authenticated sessions; scoped parent/coach/admin home; synthetic seed; CI and real-stack Playwright login/permission journeys | 00 | Merged [PR #2](https://github.com/rosschiu/swimming/pull/2), `de7f06a`; local/CI tests and independent review passed |
| 02 | Admin maintains families/students/coaches/venues/class types/sessions; coach roster and parent child selection read shared records; invitations/recovery and student access decision resolved | 01 | 02a merged PR #3 (`6c6d829`); 02b merged [PR #4](https://github.com/rosschiu/swimming/pull/4) (`04fa641`); rosters depend on03 bookings |
| 03 | Configurable packages, ledger, clearly labelled mock payment pending/success/failure/duplicates through shared entitlement services, receipts; flexible and bounded recurring bookings; concurrency/overlap/eligibility checks | 02 and credit decisions | Pending |
| 04 | Attendance/corrections, cancellation cases, change approval/rejection, expiry, carry-forward, scoped coach recovery, substitution and school cancellation; audit all consequences | 03 and timing/recovery decisions | Pending |
| 05 | Progress entry/history, skill milestones, notification history/reminders and email delivery, coach availability, admin action queue, search/filters; complete role journeys and language/accessibility states | 04 | Pending |
| 06 | Provider-backed typed assistant, STT and optional TTS; scoped reads and confirmed mutations; ambiguity/stale confirmation/idempotency/failure tests; actual provider validation | Stable domain tools; provider credentials | Pending |
| 07 | Cumulative end-to-end coverage, operational documentation, import/export needs review, backup/restore proof, permission/security review, representative scale/concurrency checks and final gap closure | 01–06 | Pending |

Stage scope may be split into smaller reviewed PRs when useful, but requirements cannot disappear. Each implementation PR must include a complete useful slice and its tests. Stage 00 is explicitly preparatory, not application delivery.

## Open decisions and external dependencies

- User approved credit reservation/consumption, explicit recurring renewal, administrator-entered package expiry, renewal carry-forward and assigned-coach recovery with reasons/new expiry on 2026-11-17.
- User confirmed separate student login with own schedule/progress access; guardians control booking/payment.
- Timing, eligibility, notifications and other unresolved questions remain in DECISIONS.md; resolve before dependent work.
- Actual language-model and STT/TTS credentials/provider access must be established before claiming stage 06 complete. Do not expose credentials in logs or repository.
- Use synthetic seed/test records only; no external messages or public deployment authorised.

## Evidence log

2026-11-17: Read attached goal and repository documentation; confirmed clean main and Go/Node/Docker/GitHub tools. Started stage/00-delivery-scope. No app existed at baseline. No application tests are claimed for stage 00. Independent scope review approved; lead reviewed scope and git diff --check passed. GitHub reports branch protection unavailable for this private repository plan; CI/review gates remain mandatory before merges.

Stage 00 merged main verified clean at `902016c`. Stage 01 ownership: backend worker services/api, frontend worker apps/web, lead contracts/root tooling/CI/E2E. Separate reviewer runs after integrated checks.

Stage 01 PR: https://github.com/rosschiu/swimming/pull/2. Local frontend8, real-stack Playwright6 and Go/Postgres race tests passed. Independent review approved after session-state and seed fixes; CI pending.

2026-11-17: Stage01 merged main clean, contract check passed, implementation tree matches tested PR head. Stage02 splits into 02a identity/family administration and 02b scheduling. Actual coach rosters and student booked schedules depend on stage03 bookings, avoiding duplicate enrolment data.

2026-11-17: Stage02a merged and main verified clean at `6c6d829`, identical to tested head; contracts check passed. Final frontend18, Playwright7, Go/PostgreSQL race/vet and both CI runs passed; independent review approved. See STAGE-02A-EVIDENCE.md. Stage02b implements venues/class types, bounded recurring sessions, coach schedules and family class catalogue. User approved concurrent same-venue classes and admin-configurable 30-minute different-venue travel gap.

2026-11-17: Stage02b merged PR #4 as `04fa641`. Final local frontend29/build, Go/PostgreSQL race83.303s/vet, cumulative Playwright9 and both CI runs passed. Independent reviewer approved after overnight-display and archived-reference edit fixes. Main verified clean with identical tested implementation tree and contract check. Stage03 split: 03a packages/mock checkout/credits; 03b flexible bookings and rosters; 03c recurring enrolment/renewal. Policy clarifications remain pending before dependent implementation.

Stage02c brings forward coach availability management from stage05 while package rules await answers: coach-owned persistent proposals, admin review and configurable72-hour lock. Publishing availability into classes depends on window-versus-exact-session clarification; not claimed by this slice. See STAGE-02C-EVIDENCE.md.

Stage02c merged [PR #5](https://github.com/rosschiu/swimming/pull/5) as `a11fc76`; frontend37/build, cumulative Playwright10, Go/PostgreSQL race/vet, contracts and both CI runs passed; independent review approved. Main verified clean with identical tested tree. User confirmed availability windows for shorter lessons; stage02d will connect windows to scheduled classes. The user requires gpt-6.2-sol for delegated work; the tool still rejects that model after goal resumption, with no silent substitution. Lead-side source review and acceptance planning are recorded in [STAGE-02D-PLAN.md](STAGE-02D-PLAN.md). User confirmed later window edits/cancellation leave scheduled classes unchanged. Stage02d implementation now includes admin lesson allocation, source history, retry protection and cross-role browser coverage. Independent review remains unavailable; see STAGE-02D-EVIDENCE.md. No stage02d merge or acceptance is claimed.

Stage02d merged [PR #6](https://github.com/rosschiu/swimming/pull/6) as `6e4c142`. Both CI runs, local frontend40/build, Go/PostgreSQL race/vet/contracts and cumulative Playwright11 passed. Independent Sol review approved; merged main verified clean and identical to tested head. User subsequently assigned Sol to development and Astra to review/testing; retrospective Astra audit is running before stage03a implementation.

Stage02e merged [PR #7](https://github.com/rosschiu/swimming/pull/7) as `c085d52`, closing Astra’s availability-edit guidance finding. Sol implemented bilingual copy; Astra added regressions and approved. Frontend42/build and both cumulative CI runs passed; main verified clean, identical to tested head, with contracts check passing. Stage03a planning restored; checkout remains gated on the unanswered expiry decision.

Stage05a progress notes brought forward independently while stage03a checkout expiry remains unanswered. This delivers real coach/admin feedback and scoped guardian/student history without inventing payment or booking policy. See STAGE-05A-PLAN.md. Sol owns backend/UI, Astra independently owns tests/review, lead owns contracts/integration. Milestones, lesson-linked feedback and voice remain required later; no reduction of goal scope.
