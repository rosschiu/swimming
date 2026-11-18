# Application review and first-release gap analysis

Reviewed 2026-11-18 against main `638c991` (code) and the live demo at `f6232c3` (UI, phone viewport 390×844, all four demo roles, read-only). PR #38 (cancellation impact preview) is unmerged; its hosted checks were cancelled at the owner's request and must be rerun before merge.

## Verdict

The backend is in good shape: transactional, idempotent, audited and well tested (32 Go test files with PostgreSQL race tests, 21 Playwright specs, scale and restore checks). Roughly half of the first-release lifecycle work remains, concentrated in the parts that need business decisions: what happens when a lesson does *not* go ahead as booked.

The web client is the weak point. It is a desktop admin console squeezed onto a phone, organised around records and filters instead of the next thing each person needs to do. It needs a redesign of the shell and the role home screens, not more polish on the current pages. The client architecture also needs restructuring before more screens are added, or each new workflow will make it harder.

## UI review

### What a phone user actually sees

| Measure | Observation |
|---|---|
| Content start | The sidebar stacks above every page as wrapped pill links. Page content starts 265px (student), 317px (guardian/coach) and 421px (admin) down an 844px screen. |
| Horizontal scroll | Student profile and packages pages render 619–647px wide on a 390px screen. The cause is a raw 64-character package ID and unconstrained forms. |
| Page length | Admin accounts 6,366px; admin setup 4,052px with eight settings forms; guardian child profile 2,681px holding bookings, enrolment, renewal, credits, milestones and notes on one page. |
| Landing page | Every role lands on a **student directory**. A guardian with one child sees a list of one; a student sees a "directory" containing themselves; a coach sees students rather than today's classes. |
| Startup | "Checking your session…" blank screen on every load. |

### Main usability problems

1. **No home screen per role.** The plan says the guardian home prioritises the next class, actions needed and credit balance; the coach home prioritises assigned classes and rosters. Neither exists. A guardian needs three taps and a date-range form to see the next lesson.
2. **Query forms instead of answers.** Booked lessons, schedules and rosters require choosing From/To dates and pressing *Load* or *Apply filters*. Sensible defaults ("upcoming", "this week") and infinite scroll would remove most of these controls.
3. **Manual reload buttons everywhere.** Each section has its own *Reload credit history*, *Reload milestones*, *Reload notes* and so on. This exposes the data layer to the user.
4. **Engineering and policy language in family-facing copy.** For example, "Recovery transfers only expired unused credits into a new package; the original history stays intact", "Original credit package 5961d8ca39a6…", "Achievements and revocations are shared immediately…". Guardians need "5 lessons left · expires 28 Dec".
5. **Desktop navigation on mobile.** There are 5–11 nav items per role, a header *Sign out* button and an unread count in a text label. Mobile convention is a bottom tab bar with at most 4–5 destinations per role and secondary items under a profile or more menu.
6. **Actions buried in long pages.** Booking, weekly enrolment, renewal and lesson-change requests are inline forms in the profile scroll. On mobile these should be focused full-screen steps or bottom sheets, each with one primary button.
7. **Unnecessary choices.** Packages starts with an empty "Choose a linked child" select even when there is one child. Admin settings are eight independent forms on one page.
8. **Admin work does not fit a phone, and doesn't need to.** Admin accounts, audit, setup and deliveries are desktop tasks. They should stay usable on mobile but be designed for a wide workspace. Coach, guardian and student are the mobile-first roles.

### Client architecture problems

- `App.tsx` is a 200-line component whose routing, session, directory, profile editing and page selection live in one chained ternary expression on a few very long lines. `styles.css` is 11 minified lines. Neither is reviewable or extensible.
- Every component re-implements fetch, loading, error, retry and stale-response protection with hand-rolled "generation" counters.
- Translations are inline `t('English', '中文')` pairs. There is no catalogue, so Simplified Chinese (an open decision) or reuse by future iOS/Android/WeChat clients means touching every string.
- Hand-rolled router (`navigation.ts`) and role checks duplicated between it and `App.tsx`.
- No design tokens or shared components (list row, card, sheet, empty state, status chip), so pages drift visually.

What is worth keeping: the accessibility work (focus management, skip link, live regions), bilingual coverage, stale-version handling, idempotent retries and the generated OpenAPI types.

### Recommended revamp

Keep the API and the generated contract. Rebuild the client shell and move existing pages across incrementally:

1. **Foundation:** React Router routes per role, TanStack Query for fetching/caching/retry (replaces generation counters and reload buttons), i18n catalogue (JSON per language), CSS tokens and a small component set (app bar, bottom tabs, list row, card, sheet, status chip, empty/error state).
2. **Mobile shell:** top app bar plus a role-specific bottom tab bar. Proposed tabs:
   - Guardian: Home · Lessons · Credits · Inbox · Me
   - Coach: Today · Schedule · Requests · Inbox · Me
   - Student: Home · Lessons · Progress · Me
   - Admin: responsive sidebar workspace, collapsing to a menu on phones.
3. **Role homes:** guardian sees each child's next lesson, credits left and expiry, items needing action and recent feedback. Coach sees today's classes with one-tap roster and attendance, plus pending requests. Student sees the next lesson and recent progress.
4. **Task flows as focused steps:** book a lesson, enrol weekly, renew, request change, buy package (mock checkout). Each step has a confirmation screen showing the credit consequence in plain words.
5. **Plain-language copy pass:** hide IDs and policy explanations; move explanations behind "Why?" links.
6. Move admin pages last, mostly as layout changes.

Each step is a reviewable PR with Playwright journeys at 390px and desktop widths. New lifecycle workflows (cancellation, absence) should be built on the new shell instead of the old one.

## Backend gap analysis

### Shipped and verified

Accounts, invitations, password reset, guardian/student links and separate student login; venues, class types, recurring series, single sessions, coach availability windows and travel-gap checks; school settings; package offers, per-purchase expiry, mock checkout with pending/failed/successful/duplicate outcomes, receipts and a credit ledger; expiry and assigned-coach recovery; flexible bookings, fixed weekly enrolment with partial allocation and explicit renewal with carry-forward; mark present; guardian lesson-change requests with coach decisions; progress notes, skill milestones, in-app notifications, opt-in teaching email with retries; audit history, admin action queue and missing-attendance report; backup/restore, scale and revocation checks; VM packaging and updater.

### Gaps to first release

Sizes: S ≈ part of a stage, M ≈ one stage PR, L ≈ two or more stage PRs.

| # | Gap | Current state | Size | Blocking decision |
|---|---|---|---|---|
| 1 | Absence, illness and attendance corrections | `booking_attendance.status` allows only `present`. No correction endpoint. | L | When no-show can be marked; correction window anchor; pooled-credit reversal; pending-request disposition ([acceptance](STAGE-04-ATTENDANCE-OUTCOMES.md)) |
| 2 | Family cancellation of one lesson | `bookings.status` allows only `reserved`. A booking cannot be cancelled at all. | L | Early/late cutoff; pending-request disposition; renewal after cancelled occurrences; notice recipients ([acceptance](STAGE-04-FAMILY-CANCELLATIONS.md)) |
| 3 | Guardian withdraws a pending lesson-change request | Request status is `pending/approved/rejected` only. | S | None |
| 4 | School cancellation of an occurrence | Read-only impact preview in PR #38, unmerged. No operation. | L | Expired returned-credit remedy; pending-request disposition; notice channels ([acceptance](STAGE-04-SCHOOL-CANCELLATION-SUBSTITUTION.md)) |
| 5 | Coach substitution | **Risk:** `PATCH /classes/{id}` already lets an admin change `coach_id` on a booked class. There's no reason, no notice and no handling of pending requests. Roster and decision access move silently. | M | Whether the substitute needs availability or acceptance |
| 6 | Administrator extra credit grants | None. | M | Attach to an existing package or create a complimentary package |
| 7 | Make-up remedy | Only rebooking with a returned credit. | S–M | Whether any remedy beyond returning the credit exists |
| 8 | Scheduled lesson reminders | None. The delivery worker exists. | M | Lead time, recipients, opt-in default ([acceptance](STAGE-05-LESSON-REMINDERS.md)) |
| 9 | Lesson-linked feedback | Notes are student-level only. | M | Coach scope, eligibility, multiplicity ([acceptance](STAGE-05-LESSON-FEEDBACK.md)) |
| 10 | Service notices (cancellation, substitution, reassignment) | Only booking confirmations and change notices exist. | M, built with 2/4/5 | Recipients and email policy |
| 11 | External email delivery proof | Local Mailpit capture only. | S | Approved recipients and SMTP credentials |
| 12 | Controlled data import | None. | M | Source format, which records, who runs it |
| 13 | Mobile home/summary reads | No aggregate endpoints. Clients must fan out per child and per section. | S–M | None; needed by the UI revamp |
| 14 | Final acceptance and operations | Four-role rehearsal exists for current features only. Live rollback and automatic delivery are not yet proven. | M | Read-only GitHub token on the VM |

Also worth confirming: a guardian cannot complete a mock purchase until an administrator enters its expiry (`awaiting_expiry`). That follows the per-purchase expiry decision but means families cannot self-serve checkout. Native clients will later need token authentication beyond the current cookie + CSRF sessions; that is out of first-release scope.

### Size of remaining work

Excluding AI/voice and real payments: about **12–15 backend/full-stack stage PRs** (gaps 1–14) plus **5–6 UI revamp PRs**. Gaps 1, 2, 4 and 6 are the largest and each needs owner answers before work starts. Gaps 3, 13 and the UI foundation need no decisions and can start immediately.

## Recommended order

1. Merge PR #38 after hosted checks pass.
2. UI foundation, mobile shell and role homes, with aggregate home reads (gap 13) and change-request withdrawal (gap 3). Needs no decisions.
3. Attendance outcomes (gap 1), then family cancellation (gap 2), on the new shell, once decisions are answered.
4. School cancellation and substitution (gaps 4, 5, 10), closing the silent coach-swap path.
5. Extra grants, reminders, feedback, import (gaps 6, 8, 9, 12).
6. External email proof, final four-role acceptance, rollback rehearsal (gaps 11, 14).
