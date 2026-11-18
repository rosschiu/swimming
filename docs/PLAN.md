# Holistic development plan

## Current execution scope

The active goal delivers the complete web application with mock payments only. BACKLOG.md is the execution sequence and gate record. The longer-term roadmap below retains real payment integration and native clients for context, but those are explicitly excluded from current completion. The user deferred AI/STT/TTS and provider selection beyond the first delivery on 2026-11-18. They remain in the later product plan; fixtures will not constitute a working integration.

## Outcome and completion boundaries

Deliver a dependable school operations product: a verified payment creates the correct entitlement, booking reserves a place and credit, the coach sees the correct roster, attendance resolves the entitlement, and changes/cancellations update all affected records together. Parents can understand their balance; administrators can explain and correct it.

Distinguish these milestones:

1. Web pilot: core operations, manual payment verification and initial typed/voice assistance.
2. Web production release: integrated payments, operational readiness and validated assistant actions.
3. Full end game: web plus iOS, Android and WeChat, with agreed feature coverage and one authoritative backend.

Finishing the web pilot does not complete integrated payments or the multi-platform objective. Estimates and calendar commitments follow policy resolution and provider evaluation; none are implied here.

## Architecture

Use a modular Go application, not microservices. React/Vite consumes a REST API described by OpenAPI. PostgreSQL holds authoritative records. Background jobs handle bounded recurring-session generation, expiry/carry-forward processing, notifications and reconciliation. Add file storage only for required attachments. No hosting/vendor choice is made here.

Suggested repository boundaries, created only when needed:

| Path | Responsibility |
|---|---|
| apps/web | React application, accessible screens, typed/voice assistant |
| services/api | Go HTTP endpoints, application/domain services, database access and jobs |
| contracts | OpenAPI and generated-contract configuration |
| packages | Only genuinely reusable client resources: translations, tokens or platform-neutral utilities |
| docs | Requirements, decisions, acceptance evidence and operating guidance |

Do not scaffold empty mobile applications. Avoid premature wrappers around every dependency. Establish a provider boundary for AI, speech and payments where an actual integration needs it.

### Application domains

- Identity: staff access, families, guardians, student links and coach assignment.
- Scheduling: venues, class types, series, sessions, capacity and substitutions.
- Entitlements: package definitions, purchased packages, versioned policies and credit movements.
- Booking: enrolments, session bookings, reservations, cancellations and change requests.
- Teaching: attendance, progress notes and skill milestones.
- Finance: charges, payment attempts/verification, receipts, refunds and adjustments.
- Communication: notifications, delivery attempts and in-app history.
- Assistant: conversation context, tool proposals, confirmations and execution outcomes.
- Audit: durable actor/action/reason/history for consequential changes.

### Data invariants

- A recurring series is distinct from each session; changing one lesson must not silently change the entire series.
- Capacity and credit checks execute transactionally with booking writes. Concurrent callers cannot reserve the same final place or credit.
- No negative usable allowance or duplicate entitlement grant. Repeated confirmations, jobs and provider events are idempotent.
- Available, reserved, consumed and expired credits remain distinguishable and traceable. Corrections use recorded adjustments.
- A change approval atomically checks the target, moves the booking and handles the reservation. Failure preserves the original booking.
- Credit and payment history are related but distinct; restoring a credit does not imply a cash refund.
- Attendance outcomes drive the agreed policy; elapsed clock time alone does not prove attendance.
- Use explicit Hong Kong scheduling semantics and unambiguous timestamps. Deadline tests cover exact boundaries.
- Every client request and AI tool execution rechecks identity, scope and policy on the server.

## User experience

Parent home prioritises the next class, actions needed and understandable credit balances. Support multiple children, request tracking, package/payment history and clear cancellation consequences.

Coach home prioritises assigned classes and rosters. Support quick attendance, request decisions, dictated feedback and scoped credit recovery. Do not require chat for basic work.

Administrator home prioritises pending payments/requests, missing attendance, disrupted classes and scheduling issues. Provide a desktop-appropriate workspace for record maintenance and audit review.

Show loading, empty, failed-save, stale-data and retry states. Explain disabled actions. Preserve entered data after recoverable failures. Support keyboard use, screen readers and consistent language switching. Spoken responses are optional; visible text remains available.

## AI and voice design

Treat AI as an additional interaction layer over tested operations:

1. Accept typed input or explicit microphone input; show/edit the speech transcript.
2. Resolve the signed-in user's authorised student/class context.
3. Retrieve current facts through scoped read tools.
4. Ask focused clarification for ambiguity.
5. Prepare a structured action proposal with exact identifiers, consequences and an expiry/version marker.
6. Confirm the proposal through a clear UI action; initially use an explicit button for consequential changes.
7. Revalidate permissions, policy and current data, then execute idempotently.
8. Return the actual execution result and an optional spoken explanation.

The model must not mark an operation successful before the backend confirms it. A changed proposal needs fresh confirmation. Never trust client/model-provided roles or balances. Treat retrieved text and student notes as data, not instructions. Provider failure falls back to normal screens.

Start with next-class/credit questions, available-class searches, navigation and feedback drafts. Add booking and attendance actions after core service tests pass; add recovery and other sensitive actions later.

Evaluate speech/language providers on task completion, names, Cantonese/English code-switching, pool noise, ambiguity handling, latency and cost. Use consented or synthetic evaluation data. Establish measurable acceptance thresholds before pilot selection, rather than inventing targets without evidence. Minimise model context and avoid raw-audio retention by default; decide transcript retention explicitly.

## Delivery phases and gates

| Phase | Work | Exit evidence |
|---|---|---|
| 0: Specification | Resolve blocking policy questions; role/permission matrix; revised journeys/mockup; schema and API proposal; representative test cases | Approved behaviour for booking, attendance, expiry, carry-forward and recovery; no critical ambiguity in first slice |
| 1: Foundation | Go/React projects, migrations, authentication/account linking, seeded synthetic data, student/coach/venue records, API contract and checks | Parent sees only linked children; coach sees assigned records; usable vertical slice through UI/API/database |
| 2: Entitlement and booking | Packages, manual verified payments, ledger, sessions, bounded recurring enrolment and flexible booking | Verified payment grants credits once; booking appears across roles; simultaneous last-place bookings remain safe |
| 3: Lesson lifecycle | Attendance, cancellation, change approval, expiry/carry-forward and scoped recovery | All approved policy cases reconcile; retries/corrections are traceable; failed swaps preserve original |
| 4: Communication and AI pilot | Progress, notification history/delivery, typed assistant, STT/TTS, read tools and selected confirmed actions | Representative users complete tasks; ambiguity and provider failures handled; no unauthorised/unconfirmed writes |
| 5: Operational pilot | Representative real workflows, controlled data import, training, support procedure, observability, backup/restore and accessibility/device checks | Staff reconcile balances; tested recovery; issues triaged; one agreed system of record |
| 6: Integrated payments | Provider selection/spike, verified callbacks/reconciliation, receipts, refunds and failure handling | Duplicate/out-of-order events safe; payment-to-credit reconciliation passes; manual exception path retained |
| 7: Web release | End-to-end regression, permission review, load/concurrency checks at expected usage, runbooks and pilot fixes | Web acceptance checklist complete; integrated payments operational |
| 8: Additional platforms | Expo mobile apps, Taro WeChat client, identity linking and platform payment/notification adaptations | Platform-specific acceptance/distribution requirements met; shared backend and policies unchanged |

AI design begins in phase 0; provider exploration can run alongside independent core work. Payment feasibility should be checked early enough to avoid a late blocker, even though production integration is a later phase. Phase 8 remains future scope unless included in the user's development goal.

## Validation strategy

- Go domain tests for credit transitions, timing, policy precedence and recovery.
- PostgreSQL integration tests for capacity/credit contention, rollback, idempotency and migration integrity.
- API permission tests for families, coaches and administrators; generated-client contract checks.
- Browser journey tests for purchase/verify/book/attend, recurring enrolment, change/cancel and recovery.
- AI tool tests for ambiguous names, prompt-injection attempts, missing context, expired confirmation, stale capacity, repeated requests and provider failure.
- Speech evaluation using realistic language/noise cases; test mobile microphone permissions and interrupted recordings.
- Payment tests for callback authenticity, duplicates, out-of-order events and refunds.
- Accessibility and real-device checks for critical screens; no claim of compliance based only on automated checks.
- Operational checks: job retries, notification failures, reconciliation and actual restore verification.

Keep tests proportional to risk; do not add tests that merely restate trivial UI code. Record what ran and any unresolved failures at each gate.

## Release and risk controls

Use synthetic data in development. Before real data, agree access, retention, audit access and attachment handling. Pilot with named staff and a limited group; define the source of truth and reconciliation procedure to prevent conflicting parallel records.

Operational measures should include booking failures, pending payment age, unmarked attendance, notification failures, outstanding credits and assistant task failures. Define alert thresholds from pilot observations.

Main risks: unclear policy interactions, capacity mismatch, concurrent bookings, manual financial mistakes, noisy speech/name ambiguity, overly broad coach permissions and overpromised cross-platform UI reuse. Each has a corresponding specification or validation gate above.

Hosting, deployment targets, external publication and real payment activation are not authorised by this planning task. The later goal must establish its delivery boundary.
