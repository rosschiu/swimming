# School cancellation and coach substitution

Planning baseline `567a45b`. The proposed first slice lets an administrator cancel one upcoming occurrence or replace its coach before it starts. The preview identifies the occurrence, affected swimmers, credit consequences and intended family notices. Whole-series changes, refunds, automatic rebooking and retrospective attendance corrections require separate scope.

## Recorded rules

- School cancellation returns the entitlement and explicitly addresses validity. A booking reserves one credit, attendance consumes it, and reserved credits retain their original expiry through renewal by default. A school cancellation must therefore reconcile each reservation with its original package. [Decision record](DECISIONS.md)
- Administrators manage scheduled lessons separately from coach availability. Editing or cancelling availability leaves allocated lessons intact. Coach scheduling enforces overlaps and the configured inter-venue travel gap, initially 30 minutes. [Decision record](DECISIONS.md)
- The currently assigned coach of the original class decides pending lesson changes. A change in the target coach invalidates the submitted target details. Families retain their original booking during a pending request. [Lesson changes](STAGE-04-LESSON-CHANGES.md)
- The product requires in-app and external notifications. Existing teaching email has preference and recipient controls. Adoption of those controls for school service notices is an open decision. [Decision record](DECISIONS.md)

## Proposed acceptance

1. **School cancellation.** Confirm one occurrence by stable ID, reason and reviewed version. Preserve its date, coach, venue, booking IDs and enrolment history. Remove it from bookable choices and missing-attendance work. Suppress subsequent attendance marks, new reservations and approvals into it. An original booking or enrolment replay must preserve cancellation. Other occurrences, paid receipts and unrelated families stay unchanged.
2. **Reconcile every affected booking.** For flexible, fixed and renewed bookings, release precisely the reservation attached to that booking. Include two swimmers with different package policies and a swimmer who has a second reservation on the same package. Confirm the resulting available, reserved, consumed and expired quantities against attributed ledger movements. Include an expired original package after renewal. Show the agreed validity remedy to staff and family. A previously resolved booking must never receive a second entitlement.
3. **Coach substitution.** Preserve the occurrence, bookings, times, venue, capacity, enrolment links and credit balances. Check the incoming coach is active and can teach the slot under overlap and travel rules. Retain who originally taught or was scheduled, who replaced them, actor, reason and time. For this occurrence, the replacement receives current roster and pending-decision access and the previous coach loses it. Check direct URLs and in-flight requests. Preserve unrelated assignments and historical decision attribution.
4. **Pending changes and races.** Identify pending requests with the affected occurrence as original and as target. For a substituted target, existing stale-target handling preserves the original booking. For a cancelled target, preserve its requester's original seat and credit. For a cancelled original, reconcile its entitlement once. Apply the agreed terminal statuses and show their cause in family and administrator history and the coach queue. Race cancellation with booking, approval, attendance, family cancellation, substitution and retry. Verify one committed outcome, correct capacity, no duplicate movement and no success notice for a rolled-back operation.
5. **Family communication.** Preview exactly which families are affected. Show cancellation versus substitution, the occurrence in Hong Kong time and venue, and the returned credit and validity or new coach as applicable. Test two guardians, siblings, an unrelated family and a revoked link. Verify bilingual in-app details, recipient authorization on reads, no disclosure of other swimmers, and policy-controlled email. Persist the event and intended notices with the change, deduplicate retries per event and recipient, and distinguish committed changes from delivery failures. Exercise delivery retry using local SMTP capture. Recheck delivery eligibility and suppress or update outstanding reminders for superseded details when reminders are implemented.
6. **End-to-end verification.** Use real PostgreSQL for concurrent updates, rollback, stale preview and replay. In Chromium follow one cancellation and one substitution from administrator confirmation through old and new coach access, family schedules, entitlement history and notices. Verify family and coach accounts cannot initiate school-wide changes. Compare unrelated bookings, ledgers and receipts before and after each operation.

## Decisions needed before implementation

| Decision | Proposed first-slice choice for confirmation |
| --- | --- |
| How is an expired or unusably short returned entitlement made usable? | Keep the original ledger trace and require the administrator to supply a replacement expiry when the original entitlement has expired. Agree whether a future expiry that leaves too little time also qualifies, and who defines that threshold. |
| Which exceptional operations are authorized? | Start with administrators and single upcoming occurrences. Refer already-started, partly marked or retroactive changes to a separately defined correction workflow. Confirm whether a substitute needs an explicit availability window or acceptance in addition to existing scheduling checks. |
| How should cancellation resolve pending changes? | Close affected requests with a school-cancellation reason and retain them in history. Confirm the terminal disposition for original versus target cancellations and whether families should be invited to submit a new request. |
| Who receives service notices and through which channels? | Confirm guardian and student recipients, whether operational email respects existing teaching opt-in, and how to group sibling notices. Continue to use authorized demo recipients or local capture. |

A make-up place, automatic transfer, cash refund or automatic compensation credit has no recorded default. The primary cancellation rule is to return the entitlement. A separate decision is needed to offer any additional remedy.

## Delivered: school cancellation (stage 5 of the mobile-first plan)

The owner approved the proposed first-slice choices on 2026-11-18. Coach substitution follows as its own stage.

- **Operation.** From Schedule → Review cancellation impact, an administrator enters a reason and confirms. `POST /classes/{id}/cancellation` sends the reason, the class version reviewed in the preview and a request key; a stale version returns `version_conflict`, a started lesson `class_started` (left to attendance corrections), and an exact replay returns the recorded result.
- **Credits.** Every reservation is cancelled and its credit returns to the funding package regardless of the package's cancellation policy. When that package is already past its Hong Kong expiry date, the administrator must enter a new expiry (today or later) for the booking (`expiry_required` lists the bookings); the unit is released into the original package's expired history and restored into a new package with that expiry as an audited `credit_recoveries` entry, keeping the original ledger trace. No make-up place or extra compensation is created.
- **Lesson state.** `class_sessions.cancelled_at` marks the occurrence and an immutable `class_cancellations` row keeps the actor, reason and request. The lesson rejects new bookings, weekly enrolment, lesson-change targets and edits, disappears from change options and home lesson lists, and no longer blocks its coach in overlap and travel checks. Schedule shows it as cancelled by the school.
- **Pending changes.** Requests into the lesson close as "School cancelled the requested lesson" and keep the requester's original booking; requests out of it close as "School cancelled the lesson". Families may submit a new request.
- **Notices.** Each affected booking's linked guardians and student get an in-app `class.cancelled` notice, with generic email per the school cancellation-email setting and their service email preference; the class coach gets in-app notices. Families see "Cancelled by the school · credit returned".
- **Migration 27** adds `cancelled_at`, `class_cancellations`, a `cancelled_by_school` flag on booking cancellations (school rows have no early/late timing) and the `class.cancelled` notice kind.

Not covered: whole-series cancellation, cancelling a lesson that has started, and notices for families whose inbound request closed.

## Delivered: coach substitution (stage 6 of the mobile-first plan)

- **Operation.** From Schedule → Substitute coach, an administrator picks another active coach and gives a reason. `POST /classes/{id}/substitution` sends the coach, reason, reviewed class version and a request key. The incoming coach must be active and free of overlap and travel-gap conflicts (`scheduling_conflict`); a stale version, a started or cancelled lesson, or the same coach is refused; an exact replay returns the recorded substitution.
- **Unchanged.** Bookings, times, venue, capacity, enrolment links and credits stay as they were.
- **Access.** Roster access and pending lesson-change decisions follow the class's current coach, so they move to the substitute at once and the previous coach loses them, including through direct URLs. Historical decisions keep their original attribution. A pending request targeting the lesson is protected by the existing stale-target check.
- **Silent changes closed.** A class with reservations can no longer change coach through the ordinary edit (`substitution_required`).
- **History and notices.** An immutable `class_substitutions` row records the previous and new coach, actor, reason and time, with a `class.substituted` audit event. Booked swimmers' guardians and students and both coaches get an in-app `class.coach_changed` notice; the previous coach can still read theirs. No email is sent for substitutions.
- **Migration 28** adds `class_substitutions` and the notice kind.

Not covered: a substitute accepting the cover, substitution of lessons that have started, and email notices for substitutions.
