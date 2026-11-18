# Demo lesson change history acceptance

Add an explicit, demo-only step after the synthetic school seed. It should work against a fresh school and a database that already has the `demo-school-v1` audit marker. Verify the known school marker, expected fixture relationships, and the exact synthetic user IDs, roles and addresses before writing. Reuse the existing demo opt-in, and fail if fixtures are missing, out of scope, or no longer eligible. Keep the original seed marker and anchor intact.

## Three examples

Use separate existing reserved Beginner bookings from Harbour Pool, where the coach shortcut owns the original classes.

- Amy’s guardian requests an alternative and leaves it pending. The admin queue shows one new item, the guardian can see its status, and the assigned coach can read it.
- Anson’s guardian requests an alternative and the same coach approves it. The booking moves to the replacement with its ID, enrolment and credit reservation intact.
- Daisy’s guardian requests an alternative and the same coach rejects it with a short synthetic reason. The original reservation stays in place.

Create each request through the guardian lesson-change flow using the current target version. Apply decisions through the assigned-coach flow. Keep the original audit attribution and timestamps. Verify the student shortcut sees Amy’s booking within its existing scope and that the student change-history API is forbidden. Check that an unrelated guardian and coach cannot see these records.

## Existing demo feasibility

The live school seed from November 18, 2026 uses the Hong Kong anchor week starting November 16. Its Harbour Pool future reserved lessons are November 23 and 30 at 17:00 local time. The other seed series teach different levels, while the unbooked Harbour occurrence is already past. For a rollout inside the current request and approval windows, the November 23 booking for each example can use a single additional published, capacity-checked Beginner class in the same venue with the same coach on November 24 at 17:00 local time, subject to preflight. Confirm current class conflicts, existing requests, attendance, same-level placement, credit validity, cutoff and an actionable pending destination on a restored backup. Use that same preflight again immediately before applying. If the earlier originals become ineligible, evaluate the November 30 originals and a distinct eligible replacement before the cutoff. If there are no eligible existing originals, stop and plan a small future fixture extension. Keep every existing lesson date and account.

## Repeat and verify

Use a stable replacement class identity, deterministic request idempotency keys, and a separate completion marker for this step. Resume interrupted runs by checking exact fixture identities and service outcomes, including the approved booking’s new class ID. Keep completed runs read-only on replay, including after a demo visitor decides the pending example. Fail closed on mismatched input, a competing request, or an unexpected decision during resume. Verify the live cutoff and active roles with actual wall-clock time.

On a restored demo backup and a fresh disposable seeded database, run twice and simulate interruption after each committed step. Check exactly three fixture requests and at most one new class, unchanged account and package counts, preserved original dates, unchanged credit totals, and zero added notification or outbound-email records. Browser checks use read-only navigation for the demo queue, all-history, guardian scope, coach scope and student booking. Keep mail delivery off. Before live application, back up the demo database and record the before/after fixture IDs and count deltas.
