# Stage 02c: coach availability

In progress. This independent slice brings forward availability management while package/booking policy answers are pending. It does not finish the overall goal or claim that availability automatically creates classes.

Scope reaches migration 4, Go routes and tests, OpenAPI/generated types, coach/admin React availability screens, school settings, frontend tests and real-stack Playwright. Existing scheduling/session records remain authoritative for actual classes.

Acceptance:
- Coaches create, edit and cancel their own venue/time/capacity availability records; administrators review all coaches' submitted records. Other coaches and families cannot access or mutate them.
- Default modification cutoff is 72 hours, administrator-configurable. Creation checks new start; editing checks old and new start; cancellation checks old start. Equality is allowed. Server time/settings are rechecked after transaction locking.
- Pending availability cannot overlap another own pending slot or an assigned class. Concurrent submissions cannot bypass this check. Unconfirmed availability does not promise travel feasibility or reserve class capacity.
- Versioned edits and cancellations reject stale submissions. All changes are audited, with atomic rollback on failure. Cancelled rows remain in history.
- UI explains cutoff locks and planning status, preserves form data after failed saves, and uses explicit Hong Kong dates/times including overnight end dates.
- Real-stack journey enters availability as coach, verifies persisted admin visibility, edits/cancels it, and verifies role isolation and cutoff enforcement.

The user has been asked whether an availability slot is a scheduling window or an exact proposed session. Publication into classes is dependent on that answer and excluded from this slice, not removed from further planning.

Validation before PR: 37 frontend tests, production build and 10 cumulative real-stack Playwright journeys passed. Migration4 applied to existing data. Independent reviewer approved fixes for cutoff after eligibility locks and archived-venue selection, conditional on final backend race/CI. Focused backend availability tests passed, including deterministic delayed-row cutoffs and settings rereads; final race suite running.
