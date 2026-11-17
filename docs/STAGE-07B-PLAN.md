# Stage 07b: representative current-feature scale checks

This slice tests implemented workflows at the stated initial scale. It does not complete operational readiness or test bookings, payments, attendance or AI before those features exist. It adds no benchmark framework or production optimization without evidence.

## Fixture and ownership

Sol owns a minimal test-only fixture helper extending the existing isolated-schema testDB. Astra owns API acceptance and focused frontend tests and independently reviews the helper. Lead owns CI integration, measurements and release evidence. No public-database mutations or external email.

Count seeded records toward exactly 10 coaches, 500 registered students and 20 venues. Assign 50 students per coach. One guardian and one student login per student are explicit synthetic fixture assumptions, not a claim about the real school's family structure or weekly attendance. Reuse a generated password hash for fixture setup, then use real authentication for representative roles.

Populate 50 classes per week over 13 weeks (650 sessions), with valid coach/time/venue relationships and recurring series. Include enough scoped notification and delivery rows to exceed a 100-row page for an individual recipient/filter and more than 100 audit events. Use tied notification timestamps to exercise ID tie-breaking. Fixture assertions must independently check cardinality and valid assignments.

## Acceptance

- Verify exact directory IDs at scale: admin500, coach50, guardian/self only the linked child. Check first and last records and cross-scope denial.
- Verify week and maximum93-day schedule results and coach/venue/region filters against independently derived expected IDs. Parent/student classes are currently a catalogue, not an own-booking schedule; this stage must not claim otherwise.
- Walk actual cursor contracts to exhaustion without duplicates or omissions: numeric audit afterId and timestamp-plus-ID notification/delivery cursors. Verify scoped unread counts.
- Run ten bounded preauthenticated clients with a synchronized start and collect errors, durations and response bytes. Check correct mixed reads, competing writes to one scheduling slot (one success), and repeated identical progress submission (one source/audit/fan-out set).
- Focus frontend checks on 500-student name/level search including the last record, all20venues/10coaches selectable, schedule date/filter behavior, and actual Load more controls for notifications/deliveries/audit. Schedules have no pagination.
- Keep cumulative Go/PostgreSQL race, frontend/build/contracts, real-stack Playwright and restore verification green.

Record revision, runtime/database versions, fixture cardinalities, connection-pool settings, request count/concurrency, aggregate durations/bytes and unexpected errors. These are observations, not an agreed SLA, production capacity guarantee or browser performance measurement. Existing global locks, full-list directories and per-series/per-skill lookups are candidates for measurement, not automatic optimization tasks.
