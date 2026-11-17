# Stage 02d: schedule lessons from availability windows

Status: implementation and validation in progress; not independently reviewed or accepted. User confirmed that one availability window can accommodate multiple shorter lessons. Independent review remains required; the requested `gpt-6.2-sol` model was rejected again by the delegation tool after goal resumption.

## Complete workflow

An administrator selects a submitted coach availability window, chooses a class type, lesson start/end and capacity, and creates an actual class. The class appears in the existing administrator schedule, assigned coach schedule and family class catalogue. This does not create student bookings. The administrator can schedule another non-overlapping lesson in the remaining time.

Creation must derive coach and venue from the selected window and recheck its version, status and bounds in the transaction. Reuse existing class eligibility, overlap and travel-gap validation. Preserve a durable source relationship and the window version used to create each class. Do not mark the entire window used after one shorter lesson.

## Confirmed lifecycle

The user confirmed that subsequent window edits/cancellation leave scheduled classes unchanged; administrators manage class changes separately. Store the source relationship as historical provenance, not an ongoing scheduling constraint. Display this distinction in window edit/cancel and linked-class views. Normal class corrections retain the source history.

The current UI calls window capacity “Proposed capacity.” Do not silently convert it into an immutable class-capacity restriction. The scheduling form can prefill it; any stricter rule needs an explicit decision.

## Code findings and scope

- `services/api/internal/app/availability.go`: routes and scoped reads exist. All mutations use `beginScheduling`. `availabilityConflict` rejects overlaps with **every** assigned class. Without adjustment, a class created from a window would prevent edits to its own source window. Preserve protection against unrelated overlapping commitments while implementing the chosen source-window semantics.
- `services/api/internal/app/scheduling.go`: reuse `checkClass` and `insertClass` inside the existing scheduling transaction; do not call the HTTP create endpoint from another handler. `eligibleClass` holds share locks against concurrent account/reference changes. Class corrections currently allow administrators to change coach, venue and times; decide how the source relationship is displayed after corrections.
- Database migration and `db.go`: persist source links without duplicating classes or changing historical migrations. Ensure transactional rollback includes class, link and audit event.
- `contracts/openapi.json` and generated web types: document scoped source-class reads, creation input, stale/conflict responses and retry semantics before parallel implementation.
- `apps/web/src/Availability.tsx`: add the administrator scheduling workflow and linked-class display, with English/Traditional Chinese text, labelled controls, preserved input on failure and clear current versus historical source information.
- Existing schedule consumers must display the created class without a separate projection or fixture. Scope linked-class reads carefully after a class is assigned to a different coach; a historical source link must not bypass current class permissions.
- Extend availability/backend integration tests, frontend interaction tests and `e2e/availability.spec.ts`. Re-run scheduling regressions and contract generation/checks.

## Acceptance checks

1. Schedule two adjacent shorter lessons inside one window; both persist and appear across authorised role screens. Equal start/end boundaries are allowed when the existing scheduling rules permit them.
2. Reject inverted/out-of-window times, stale windows, cancelled windows, inactive references and coach overlaps/travel violations. Failed attempts leave no class, link or success audit entry.
3. Race creation against window edits/cancellation and reference archival. The committed result must satisfy the window and reference state observed under the transaction locks.
4. Retry a submission after an uncertain network response without creating a second class. Define a stable request key and payload-mismatch handling in the contract; concurrent identical requests create one class and one audit event.
5. Parents/students cannot allocate classes or read private availability. Coaches cannot allocate classes or inspect another coach's window. Reassignment must not expose another coach's current class details through the source link.
6. Verify that subsequent window edits/cancellation leave classes unchanged, and class corrections preserve source history. Test the existing 72-hour configurable boundary and avoid introducing automatic class cancellation.
7. Frontend tests cover loading, stale data, failure/retry, duplicate clicks and keyboard-accessible controls. Real-stack Playwright covers coach submission, admin allocation, coach schedule and family catalogue.
8. Full cumulative frontend/backend/PostgreSQL/contract/Playwright checks, independent subagent review, lead review, passing CI, stage PR and verified merge are required before advancing.

## Proposed ownership once delegation is available

Lead owns API contract, integration, E2E and delivery evidence. Backend worker owns migration and Go implementation/tests. Frontend worker owns availability UI and frontend tests after contract agreement. A separate non-implementing reviewer checks the integrated stage. These are proposed assignments, not running agents.
