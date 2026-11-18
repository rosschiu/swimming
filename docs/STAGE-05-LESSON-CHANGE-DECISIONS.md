# In-app lesson change decisions

[The lesson-change stage](STAGE-04-LESSON-CHANGES.md) already persists the requester, original and proposed lesson snapshots, outcome, coach, timestamp and optional reason. The backlog leaves dedicated decision notices open. This slice tells the requesting guardian about a committed coach decision and links to its persisted detail on the student's profile.

## Acceptance

- A successful assigned-coach approval or rejection creates one unread notice for the original requester, provided that requester is still an active guardian of the same student. A missing or revoked link does not prevent the coach from deciding. Existing notification reads and mark-read enforce current recipient and student access. Other guardians, students, administrators and coaches receive no notice from this event.
- The notice commits with the change decision, audit record and any booking move. An exact decision replay returns the saved outcome without another notice. Pending requests and failed/conflicting decisions produce no notice. An injected notice-write failure rolls back the decision and any move and allows a subsequent retry. No notice is backfilled for decisions that already existed when this deploys.
- The English and Traditional Chinese inbox displays an approval or rejection with the existing unread and mark-read behavior. A bookmarkable link opens the exact persisted request in the student's profile, including the outcome, decision timestamp, original and requested lesson snapshots and any reason. It still works when the request has fallen behind the first history page. Current access is checked for every fetch. A forged student/request pair or a revoked relationship exposes no request data.
- Keep the current scope of schoolwide history and the normal family profile. The inbox detail requires an active requesting guardian and the exact student/request pair. Existing shared teaching notices and booking confirmations keep their current behavior. Email, SMS, external consent, other recipients and request-submission notices remain separate.
- The explicit demo history fixture must retain its zero-notification promise. The synthetic CLI uses the same decision transaction, with an internal option to omit the in-app notice. Public HTTP decisions always use ordinary notice behavior.

## Implementation and verification

Use the existing notifications table, scoped read/unread paths, and two new kind values for approvals and rejections. Use the request ID as the notification source. Add a scoped read for one decision under the student, and a matching profile route and inbox link. Use the stored lesson snapshots for the decision detail and the current booking service for the live schedule.

Verify a real PostgreSQL upgrade from the previous notification-kind constraint; preservation of existing notification kinds; both decisions and exact replay; transaction rollback with an injected constraint; current-access checks for inbox, unread count, mark-read and detail; delivery-row invariance; fixture replays; and a Chromium guardian/coach journey that reads both decisions, uses a direct profile link and sees the updated booking when approved.
