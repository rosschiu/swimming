# Decisions and requirements

Recorded 2026-11-17. This document preserves the discussion; proposals are not silently promoted to approved business rules.

## Active development goal

The user has authorised staged implementation and reviewed PR merges. Deliver all agreed web features for coach, student/parent and administrator, with AI/voice deferred from the first delivery by the user on 2026-11-18. Payments are mock-only in this goal: pending, success, failure and duplicate outcomes must exercise persistent entitlement/receipt services. Real payment integration remains future end-game scope. Native clients, hosting selection and public deployment are excluded. See BACKLOG.md for acceptance gates.

## Confirmed direction

Build one reliable operating system for one swimming school with multiple venues and coaches. Parents manage lessons and payments, coaches run classes and record progress, administrators handle operations and exceptions. One action must update all affected records consistently, without manual reconciliation across role-specific screens.

Expected initial scale: 10 coaches, 500 students, 20 venues and 50 classes per week. Whether all 500 students attend weekly is unresolved. At the mockup's six-student capacity, 50 classes supply only 300 weekly places; six is a sample limit, not an approved universal capacity.

Web is first. The eventual product also includes iOS, Android and a WeChat mini program. Choose technology with future reuse in mind, but do not implement those clients now. Hosting is explicitly excluded from current selection/planning work.

### Booking and credit requirements

| Topic | User-confirmed requirement |
|---|---|
| Enrolment | Support both fixed weekly enrolment and flexible package booking |
| Early cancellation, late cancellation, absence, illness | Behaviour configurable per case; default retain the credit |
| Expiry | Enabled by default; configurable |
| Carry-forward | Allowed by default; configurable |
| Recovery | Coach can manually recover credits for a parent |
| Payments | Manual verification today; fully integrated before the end game is complete |

Later approvals below establish administrator-entered expiry, renewal carry-forward, explicit renewal and configurable timing defaults. Recovery limits and the detailed interaction with existing reservations remain unresolved.

### Technology direction

- Go backend, replacing the proposed NestJS backend. NestJS is rejected.
- PostgreSQL database.
- React + Vite + TypeScript web client.
- REST + OpenAPI for shared client contracts.
- One repository with distinct client and backend ownership.
- Future mobile direction: Expo + React Native.
- Future WeChat direction: Taro + React, with compatibility validated before that phase.
- No promise of automatic UI conversion. Reuse the backend, contracts, translations, design tokens and platform-independent client logic. Go domain logic stays on the server; it is not a shared TypeScript package.
- The implemented backend uses Go's standard HTTP router and pgx/PostgreSQL, with database-backed cookie sessions. AI/speech provider selection remains unresolved.

### AI-native requirement

The product must support natural-language interaction by typing and voice, particularly for parents and coaches who are less comfortable with software. STT and TTS are core planned capabilities, not an unrelated chatbot add-on.

Proposed implementation direction from the discussion:

- Typed and spoken requests converge on the same assistant workflow.
- AI calls explicit tools backed by the same Go application services as normal screens.
- No direct model database writes; permissions and policies remain server-enforced.
- Read tasks can execute immediately. Mutations show a concrete preview and require confirmation.
- Resolve ambiguous student names, dates and classes before mutation.
- Begin with push-to-talk, editable transcripts and optional spoken replies.
- Keep normal controls available; AI failure must not block routine school operations.
- Evaluate Cantonese, English, mixed speech, student names and poolside noise.
- Provider selection and real-time conversational voice are deferred.

## Scope carried forward from the discussion

The first operational product covers accounts and family linking; student and staff records; venues and recurring sessions; packages and credits; both booking models; attendance; change requests; cancellation and make-up handling; manual payment reconciliation; receipts; progress notes; notifications; administrator exceptions and audit history.

Future end-game scope additionally includes integrated digital payments and the three additional client platforms. AI capabilities are introduced in stages but designed into the foundation.

Advanced marketing, marketplace features, payroll, AI-generated coaching programmes and a large teaching library are not first-release requirements. Offline attendance is undecided, not promised.

## Approved starting rules (2026-11-17)

The user approved: booking reserves a credit; attendance consumes it; fixed enrolment requires explicit renewal; package expiry is an administrator-entered date; carry-forward transfers unused credits on renewal to the new expiry; assigned coaches recover credits with a reason and new expiry. Remaining details below are still proposals where not covered by this approval.

Students have separate login access to their own schedule and progress; guardians control bookings and payments (confirmed 2026-11-17).

User also confirmed configurable timing defaults: parent change requests at least 24 hours ahead; coach availability locked within 72 hours; attendance opens 15 minutes after session start; attendance corrections allowed for three days.

The user requires an external notification delivery channel in addition to in-app notifications. Email is implemented with local mail capture, durable teaching notification delivery and administrator retry. The user explicitly authorised demo email notifications. External demo recipient addresses and SMTP/provider access are pending; no unrelated real-school recipients are authorised. Local capture verifies SMTP delivery, not receipt by an external inbox.

Coach scheduling requires a 30-minute gap between classes at different venues by default, configurable by administrators. Multiple classes may run at the same venue concurrently; administrators manage venue/lane limits for this web release (confirmed 2026-11-17).

## Additional rule details

- Booking reserves a credit; attended lessons consume it. Retained credits are released from the booking and remain subject to validity rules.
- Keep the original booking during a pending change. Do not reserve a destination place until approval; recheck capacity then.
- Generate recurring bookings for a bounded enrolment period, rather than indefinitely.
- Policies default at school level, with package overrides. Store the applicable policy version on relevant records; changes apply prospectively unless an authorised correction is made.
- Expired credits remain in history; recovery creates an adjustment with quantity, reason, actor and new expiry.
- Coaches recover credits only for students in their authorised scope. Exact scope and financial limits need agreement.
- School cancellation returns the entitlement and explicitly handles any validity problem.
- Normal administration is web-first; the exact mobile/WeChat feature matrix remains to be agreed.

## Questions to resolve before dependent implementation

1. Fixed enrolment: explicit renewal is approved. Confirm all-or-nothing allocation and whether unpaid future sessions may ever be provisionally held.
2. Package validity: administrator-entered expiry is approved. The user confirmed individually entered dates per purchase on 2026-11-18. Whether validity must cover the lesson end remains unresolved for booking.
3. Carry-forward: transfer unused credits on renewal to the new expiry is approved. Confirm treatment of reserved credits, matching package eligibility and any quantity limits.
4. Recovery: assigned coaches, a reason and a new expiry are approved. Confirm whether recovery is limited to actual expired unused credits or needs administrator approval in some cases.
5. Timing defaults are approved and admin-configurable:24-hour parent cutoff,72-hour availability restriction, attendance opening15 minutes after start and three-day corrections. No new timing decision is required before implementing those defaults.
6. Class eligibility: confirm enforced swimming-level eligibility versus guidance, and any package/class-type restrictions. Coach travel defaults30 minutes, configurable; concurrent classes at a venue are allowed with venue/lane limits managed by administrators.
7. Are 500 students registered or active weekly? Confirm capacity planning.
8. Payments: this goal is mock-only. Real provider selection and actual transactions are excluded. Any partial refund or financial exception policy must be agreed before implementing it.
9. Language: Traditional Chinese and English as proposed; Simplified Chinese requirement? Supported voice languages and acceptable recognition quality?
10. Connectivity: online-only initially or an actual offline attendance requirement?
11. Notifications: in-app plus email is approved; external demo delivery needs configured SMTP access and named recipients. Booking reminders depend on bookings/lifecycle implementation.

Ask questions at the relevant phase; continue independent work. Do not infer consent or policy answers from silence.

## Existing mockup assessment

The workspace initially contained only a single standalone HTML mockup. It includes role switching, bilingual UI, parent schedule/booking, sample payments, coach attendance/availability, administrator schedules/regions/student records and request history. Data resets on refresh.

Known source-level gaps: parent requests do not enter the coach's actual approval queue; approvals do not move actual bookings; attendance and balances are disconnected; next-term allowance is unenforced; timing rules are not enforced; coach rosters are fixed; availability does not produce bookable classes; progress is static; digital payments are simulated. No backend, real authentication, persistent storage or operational payment integration exists in the supplied workspace.

The initial assessment inspected source, not browser/device behaviour. It was not a security audit. The mockup is a reference, not the authoritative specification.

## Availability windows (confirmed 2026-11-17)

Coach availability represents a window in which administrators may schedule shorter lessons, not an exact proposed class. One window may therefore support multiple lessons. Availability management is implemented in stage02c; linking shorter scheduled classes is the next slice.

The user also confirmed that later availability edits or cancellation leave scheduled classes unchanged. Administrators manage changes to scheduled classes separately. Source-window links record how a class was originally scheduled; they do not constrain subsequent class corrections.

## First-delivery scope update (2026-11-18)

The user selected individual expiry per purchase; the previously approved administrator-entered date still applies. Package offers do not supply a shared expiry date. AI/voice and provider selection are deferred beyond the first delivery, so provider credentials are not a gate for that delivery. Full coach, guardian/student and administrator web workflows, mock payments and staged reviewed/tested PRs remain required. AI remains part of the later product plan.
