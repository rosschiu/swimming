# Flexible booking verification

Draft [PR #17](https://github.com/rosschiu/swimming/pull/17) starts from verified main `18eab4e`. Migration 11 adds persistent bookings with a foreign key that ties the selected credit package to the same student. Creating a booking writes the reservation, ledger movement and audit event together.

## Verified workflows

- All 20 Chromium journeys passed together against a real Go service and dedicated PostgreSQL database. The booking journey exercised mock checkout, guardian review and confirmation, sibling isolation, administrator level override, the student's read-only lesson list and the assigned coach's roster.
- All 163 frontend tests and the production build passed before the end-time display refinement. Tests cover confirmation, retaining the same request key across an uncertain response, and requiring a fresh review when class versions change.
- PostgreSQL race tests exercised the last place and last credit, duplicate submissions, recovered credit packages, guardian access revocation and rollback on audit failure. Two identical requests against a recovered package returned one booking and one ledger reservation. The original receipt and expiry remained intact.
- At commit `216fa32`, an independent reviewer ran the queued student-level and class-edit regression three times with the race detector. The test waits for PostgreSQL to report the booking blocked on the editing transaction, then checks that the committed edit is visible before any reservation is written.
- The independent reviewer ran 87 navigation and scheduling tests and found no blocking reservation defect. They requested that the end time used for credit validity be visible in the class choice and confirmation.

## Outstanding

Configurable booking validity, including coach authority scope, remains pending. Lesson-end validity is the implemented default. Final integration and CI results must cover the complete stage before merge.

Class choices and confirmation now show start and end in Hong Kong time, including both dates for an overnight lesson. The revised frontend passed all 163 tests, the production build and the real-stack booking journey.

The opt-in PostgreSQL scale test passed under the Go race detector with 500 registered students, 10 coaches, 20 venues and 50 weekly classes. Four reservations per class produced 2,600 bookings across 13 weeks. It checked lesson and roster identities, denied out-of-scope reads, verified ledger reservations and booked a remaining place through the API. This measures correctness at representative occupancy; it does not establish a response-time guarantee.
