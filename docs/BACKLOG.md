# Web delivery backlog

Active goal: complete web functionality with mock payments and reviewed stage PRs. AI/voice is deferred beyond first delivery (user decision, 2026-11-18). Real payments and native clients remain outside first delivery. The user has added automatic deployment to a designated VM; target access and release trigger are pending.

## Gates for every implementation stage

Frontend behaviour tests, Go tests, relevant PostgreSQL integration tests, API contract checks and Playwright journeys against the real application must pass. CI runs the cumulative suite. An independent review agent and the lead review the integrated change. Fix blocking findings before merging; verify merged main before advancing. Store test commands/results and PR links below. Tests using fake AI providers are not evidence of actual voice/model integration.

## Stages

| Stage | Scope and acceptance | Dependencies | Status/evidence |
|---|---|---|---|
| 00 | Record active goal, exclusions, stage gates and open decisions | None | Merged [PR #1](https://github.com/rosschiu/swimming/pull/1), `902016c`; independent review and diff check passed |
| 01 | Runnable Go/Postgres/React foundation; persistent authenticated sessions; scoped parent/coach/admin home; synthetic seed; CI and real-stack Playwright login/permission journeys | 00 | Merged [PR #2](https://github.com/rosschiu/swimming/pull/2), `de7f06a`; local/CI tests and independent review passed |
| 02 | Admin maintains families/students/coaches/venues/class types/sessions; coach roster and parent child selection read shared records; invitations/recovery and student access decision resolved | 01 | Merged PRs #3–7 cover records, scheduling, coach availability and publishing lessons from availability. Shared rosters shipped in [#17](https://github.com/rosschiu/swimming/pull/17). |
| 03 | Configurable packages, ledger, clearly labelled mock payment pending/success/failure/duplicates through shared entitlement services, receipts; flexible and bounded recurring bookings; concurrency/overlap/eligibility checks | 02 and credit decisions | Mock checkout and credit issuance merged in #15. Flexible booking and school-wide credit validity merged in #17. Fixed enrolment and explicit renewal merged in [#21](https://github.com/rosschiu/swimming/pull/21). Both alternatives are administrator-configurable school-wide. |
| 04 | Attendance/corrections, cancellation cases, change approval/rejection, expiry, carry-forward, scoped coach recovery, substitution and school cancellation; audit all consequences | 03 and timing/recovery decisions | Expiry and coach recovery merged in #16. Timing settings merged in #18. Marking attendance merged in [#22](https://github.com/rosschiu/swimming/pull/22). Guardian change requests and coach decisions are in local integration. Corrections and cancellations remain outstanding. |
| 05 | Progress entry/history, skill milestones, notification history/reminders and email delivery, coach availability, admin action queue, search/filters; complete role journeys and language/accessibility states | 04 | Progress, milestones, teaching notifications and opt-in email, and role navigation merged in #8–10 and #14. The admin action queue merged in #20 and in-app family booking confirmations in [#19](https://github.com/rosschiu/swimming/pull/19). Scheduled reminders, external demo inbox verification and remaining role journeys are outstanding. |
| 06 | Provider-backed typed assistant, STT and optional TTS; scoped reads and confirmed mutations; ambiguity/stale confirmation/idempotency/failure tests; actual provider validation | Stable domain tools; provider credentials | Deferred beyond first delivery by user |
| 07 | Cumulative end-to-end coverage, operational documentation, import/export needs review, backup/restore proof, permission/security review, representative scale/concurrency checks and final gap closure | First-delivery stages01–05 | Backup/restore, representative-scale checks and administrator revocation checks merged in #11–13. Final cross-role acceptance and operating documentation remain open. |

Stage scope may be split into smaller reviewed PRs when useful, but requirements cannot disappear. Each implementation PR must include a complete useful slice and its tests. Stage 00 is explicitly preparatory, not application delivery.

## Workflow breakdown

Update these rows in the stage PR when scope, evidence or status changes. Record the merge commit and verification in the evidence log after each release. A passing test on a draft branch counts as review evidence. Mark a workflow shipped once verified on main.

| Workflow | Delivery state | Acceptance or next check |
|---|---|---|
| Account access and family links | Shipped, #2–3 | Guardians manage linked students; a student signs in separately to their own records. |
| Timetable and availability | Shipped, #4–7 | Administrators publish lessons within coach windows, apply travel gaps and preserve scheduled lessons when a window changes. |
| Mock purchase and credit issuance | Shipped, #15 | Pending, failed, successful and duplicate outcomes reconcile to the purchase, receipt and ledger. |
| Flexible booking and rosters | Shipped, #17 | Reserve one place and credit. The student schedule and assigned coach roster show the same booking. |
| Fixed weekly selection | Merged, [#21](https://github.com/rosschiu/swimming/pull/21) | Default books all selected dates together. Stale input or insufficient credit leaves every date unchanged. |
| Partial weekly selection | Merged, #21 | An administrator can allow partial booking school-wide. Show the exact available dates and require a fresh confirmation before reserving any. |
| Explicit renewal and carry-forward | Merged, #21 | Confirm next dates and exact paid package. Default to the same package type. An administrator can allow another type for the same student. Transfer unused credits when enabled; preserve existing reservations and their expiry. |
| Mark present | Under review, [#22](https://github.com/rosschiu/swimming/pull/22) | Coach marks attendance once; a retry consumes no extra credit and the family sees the outcome. Integration tests cover originally reserved and renewed lessons. |
| Absences and attendance corrections | Open | Apply the configured outcome and correction window, reconcile credit movements and show the audit trail. |
| Parent change requests | Local integration | Keep the original reservation while pending. Approval moves it once; rejection or a failed swap preserves it. Dedicated request and decision notifications remain open. |
| Family cancellation and make-up | Open | Apply the configured timing and credit policy and show the resulting entitlement to the family. |
| School cancellation and substitution | Open | Reconcile affected bookings, roster access, credits and family communications. |
| Credit expiry and recovery | Shipped, #16 | An assigned coach restores actual expired unused credits with a reason and new expiry. |
| Progress, milestones and teaching email | Shipped, #8–10 | Family sees scoped feedback; opt-in teaching emails have delivery history and retries. External demo inbox delivery still needs verification. |
| Booking confirmation and reminders | Confirmations shipped, #19; reminders open | In-app confirmations are deduplicated. Verify scheduled reminders and delivery preferences separately. |
| Administration and navigation | Shipped in part, #14 and #20 | Role routes, pending purchase and email queues are live. Request decisions and missing attendance still need cross-role acceptance. |
| Operational readiness | Shipped in part, #11–13 | Restore, scale and permission checks exist. Repeat on the final workflows, verify setup/seed instructions, and close remaining accessibility and operational gaps. |
| Typed assistant and voice | Deferred beyond first delivery | Verify authorized queries, confirmed actions, STT and optional TTS against real providers before claiming delivery. |

## VM delivery

| Work item | State | Acceptance |
|---|---|---|
| Identify the demo target | Awaiting VM connection details | Inspect its OS, existing ingress, deployment directory and persistent storage. Confirm the app URL and whether it is private. |
| Package a tested revision | Planned | Build the web assets and Go services from the reviewed commit. Record the commit in the release artifact. |
| Automate rollout | Planned | Deploy only the selected release after CI passes. Serialize deployments, run migrations once and retain application data. |
| Verify and recover | Planned | Check the running version, API health and web routing from the target. Exercise recovery to the previous compatible release on the demo VM. |

## Open decisions and external dependencies

- User approved credit reservation/consumption, explicit recurring renewal, administrator-entered package expiry, renewal carry-forward and assigned-coach recovery with reasons/new expiry on 2026-11-17.
- User confirmed separate student login with own schedule/progress access; guardians control booking/payment.
- Timing, eligibility, notifications and other unresolved questions remain in DECISIONS.md; resolve before dependent work.
- AI/voice is deferred from first delivery. Actual language-model and STT/TTS credentials/provider access must be established before claiming that later stage complete. Do not expose credentials in logs or repository.
- Use synthetic demo records. Target access, public routing and email recipients require the intended VM and demo configuration.

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

Stage05a merged [PR #8](https://github.com/rosschiu/swimming/pull/8) as `b88cc3d`. Local frontend46/build, Go/PostgreSQL race/vet, contracts, Playwright12 and both cumulative CI runs passed; Astra independently approved. Main verified clean with identical tested tree and passing contract check. Stage05b adds an administrator-defined skill catalogue and scoped milestone history; see STAGE-05B-PLAN.md.

Stage05b merged [PR #9](https://github.com/rosschiu/swimming/pull/9) as `48a2c9b`. Sol implemented, Astra tested and independently approved after a React sibling-key correction. Final frontend54/build, Go/PostgreSQL race/vet, contracts, Playwright13 and both CI runs passed; main verified clean and identical to tested head. Stage05c adds teaching notifications and durable email delivery. Booking reminders still depend on stage03/04; real external demo SMTP/recipients remain pending.

Stage05c merged [PR #10](https://github.com/rosschiu/swimming/pull/10) as `9c0a14d`: scoped teaching notifications and durable opt-in email delivery/retries. Sol implemented; Astra independently tested and approved. Both cumulative CI runs passed; frontend62/build, Playwright14, Go/PostgreSQL race and contracts passed. Main verified clean and identical to tested head. External demo inbox delivery remains unverified; booking reminders depend on03/04.

Stage07a brings forward actual synthetic backup/restore verification while package expiry and AI-provider decisions remain unanswered. See STAGE-07A-PLAN.md. This does not replace outstanding web features or complete stage07.

Stage07a merged [PR #11](https://github.com/rosschiu/swimming/pull/11) as `6958079`. Actual synthetic restore/corruption/cleanup/permission checks and cumulative backend/frontend/Playwright/contracts passed locally and in both CI runs; Astra approved. Main verified clean and identical to tested head. Stage07b adds representative current-feature scale checks; see STAGE-07B-PLAN.md. Booking/payment/lesson lifecycle/AI requirements remain outstanding.

Stage07b merged [PR #12](https://github.com/rosschiu/swimming/pull/12) as `c70ab02`: representative500-student/10-coach/20-venue/50-weekly-class correctness and contention checks. Stage07c merged [PR #13](https://github.com/rosschiu/swimming/pull/13) as `7c028a4`: closes the cumulative audit's administrator-revocation race across twelve mutations and retry replay. Sol implemented; Astra independently reproduced the original failures, tested and approved. Both stages passed cumulative local/CI gates and merged-main tree/contract checks. See STAGE-07B-EVIDENCE.md and STAGE-07C-EVIDENCE.md.

Stage05d makes existing role screens and student progress profiles bookmarkable, refreshable and usable through browser history. Sol owns implementation; Astra owns independent tests/review. See STAGE-05D-PLAN.md. This is independent of the still-unanswered package expiry placement and AI-provider decisions; booking, mock finance, lesson lifecycle and actual AI/voice remain required.

Stage05d merged [PR #14](https://github.com/rosschiu/swimming/pull/14) as `2d1fca1`. Sol implemented; Astra approved independently. Frontend129/build, Playwright17, Go/PostgreSQL race390.116s, contracts/vet and both CI runs passed. Clean main matched the tested tree. On2026-11-18 the user chose individual purchase expiry and deferred AI/voice from first delivery. Stage03a design now proceeds on that basis.

Stage03a merged [PR #15](https://github.com/rosschiu/swimming/pull/15) as `0f8e029`: individual expiry, offers/policies, mock attempts/retry/replay, immutable receipts and credits. Sol implemented; Astra approved. Full local/CI checks passed and merged main matched the tested tree. Stage03b adds flexible bookings and real role-specific lesson lists/rosters; user approved level enforcement with administrator override and configurable coach/admin booking/renewal defaults. See DECISIONS.md for exact approvals and remaining configuration questions.

Stage04a brings forward durable credit expiry and approved assigned-coach recovery while03b configuration alternatives await answers. See STAGE-04A-PLAN.md. It delivers actual expired-unused credit adjustments without guessing booking/renewal policy.

Stage 04a merged in [PR #16](https://github.com/rosschiu/swimming/pull/16) at `18eab4e`. Both complete CI runs passed. Clean main matched the tested head and passed contract regeneration. Assigned coaches can now restore actual expired unused credits with a reason and a new expiry date. Stage 03b builds flexible bookings on the same ledger.

Stage 05f merged in [PR #20](https://github.com/rosschiu/swimming/pull/20) at `7df170f`. Administrators can open pending purchases and failed email deliveries from the action queue. Both CI runs passed. The merged tree matched the reviewed commit and contract regeneration passed.

Stage 03b merged in [PR #17](https://github.com/rosschiu/swimming/pull/17) at `8447350`. Flexible bookings reserve seats and credits atomically. Administrators control the school-wide validity rule. Both CI runs, the full Go suite, 175 frontend tests and 25 Chromium journeys passed. The merged tree matched the reviewed commit and contract regeneration passed.

Stage 04 timing settings merged in [PR #18](https://github.com/rosschiu/swimming/pull/18) at `2595ec4`. The full PostgreSQL Go race suite, 179 frontend tests and both CI runs passed. Independent upgrade and Chromium tests verified that an existing school's booking-validity setting survives migration and that stale timing edits require a reload. The merged tree matched the reviewed commit and passed contract regeneration.

Stage 05e merged in [PR #19](https://github.com/rosschiu/swimming/pull/19) at `17a83e2`. Family booking notifications, reservations and credit movements commit together. Upgrade testing preserved existing data and verified replay and revoked access. Both CI runs, the PostgreSQL-backed Go suite, 181 frontend tests and 29 Chromium journeys passed. The merged tree matched the reviewed files and passed contract regeneration.

Stage03c merged [PR #21](https://github.com/rosschiu/swimming/pull/21) as `227720d`. PostgreSQL race tests covered fixed enrolment, partial allocation, renewal, expiry and credit recovery. Chromium exercised confirmation and next-term renewal. Attendance PR #22 integrates that merge while preserving migration 15 for attendance and migrations 16 and 17 for school settings.
