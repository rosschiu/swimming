# Stage03b: flexible booking design

Preparation only, based on merged finance stage0f8e029. User requires level matching with administrator override, and configurable coach/admin booking-validity defaults. The precise coach configuration scope and supported alternate validity rules are pending; do not silently implement default-only behavior as complete.

## Intended workflow

Guardian selects a linked student, future class and paid credit package. Booking atomically reserves one place and one credit. Parent/student own lesson lists and the current class coach's roster read the same persistent bookings. An administrator may override a level mismatch with a reason; guardians cannot supply that authority. Recurring enrolment and lesson lifecycle follow separate slices.

## Reviewed invariants

Acquire scheduling advisory lock829417003 before identity829417002, then relevant student/class/package row locks. Existing student edits do not acquire the advisory locks, so the booking must lock the student row before checking level. Recheck active actor, current guardian scope, class version, student overlap, capacity, exact package ownership and usable ledger balance in the transaction. Record unique booking and ledger reservation source; identical actor-key replay returns original booking after fresh access checks, conflicting input returns409.

Roster permission follows current class coach assignment, independently of general teaching relationships. Return only roster identities/levels/status, not financial details. Self-linked students can read their own bookings but cannot create them. Availability provenance edits never cancel booked sessions.

Eligibility must retain the compared student/class levels and any administrator override reason/actor. Class and class-type edits must not silently invalidate existing reservations; capacity cannot fall below reservations. The next contract must define explicit edit guards or snapshot-based preservation before implementation, including later student progression.

## Pending configuration

Proposed admin school defaults with assigned-coach student overrides, and validity choices lesson-end(default) versus lesson-start, are awaiting user confirmation. The user has already approved the defaults and configurability; only concrete alternatives/scope remain open. Recurring partial-allocation and reserved-credit transfer alternatives are separate03c questions.

## Acceptance

Meaningful PostgreSQL race tests cover last place/last credit, scope revocation, class/level edits, duplicate requests, rollback and Hong Kong expiry boundaries. Frontend/browser tests cover guardian booking, student read-only lessons, current coach roster, administrator override and child switching. Sol develops, Astra independently tests/reviews, lead integrates/releases after cumulative CI.
