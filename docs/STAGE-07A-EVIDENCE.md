# Stage 07a evidence in progress

Branch stage/07a-restore-verification starts at verified main `9c0a14d`. Sol implements restore helpers/runbook; Astra independently owns tests/review. No restore success, PR or stage acceptance is claimed yet.

Local cumulative frontend62/build and contract checks passed. Existing real-stack Playwright14 passed in43.1s, refreshing the synthetic business fixtures. After Playwright completed, process inspection found no test API or Vite server; source public data is quiescent before restore tests. No new restore acceptance is claimed yet.

Lead/Astra review tightened database session normalization, fixture credentials/roles, generated-target identity checks, and cleanup reporting. Actual execution caught PostgreSQL OID JSON typing and an empty public-schema collision; Sol fixed both. The helper verifies both Go/client target identities before dropping only the fresh target's empty public schema without CASCADE. Unsupported materialized views, foreign tables and large objects fail preflight; no unsupported data recovery is silently claimed.

Astra's actual rich PostgreSQL17 restore race test passed in12.167s: truncated-archive atomic failure, cleanup after failed restore, row/default/constraint/sequence/is_called mismatch detection, complete current schema/data equality, restored parent/student teaching reads and Ben denial, fresh source preservation, and no leftover temporary targets. A final focused rerun is covering the last external-client session-normalization edit. Go vet/diff check passed. Cumulative Go race and independent final review remain pending; no PR/merge acceptance yet.

Astra independently APPROVED the final stage. Latest-helper focused actual restore race test passed12.112s; the earlier all-TestRestore race run passed12.167s including guards. Source/target equality, failure detection, account permissions and cleanup all passed. Declared limits remain local synthetic libc databases, excluded roles/ownership/ACLs, and possible owned artifacts after abrupt termination or inability to pin a newly created database. Cumulative local Go race and exact-head CI remain release gates.
