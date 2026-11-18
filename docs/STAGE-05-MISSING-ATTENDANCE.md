# Missing attendance action queue

Add an administrator report of ended classes with unmarked reserved bookings. Show the number of affected classes in the existing action queue. The report and roster link help administrators find missing records. They do not classify a swimmer as absent or modify bookings, credits, attendance or notifications.

Baseline `7003ab4`. Integrate the pending lesson-change queue after PR #27 merges and preserve its counts and navigation. Use the existing class roster and permission checks.

## Selection and presentation

- Select a class when its stored end instant is at or before the server's observation time and at least one booking is reserved with no attendance row. Include a lesson ending exactly at the observation time. A mark of any supported attendance outcome removes that booking from the missing count. A cancelled or otherwise non-reserved booking is excluded.
- Count each affected class once in the action queue. On the report show one row per class and the number of missing marks for that class, with the class type, coach, venue, and Hong Kong date and time. Include earlier unresolved classes through bounded pages and stable ordering by end time and class ID. The summary counts the full qualifying set. The list and summary use the same predicate. Each request uses one server observation time. Counts may change between requests as staff record attendance.
- Link directly to the existing roster for the exact class. The link must work on refresh, including for past and overnight classes, and browser Back returns to the report. A resolved queue row or outdated fetch must not strand the administrator. If the class cannot be opened, use the existing unavailable and retry patterns. Prefer routing by class ID over relying on the schedule's current-week list and locally open roster state.
- Use administrator authorization on each summary and report read. Existing roster permissions remain independent, including access for an assigned coach. Recheck active roles after sign-in and avoid rendering stale rows after account changes.
- Use the labels “Classes with missing attendance” and “Unmarked bookings” or their Traditional Chinese equivalents. Keep the empty state and error/retry states available in both languages. When a roster is reviewed or a mark is recorded, refreshing or returning to the report shows the current counts.
- The attendance opening setting controls whether the existing roster action is available. It is possible for a short class to end before that opening time. The class belongs in the report at its end, and the roster still enforces the opening time. Attendance correction windows and package absence, illness and cancellation policies do not affect this report.

## Verification

1. PostgreSQL tests cover ongoing, future and ended classes, the exact end boundary, empty and fully marked rosters, and a mixed class with multiple unmarked bookings. Verify cancelled bookings are excluded when that lifecycle state exists. Assert total class count and per-class booking counts, paging through tied end times, and accurate counts after the final unmarked booking is marked.
2. Check direct report and summary access for unauthenticated, parent, student, coach and revoked administrator sessions. Verify assigned coach roster access remains available independently of report access. Verify zero results, dates, paging bounds, and unchanged pending-change, purchase and delivery counts.
3. Exercise read-only behavior with database assertions around report loads and retries. Assert no new attendance marks, ledger movements, notifications or audit write side effects from reporting. Include a short ended class whose attendance opening is still in the future.
4. Real Chromium verifies an administrator following the action queue to an ended roster, reload and Back, seeing a mark reflected in current count, and the bilingual empty and error states. Use synthetic bookings and keep the lesson-change queue regression.

## Open boundary

The report deliberately uses the end of class and has no reporting grace interval. A separate operational definition of overdue attendance, staff escalation or automatic absence is outside this slice and requires approved rules. No policy decision blocks the ended-class report as scoped.
