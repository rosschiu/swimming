# Pending lesson changes in the administrator action queue

Planning draft based on main at `55a4a9a`. The administrator action queue already returns school-wide counts of purchases awaiting expiry and failed notifications. The lesson change list already supports `GET /api/v1/lesson-changes?status=pending`, but browser navigation only preserves delivery status and the current pending checkbox is local to the coach view.

## Acceptance

- Add a nonnegative integer `pendingLessonChanges` to the existing administrator action queue response and generated contract. Count persisted pending requests school-wide in the summary query. Count all matching requests across pagination. Approved and rejected requests do not contribute. Preserve the original counts and existing administrator-only, active-account gate.
- Add a bilingual pending lesson-change link and count to the existing action queue. Use `/lesson-changes?status=pending` as a real link so copy-link, refresh, Back/Forward and opening a new tab work. Keep the standard sidebar link as all administrator history. The pending view exposes an accessible path back to all history.
- The destination loads from the existing authorized endpoint. Share the same status filter across its initial page and pagination. Switching filters or accounts clears earlier items, cursors and decision feedback. A late response must not replace newer results or reveal a prior account’s records.
- The count is current as of summary fetch. Returning to the queue or refreshing re-fetches it. If a coach decides the last request between the summary and navigation, show a bilingual pending-empty state and allow refresh or viewing all history. Show load errors and retry separately from a zero count.
- Preserve the coach’s current pending default, separate approve/reject authority, and guardian scoping. Administrators can inspect requests and their conflicts. This entry grants no decisions or new actions.
- Keep labels in English and Traditional Chinese. Align the accessible link name, status count and pending-empty message with the existing queue conventions. Keep historical and already completed changes in the regular history view.

## Verification

1. PostgreSQL tests exercise mixed statuses and scopes, role bypass attempts, expired sessions, count greater than list page size, and count decrease after an existing coach decision. Ensure the original counts still pass.
2. Frontend tests cover link generation, direct URL and browser history, filter preservation during pagination, invalid status fallback, loading/error/empty states, bilingual labels, same-view and account changes, and out-of-order responses.
3. Chromium uses a real guardian request, checks the administrator count and pending deep link, makes the decision as the assigned coach, then refreshes or returns to the administrator queue. The count decreases, the filtered list becomes empty and regular history still shows the completed request.

## Product boundary

Count persisted pending requests even when current conditions prevent approving them, consistent with the existing pending filter and coach rejection workflow. Auto-expiry, blocker summaries, reminders and missing-attendance counts are separate work.
