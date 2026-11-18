# Stage 04 lesson changes

- `GET /api/v1/lesson-changes` returns `{items, cutoffHours}`. Guardians see requests for linked students, the currently assigned original coach sees requests for that class, and administrators see the school history.
- `GET /api/v1/students/{id}/bookings/{bookingId}/change-options?from=YYYY-MM-DD&to=YYYY-MM-DD` returns `{items, cutoffHours}` for a linked guardian. Dates are inclusive Hong Kong dates. Items are eligible class sessions.
- `POST /api/v1/students/{id}/bookings/{bookingId}/change-requests` accepts `{targetClassId, idempotencyKey}`. A new request returns 201. An exact replay returns 200 and the current request.
- `PUT /api/v1/lesson-changes/{id}/decision` accepts `{decision, reason?}` from the currently assigned original coach. Decisions are `approved` or `rejected`. An exact replay returns 200. Conflicting decisions or a failed approval return 409.

Request records include `id`, `bookingId`, `studentId`, `studentName`, `originalClass`, `targetClass`, `status`, `requestedAt`, and after a decision `decidedAt`, `decidedBy`, and any supplied `decisionReason`. Status is `pending`, `approved`, or `rejected`.

The original class must be upcoming. The configured cutoff includes equality. A pending request leaves the original seat and credit reserved. Approval rechecks both classes, target eligibility, capacity, overlap and package coverage. Approval moves the existing booking with its credit reservation and enrolment linkage. Rejection or failed approval preserves the original booking. A failed approval leaves the request pending so the coach can retry or reject it.

Checks cover cutoff equality, scoped access, request replay and conflicts, capacity changing after submission, concurrent decision retries, audit rollback, and reservation conservation.

## Cross-role acceptance journey

A guardian requests a different lesson for one linked swimmer. The original roster and credit balance stay unchanged while the request is pending. The originally assigned coach sees the pending request. Once the coach approves, the guardian and student see the new date, the swimmer leaves the original roster and appears on the destination roster, and the same single credit stays reserved. The administrator can see who requested and decided the change. For a fixed enrolment, the other selected lessons stay as booked.

Exercise a separate request where the last target place is taken after submission. Approval must fail without moving the swimmer, changing either credit balance, or publishing a successful change. Retrying after the conflict clears must complete exactly once. Check that a guardian cannot request changes for another family's swimmer and that an unassigned coach cannot decide the request.
