# Stage 02b: school scheduling

In progress; no completion claim yet.

Scope reaches migration 3, Go scheduling services/routes, OpenAPI/generated types, React school setup and schedule screens, frontend/Go tests, real-stack Playwright and operating documentation. Existing identity and family access must remain intact. Booking rosters and individual booked schedules arrive in stage 03, using these persistent sessions.

Acceptance plan:
- Administrators create/edit/archive venues and class types, schedule individual classes, generate bounded weekly series and edit one occurrence without changing its siblings.
- Hong Kong dates and times are explicit; date ranges are inclusive and limited to 93 days. Capacity is positive per session.
- Coaches see assigned classes only. Parents/students see a class catalogue without other students' information.
- A coach cannot overlap classes. Different venues require an administrator-configurable travel gap, default 30 minutes, including exact boundary tests in both directions. Parallel classes with different coaches at one venue are allowed; lane capacity is managed manually.
- Settings changes reject conflicts in future schedules. Transactional locking prevents concurrent scheduling/settings operations from violating these invariants. Failed recurring creation leaves no partial series or audit writes.
- Archived venues/types and inactive coaches cannot receive new scheduling. Existing history remains readable.
- Versioned edits reject stale writes; mutations preserve audit history and enforce administrator permissions/CSRF.
- Real UI journey creates venue/type/series, reads the same schedule as coach and catalogue as parent, and verifies failed conflicts preserve records. Cumulative frontend/backend/E2E and contract gates must pass before independent review and PR merge.

Implementation ownership: backend agent services/api, frontend agent apps/web except generated API types, lead contracts/root E2E/docs. Independent reviewer does not implement this stage.

Integration evidence so far: migration 3 applied to existing local data; all 9 cumulative real-stack Playwright tests passed, including SMTP account flows and scheduling UI/API cross-role journeys. AJV now validates date/date-time formats as well as response shapes. Independent review requested explicit overnight end-date display and edits preserving unchanged archived references; fixes and final regression gates remain pending.

Final integrated frontend tests: 29 passed; production build passed. Final cumulative Playwright: 9 passed against real PostgreSQL/Go/SMTP, including contract date formats. Independent reviewer rechecked fixes and approved subject to final backend race suite and CI. Backend non-race suite passed after the archive correction; race/CI pending.
