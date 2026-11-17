# Stage 02e: availability edit guidance

Astra's retrospective review of merged PR #6 found that the availability edit form omitted the agreed explanation: editing a planning window leaves scheduled lessons unchanged. The cancellation and linked-lessons views already explain this.

Sol added the explanation directly below the edit heading in English and Traditional Chinese. Astra added two regressions covering visibility while editing and absence when creating a window. No backend, migration or API contract changes.

Astra's retrospective backend audit found no blocking defect. It ran five targeted PostgreSQL race tests (19.424 seconds) covering allocation, retries, rollback, cancellation ordering, cutoff and coach eligibility, plus three allocation frontend tests. The original PR's cumulative suites and both CI runs had passed.

For this follow-up, Astra approved the copy and focused tests. Lead build verification caught unsupported `exact` options in the new Testing Library queries; Astra corrected them and reran 13 focused tests plus the TypeScript/production build successfully. Lead cumulative frontend run passed all 42 tests. Cumulative CI remains required before merge.

Model roles for subsequent work: `gpt-6.1-sol` development, `gpt-6-astra` independent review/testing, as explicitly selected by the user.
