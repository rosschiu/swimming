# Stage03a evidence in progress

Branch stage/03a-purchases-credits starts from verified main2d1fca1. User approved individual administrator-entered expiry per purchase and deferred AI/voice beyond first delivery on2026-11-18. No stage completion or merge is claimed yet.

Sol implemented backend/OpenAPI and frontend with separate file ownership. Astra approved the shared contract and independently owns backend, frontend and browser tests. Lead inspected integrated finance transactions, migration, navigation and screens. Review changes include complete policy validation, safe request keys, a transport-independent fulfillment service, administrator student names/ledger access, exact HKD decimal input and unique expiry-field labels.

Migration9 introduces versioned policy defaults/offers, frozen purchase terms, payment attempt history, credit packages, immutable ledger movements and simulated receipts. Current scoped permissions and identity locking protect replay as well as initial actions. Success commits all financial state atomically; uniqueness prevents repeated credits/receipts. First settlement checks expiry; earlier successful outcomes remain replayable after expiry.

Local compile/vet and production build passed. Cumulative frontend144 tests passed; the final Packages-only suite passed12 tests including duplicate expiry-label coverage added afterward. Independent finance PostgreSQL race tests passed in28.8s. Generated TypeScript contracts reproduce exactly; existing API path/schema definitions are semantically unchanged. All17 previous real-stack Playwright journeys passed; the new finance journey passed separately in5.6s after correcting its navigation selector and fixing nullable-policy OpenAPI schemas. Combined browser coverage is18/18; a single clean cumulative browser run remains for CI. Astra independently approved the integrated stage with no open blockers. Cumulative Go/PostgreSQL race passed420.329s. Both CI runs44228549128 and44228518665 passed, including a complete browser run, scale and synthetic restore.

Limitations: mock payment only; lifecycle expiry is currently reflected in the read balance, with durable processing and booking/attendance/renewal/recovery implemented in later stages. AI/voice is deferred by user. No external deployment or real financial transaction.

Merged [PR #15](https://github.com/rosschiu/swimming/pull/15) as `0f8e0295dd9a7826e1eb0ba68d8eaca59e2181c3`. Clean merged main matched reviewed/tested head `ad05d6d15781bb181497f30471f5fe08bbfe0c66`; generated contracts passed after merge.
