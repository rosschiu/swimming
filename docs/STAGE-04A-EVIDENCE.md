# Stage 04a verification

The stage starts from verified main commit `0f8e029`. The user approved allowing assigned coaches to recover actual expired unused credits with a reason and a new expiry date. `gpt-6.1-sol` implemented the backend and frontend, and `gpt-6-astra` provided the initial independent testing and review. Current assignments use `gpt-6.2-sol` for coding and `gpt-6.2-astra` for independent testing and review.

Migration 10 preserves unique original purchase grants while allowing recovered packages linked to their source, immutable recovery records and null actors for system movements. Durable expiry replaces projected balances. The worker processes at most 100 expired packages with available credits per batch. Reads and recovery use the same operation to process expiry for the selected student. Credit queries use a package index.

Go compile and vet checks passed. Focused backend PostgreSQL race checks passed in 25.4 seconds and 11.9 seconds. Frontend typechecking, all 153 frontend tests and the production build passed. The lead reviewed transaction scope, credit conservation between source and destination packages, history preservation, worker selection and profile integration.

The initial real-stack Playwright run passed 18 of 19 journeys. The finance journey passed in a focused rerun after its heading assertion was updated to match the original purchase package heading. On corrected head `659a3f2`, all 19 real-stack browser journeys passed together in 59.5 seconds. The local cumulative Go race suite passed in 448.655 seconds on the earlier implementation head `2cf1a33`.

The browser fixture backdates one uniquely created synthetic package to exercise durable expiry. The recovery journey checks that the original receipt remains unchanged.

Independent 6.2 review found that PostgreSQL microsecond precision could change the timestamp on a replay, and that a slow recovery request could hold the identity lock while the server read the body. The revised handler decodes staff requests before acquiring the lock and rechecks authority inside the transaction. It normalizes timestamps to UTC microseconds. Five focused PostgreSQL recovery tests passed under Asia/Hong_Kong, including exact response replay and request decoding while the lock is held.

The independent reviewer reran both regressions on `659a3f2` under the Go race detector against PostgreSQL. They passed in 14.250 seconds, and the reviewer approved the corrected head.
