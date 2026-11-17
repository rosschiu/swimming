# Decisions and requirements

Recorded 2026-11-17. This document preserves the discussion; proposals are not silently promoted to approved business rules.

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

The user has not yet specified expiry duration, carry-forward mechanics, recovery limits, recurrence renewal or all timing rules.

### Technology direction

- Go backend, replacing the proposed NestJS backend. NestJS is rejected.
- PostgreSQL database.
- React + Vite + TypeScript web client.
- REST + OpenAPI for shared client contracts.
- One repository with distinct client and backend ownership.
- Future mobile direction: Expo + React Native.
- Future WeChat direction: Taro + React, with compatibility validated before that phase.
- No promise of automatic UI conversion. Reuse the backend, contracts, translations, design tokens and platform-independent client logic. Go domain logic stays on the server; it is not a shared TypeScript package.
- Exact Go routing/database libraries, authentication implementation and provider choices remain unselected.

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

## Proposed rules requiring confirmation

- Booking reserves a credit; attended lessons consume it. Retained credits are released from the booking and remain subject to validity rules.
- Keep the original booking during a pending change. Do not reserve a destination place until approval; recheck capacity then.
- Generate recurring bookings for a bounded enrolment period, rather than indefinitely.
- Policies default at school level, with package overrides. Store the applicable policy version on relevant records; changes apply prospectively unless an authorised correction is made.
- Expired credits remain in history; recovery creates an adjustment with quantity, reason, actor and new expiry.
- Coaches recover credits only for students in their authorised scope. Exact scope and financial limits need agreement.
- School cancellation returns the entitlement and explicitly handles any validity problem.
- Normal administration is web-first; the exact mobile/WeChat feature matrix remains to be agreed.

## Questions to resolve before dependent implementation

1. Fixed enrolment: automatic renewal or explicit parent renewal? Can unpaid future sessions be provisionally held, and until when?
2. Package validity: duration and start event (purchase, first booking, first attendance or term date)?
3. Carry-forward: automatic or conditional on renewal, maximum quantity, destination package and new validity? What happens without renewal?
4. Recovery: allowed quantities, expiry extension, authorised coach/student relationship and whether any cases require administrator review?
5. Timing rules: retain or replace the mockup's 24-hour parent cutoff, 72-hour availability restriction, attendance opening after 15 minutes and three-day correction window?
6. Class eligibility: age, skill level, class type, venue restrictions, capacity and coach travel/conflict rules?
7. Are 500 students registered or active weekly? Confirm capacity planning.
8. Payments: methods, verification roles, cash collection permissions, partial payment/refund rules and provider account availability?
9. Language: Traditional Chinese and English as proposed; Simplified Chinese requirement? Supported voice languages and acceptable recognition quality?
10. Connectivity: online-only initially or an actual offline attendance requirement?
11. Notifications: required delivery channels and escalation when delivery fails?

Ask questions at the relevant phase; continue independent work. Do not infer consent or policy answers from silence.

## Existing mockup assessment

The workspace initially contained only a single standalone HTML mockup. It includes role switching, bilingual UI, parent schedule/booking, sample payments, coach attendance/availability, administrator schedules/regions/student records and request history. Data resets on refresh.

Known source-level gaps: parent requests do not enter the coach's actual approval queue; approvals do not move actual bookings; attendance and balances are disconnected; next-term allowance is unenforced; timing rules are not enforced; coach rosters are fixed; availability does not produce bookable classes; progress is static; digital payments are simulated. No backend, real authentication, persistent storage or operational payment integration exists in the supplied workspace.

The initial assessment inspected source, not browser/device behaviour. It was not a security audit. The mockup is a reference, not the authoritative specification.
