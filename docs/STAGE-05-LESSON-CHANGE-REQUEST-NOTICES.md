# In-app lesson change request notices

The [lesson-change history](STAGE-04-LESSON-CHANGES.md) currently gives the assigned original coach a queue and the authority to decide requests. The backlog leaves request-submission notices open. Add a coach inbox notice for the committed request and link it to that exact scoped history record.

## Acceptance

- A valid request creates one unread in-app notice for the active coach assigned to the original lesson at commit. The replacement coach only receives it if also assigned to the original lesson. The coach need not have a separate student profile relationship because the existing queue scopes by original class. Request, audit, and notice commit atomically. Invalid, stale or failed requests create no notice. Retries return the same request without creating another notice.
- Inbox list, unread badge and mark-read require an active recipient who currently coaches the original class. Exact request detail and decisions use the existing original-class assignment checks. After reassignment, the former coach loses access to both the notice and the history record. The newly assigned coach can open and act on the request from the current queue. Issuing a new notice on reassignment remains a separate policy decision.
- Show English and Traditional Chinese copy and the current stored request status. An already decided request stays visible to its original recipient while they remain assigned, with an accurate outcome and no decision controls. A stale approval or rejection uses the existing service checks, including conflict responses and preserving the booking when an approval fails.
- Link to the exact history record, even after it falls behind the first page. Do not require coach access to unrelated profile details. Keep family inbox and teaching preferences scoped as before. The coach inbox contains request notices and has no teaching email setting.
- No external email, delivery rows, or backfill. The explicit synthetic history command continues to create no notices through an internal-only request option.

## Verification

Exercise PostgreSQL upgrade, request replay, injected notice-write failure, current assignment and active-role checks on every read, deciding between load and action, and reassignment. Verify the exact scoped link, notice state after decision, mark-read, and role navigation in Chromium. Confirm existing family notification and demo fixture regressions pass.
