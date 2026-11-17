# Stage 07c evidence in progress

Completed: PR13 merged as `7c028a41faf3594428a802e2a444f97be0e418da`. Astra's final PostgreSQL race regressions passed87.096s: twelve mutations plus retry replay reject admitted-but-disabled administrators, with unchanged database snapshots; scheduling queuing and actor-lock commit/rollback ordering pass. Against original98fe47e, all thirteen HTTP cases wrongly returned200/201, twelve mutations changed records/audit, and lock-order checks failed as expected. The owned baseline worktree was removed. Astra independently approved the correction with no introduced lock inversion.

Final cumulative Go/PostgreSQL race passed380.159s; frontend67/build, contracts and real-stack Playwright14(44.8s) passed. Both exact-head CI runs passed (44165609634:8m13s;44165700996:8m22s), including scale and restore verification. Merged main was clean, identical to tested head69cd7b1, and contracts passed. Earlier progress entries below are historical.

The isolated `stage/07c-admin-revocation` branch starts at `98fe47e`, the tested stage07b head. Stage07b's cumulative Go/PostgreSQL race suite passed in 306.720s; frontend67/build, contracts, vet, Playwright14 and both exact-head CI runs passed (44163216512: 7m26s; 44163253993: 7m0s). PR12 remains open while the cumulative security finding is corrected.

Sol implemented the shared actor-row lock and twelve transaction guards in five production files. Formatting, whitespace checks, Go build and vet passed. Astra's independent regression tests, lock-order review and cumulative checks are pending; no correction acceptance or merge is claimed.

The completed interim security scan `9e46c689-9b15-411e-89e9-d5096424701a` is bound to original revision `98fe47eb9d1ee90b346c58fa6a5a21c298f7af5f`. Its source remained unchanged. Independent audit coverage was reconciled against all119 tracked files; one medium-severity administrator-revocation finding was retained. Canonical report generation and completion succeeded. This static scan does not verify the correction, cover later feature work or establish production deployment security.

Lead cumulative frontend67/build and real-stack Playwright14 (44.8s) passed against the isolated correction worktree. Three notification mutation contracts now declare their already-enforced CSRF header; generated types were updated. Focused PostgreSQL regression and baseline reproduction remain in progress. No tests or checks were disabled.
