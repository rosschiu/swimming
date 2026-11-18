# Stage 03 design and decision gate

Stage03a resumes from verified main `2d1fca1` on branch `stage/03a-purchases-credits`. The user approved administrator-entered expiry individually per purchase on2026-11-18. Sol is refining the implementation contract before Astra review; no entitlement implementation is claimed yet.

## Delivery slices

- 03a: administrator package offers and school credit-policy defaults; guardian mock checkout, payment attempts, receipts and credit ledger.
- 03b: flexible booking reserves a place and credit atomically; parent/student own lesson lists and coach actual rosters.
- 03c: bounded fixed enrolment, explicit renewal and configured carry-forward.

## Agreed requirements

Booking reserves credits; attendance consumes them. Cancellation/absence policies are configurable per case and default to retaining credits. Expiry and carry-forward default enabled. Package expiry is administrator-entered. Recurring enrolment renews explicitly. Assigned coaches recover credits with reason and new expiry. Students have their own schedule/progress access; guardians control bookings and payments. All financial interactions in this goal are simulated.

## Proposed 03a persistence and controls

Separate package definitions, immutable purchase orders and payment attempts. New orders snapshot price in integer HKD minor units, allowance and effective credit policy/version. An administrator finalises individual expiry before the first payment attempt when expiry is enabled. School defaults and optional complete package overrides affect new orders only.

Order creation uses a stable actor-scoped idempotency key; replay returns the same order, while a different payload under the same key is a conflict. Failed payment attempts may be retried on the same order. A single pending attempt per order avoids concurrent checkout branches. Successful fulfilment locks the order and commits payment, entitlement, initial ledger movement, receipt and audit together. PostgreSQL uniqueness enforces one entitlement and one receipt per order. Duplicate success returns existing records; contradictory terminal outcomes are conflicts.

Keep provider simulation behind a narrow boundary invoking shared fulfilment services. Label every payment/receipt as simulated. No client-supplied trusted prices, real-provider callbacks or real financial transactions.

Guardians can act only for currently linked children, with scope rechecked on every operation. Administrators manage offers and review mock finance. This slice restricts financial endpoints to administrators and current guardians; coach allowance views belong to the later booking/roster workflow. Student financial access is not implied by their schedule/progress login.

Ledger fields distinguish available, reserved, consumed and expired quantities. Immutable movement references prevent duplicate grants and later duplicate reservations/transfers. Do not claim booking, expiry processing or renewal completed in 03a.

## Pending user decisions

Questions were sent asynchronously; no answer is assumed:

- Whether class eligibility requires matching recorded swimming level with reasoned administrator override, or is guidance only.
- Whether assigned coach recovery is limited to actual expired unused credits, with extra grants administrator-only, or always needs administrator approval.
- Whether credits must remain valid through lesson end, recurring enrolment is all-or-nothing, and renewal carries only unreserved unused credits to matching eligibility while existing reservations remain on the old package.
- Resolved2026-11-18: expiry is entered individually per purchase by an administrator, not inherited from an offer.

Booking eligibility and renewal/recovery interactions remain decisions for later slices. Stage03a purchase contracts are implemented without those booking restrictions; see contracts/openapi.json.

## Verification plan

Frontend checks cover selected-child isolation, clear simulation labels, retained input on failed checkout and absence of usable credits for pending/failed payments. PostgreSQL tests cover scope changes, immutable snapshots, idempotent order/attempt creation, concurrent outcomes, atomic rollback and uniqueness. Real-stack Playwright follows administrator offer creation, guardian failed payment/retry/success/duplicate outcome, then verifies exactly one receipt and entitlement. Later booking slices extend this to coach rosters and student schedules.

## Contract preparation after stage02d

Proposed records: versioned package offers, immutable purchase terms, payment attempts, credit packages, append-only credit movements and immutable simulated receipts. Enforce one credit package and receipt per purchase at the database level. A duplicate success is replay of a successful attempt, not a separate payment status. Preserve failed attempt history while permitting an explicit new attempt on the same order; a successful order cannot grant twice.

Implemented stage03a endpoint families (verification pending):

- Admin offer management: `GET/POST /package-offers`, `PATCH /package-offers/{id}` with version and active state. Guardians can read active offers and school policy defaults.
- Guardian checkout/history: `POST/GET /students/{id}/purchases`, `GET /purchases/{id}`.
- Explicit retry and simulation: create a payment attempt on a pending purchase, then submit pending/success/failed to that attempt. Exact request shapes are being refined around individual administrator-entered expiry.
- Scoped credit history: `GET /students/{id}/credits`; financial receipt read: `GET /receipts/{id}`.

Scope finance separately from generic `studentScope`: that helper deliberately allows coach/student access and is inappropriate for prices or receipts. Recheck current guardian links and student/account eligibility under locks coordinated with identity mutations. A former guardian must not complete checkout based only on a previously authorised purchase.

Use integer minor units with an explicit currency field; HKD is the proposed initial UI currency, not a confirmed multi-currency requirement. Do not trust client prices or credit quantities. Settlement takes trusted stored terms, locks the purchase, and commits the attempt outcome, entitlement, grant movement, receipt and audit together. The mock transport calls this application operation; future providers should call the same operation after their own verification. No real provider implementation is included.

The Sol proposal suggested making failed purchases terminal. Lead retains the already documented explicit retry-attempt model: it preserves purchase/attempt separation and lets the mock exercise retry without silently making a new purchase. This is an implementation choice, not a change to credit policies. Booking eligibility, recovery limits and renewal mechanics remain unresolved and must not be inferred from this design.
