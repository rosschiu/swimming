# Stage04a: durable expiry and scoped recovery

Bring forward the now-approved credit recovery workflow while booking configuration alternatives are pending. Base is verified main0f8e029; branch stage/04a-credit-expiry-recovery. User approved assigned coaches recovering only actual expired unused credits, with reason/new expiry; extra grants remain administrator-only and are not part of this slice.

## Workflow and integrity

Durable expiry moves only available credits into expired after the inclusive Hong Kong expiry date. Reserved and consumed credits do not move. A bounded startup/periodic sweep and the same on-demand operation on history, finance-credit reads and recovery make expiry persistent. Repeated processing at zero available does nothing; later releases can be expired without a one-time lifetime key preventing it.

An assigned coach or administrator chooses an expired source package, quantity, reason and new expiry. Atomic recovery debits remaining expired quantity and grants the same quantity to a new descendant package; it never changes the original purchase/receipt. Descendants retain purchase provenance with explicit source-package lineage. A partial unique purchase index preserves exactly one original purchase package. Recovery records and paired movements retain audit history. System expiry uses an explicit null movement actor, never a fabricated administrator.

Current guardian/admin/assigned-coach history uses a dedicated DTO without purchase prices, receipt details or financial terms. General finance endpoints retain their existing role gates. Recheck active actor and current assignment/guardian scope under identity transaction coordination, including idempotent replay. Positive recovery quantity cannot exceed the current expired balance. Exact actor-key replay returns its original result; different payload conflicts. No extra-grant implementation or invented booking policy.

## Ownership and acceptance

Sol backend owns migration10, services and OpenAPI/generated types; Sol frontend owns profile history/recovery and compatible package ledger display. Astra independently owns tests/review; lead owns integration/docs/releases. Shared types are agreed before UI coding.

Tests: HK boundaries, no-expiry packages, repeated sweeps, available-only expiry, later available releases, partial/full/excess recovery, concurrent over-recovery, exact/conflicting retries, second-generation recovery, revoked/unassigned/disabled coaches, guardian isolation, financial field exclusion, transaction rollback and unchanged receipt. Real-stack browser follows purchase/expiry/recovery and guardian history. Synthetic time control must remain test-only; no production backdate endpoint. Cumulative backend/frontend/contracts/browser/CI and independent review gate merge.
