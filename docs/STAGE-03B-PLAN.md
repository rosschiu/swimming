# Stage03b: flexible booking design

Students must match the class level unless an administrator records an override. Administrators configure credit validity school-wide.

## Intended workflow

Guardian selects a linked student, future class and paid credit package. Booking atomically reserves one place and one credit. Parent/student own lesson lists and the current class coach's roster read the same persistent bookings. An administrator may override a level mismatch with a reason; guardians cannot supply that authority. Recurring enrolment and lesson lifecycle follow separate slices.

## Reviewed invariants

Acquire scheduling advisory lock829417003 before identity829417002, then relevant student/class/package row locks. Existing student edits do not acquire the advisory locks, so the booking must lock the student row before checking level. Recheck active actor, current guardian scope, class version, student overlap, capacity, exact package ownership and usable ledger balance in the transaction. Record unique booking and ledger reservation source; identical actor-key replay returns original booking after fresh access checks, conflicting input returns409.

Roster permission follows current class coach assignment, independently of general teaching relationships. Return only roster identities/levels/status, not financial details. Self-linked students can read their own bookings but cannot create them. Availability provenance edits never cancel booked sessions.

Eligibility must retain the compared student/class levels and any administrator override reason/actor. A class with reservations keeps its venue, class type and start and end times. Administrators can reassign the coach and adjust capacity as long as it covers every reservation. Later changes to swimming levels leave the eligibility recorded at booking intact.

## School-wide credit validity

One administrator-selected rule applies to every student and coach. By default a credit must cover the whole lesson. Administrators may instead require validity at lesson start. Each reservation records the effective rule at booking time. Changes apply prospectively. Recurring partial-allocation and reserved-credit transfer alternatives are separate03c questions.

## Acceptance

Meaningful PostgreSQL race tests cover last place/last credit, scope revocation, class/level edits, duplicate requests, rollback and Hong Kong expiry boundaries. Frontend/browser tests cover guardian booking, student read-only lessons, current coach roster, administrator override and child switching. Sol develops, Astra independently tests/reviews, lead integrates/releases after cumulative CI.

## Integration with recovered credits

Select the exact package ID when reserving credit. A recovered package can share its purchase with several other packages. Store the booking, its available-to-reserved movement and audit record in one transaction. Include both the class version and class-type version in the booking request, since a class type can change level independently of its sessions.

Use the same booking records for guardian and student lesson lists and the current class coach's roster. Keep package identifiers, prices and override reasons out of roster and student responses. Verify reassignment transfers roster access and guardian unlinking prevents replay.

Test a booking using a recovered package while other credits share the original purchase. Include a stale class type edit, overlap at adjacent lesson boundaries and concurrent requests for the last place and last credit. Record the effective policy and compared levels on each booking so later changes can be applied prospectively.

A lesson occupies the half-open interval from its start to its end. Under the default validity rule, a package covers a lesson ending exactly at midnight after the package's last valid Hong Kong date. Extending that lesson beyond midnight requires later validity. Regression tests should exercise both sides of the boundary.
