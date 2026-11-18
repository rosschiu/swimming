# Administrator action queue

The administrator landing page should show how many purchases need an expiry date and how many failed email deliveries need attention. Both operations and their role checks already exist on main. Each count links to the corresponding filtered list. Counts come from the same persistent records that administrators update. A failed count request must be distinguishable from an empty queue.

The summary endpoint returns counts only, checks current administrator access, and requires no migration. Verify an expiry assignment removes that purchase from the queue, a failed delivery is visible under the correct filter, other statuses are excluded, and non-administrators cannot access the endpoint. Verify refresh and direct navigation through the normal application. Attendance and change requests can join the queue when their workflows exist.

## Verification

All 160 frontend tests and the production build passed. The PostgreSQL race test verified counts after actual expiry assignment and delivery retry, including administrator deactivation before the count query. All 23 browser journeys passed together. After independent review found a stale delivery-filter URL, four focused Chromium journeys verified changing the filter, reloading, going back through earlier filters, retrying the delivery and denying access to non-administrators.
