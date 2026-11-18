# Local development and validation

Requirements: Go 1.26, Node.js 24 or newer, npm and PostgreSQL 17 (or Docker Compose). No public deployment is needed.

## Start

1. `docker compose up -d db mail` starts only this project's database and local email capture. If a dedicated local PostgreSQL already runs on port 55439, use it instead; do not stop unrelated services.
2. Copy `.env.example` to `.env`. Export its values into the shells running Go and Playwright. These sample credentials are exclusively for synthetic local data.
3. `npm ci` and `npm ci --prefix apps/web` install dependencies.
4. In `services/api`, run `go run ./cmd/migrate` then `ALLOW_DEMO_SEED=true go run ./cmd/seed`.
5. In `services/api`, run `go run ./cmd/server`.
6. In a second shell, run `npm run dev --prefix apps/web -- --host localhost` and open `http://localhost:18573`.

Synthetic accounts: `admin@swim.test`, `parent@swim.test`, `coach@swim.test`, `student@swim.test`, `other-parent@swim.test`. Password: `Swim-test-2026!`. Never seed these accounts into an environment containing real users. Seeding is explicit, not part of server startup.

## Checks

- `npm run contracts:check` regenerates frontend types and fails on tracked drift.
- In `services/api`: `go vet ./...` and `go test -race ./...` with DATABASE_URL pointing to a dedicated development/test PostgreSQL.
- `npm test --prefix apps/web -- --run` and `npm run build --prefix apps/web`.
- `npx playwright install chromium`, then `npm run test:e2e` with DATABASE_URL exported. Playwright starts its own API/web servers and fails if the test ports are already occupied and uses real PostgreSQL records.
- Browser failures retain screenshots and traces in ignored `test-results/` and `playwright-report/`; CI uploads them.

The application includes scoped student records, account invitations/recovery, family access links, audit history and school scheduling. Administrators use School setup to maintain venues, class types and coach travel time, then Schedule to create individual classes or weekly series of up to 93 days. Dates and times use Hong Kong time. Editing a series occurrence changes only that occurrence. Archived venues/types keep existing classes readable and prevent new assignments. Coaches see assigned classes; families see the catalogue. Bookings, credits and attendance are added in subsequent stages. The original HTML mockup is reference material and does not supply runtime records.

## Demo email

Local SMTP capture listens on `127.0.0.1:11025`; inspect messages at `http://127.0.0.1:18025`. Set SMTP_ADDR, SMTP_FROM and SMTP_TLS from .env.example. These messages do not leave the machine. The user authorised demo email notifications; external demo recipients and provider credentials are still pending. Do not substitute arbitrary real recipients.

## Coach availability

Coaches use Availability to submit venue/time/capacity proposals and edit or cancel their own entries. Administrators review proposals and configure the editing cutoff in School setup (72 hours by default). The server checks the old and new start times when editing. Cancelled records remain visible in history. Submitting availability does not create classes, reserve places or guarantee travel feasibility. Administrators open “View / schedule lessons” on a window to create one or more shorter lessons, choosing a class type, time and capacity. Created classes appear in Schedule and retain their source history. Editing or cancelling a window leaves its classes unchanged; administrators manage class corrections separately in Schedule. Creation rechecks coach overlap/travel rules and current window details. After an uncertain response, retry the same submission before changing its details. Migration 5 must be applied before running this version.

## Student progress notes

After applying migration 6, open a student profile to view shared progress notes. Assigned coaches and administrators can add feedback; guardians and students can read only records within their current access. The author coach (while still assigned) or an administrator can correct a note with a reason. Corrections retain the original author and are audited; stale versions require reloading and reviewing before retrying.

Notes are immediately shared with the student and linked family; there is no private/draft-note storage. The editor preserves text during recoverable request failures, but switching student/account views clears local drafts. A failed creation with an uncertain outcome offers the same submission again to prevent duplicate notes. Progress notes do not imply attendance, a completed skill milestone or a credit change.

## Skill milestones

Apply migration 7. Administrators define skills in School setup, including a name and optional assessment description. Editing or archiving requires a reason; archive preserves all history and prevents new achievements. The application supplies no curriculum or automatic swimming-level promotion.

Open a student profile to view skills and milestone history. Assigned coaches and administrators can record an achievement against the displayed definition. The current achievement's author, while still assigned, or an administrator can revoke it with a reason. Revocation keeps the assessed definition and history; a later achievement uses the newly reviewed definition. Guardians and students have scoped read-only access.

Achievements and revocations are shared immediately. If another user changes the milestone or definition, reload and explicitly review before submitting again. An uncertain result offers an exact retry; the screen then reloads authoritative state because a retried historical event may no longer be the current state. Times describe when records were written in Hong Kong time, not an inferred lesson or assessment date.

## Teaching notifications

Apply migration 8 before starting the server. New and corrected progress notes and milestone events create in-app notifications for currently linked guardians and students. Use Notifications to read updates, mark them read and open the student's current profile. Removing access hides those notifications; newly linked accounts receive future updates only. Email links to `/notifications` retain that destination through sign-in.

Teaching email is off by default. A parent or student can enable “Email shared teaching updates” for their own login address. The server must also include that address in `TEACHING_EMAIL_ALLOWLIST`; leave it empty until configuring named demo recipients. For a local synthetic demonstration, set it to `parent@swim.test,student@swim.test` with the Mailpit SMTP settings above, enable the parent preference, then share a progress note as the coach. Playwright sets those synthetic allowlist addresses for its own API process. Invitation/password-reset delivery is unchanged.

Emails contain a generic bilingual sign-in link, with no student details. The server worker retries transient failures, recovers expired leases after restart and exposes failures in the administrator's Notification deliveries screen. Administrators can retry a failed delivery with a reason after correcting the problem. “SMTP accepted” means the server accepted the email, not that it reached an inbox. Crash recovery can duplicate email. An already authorized email can arrive after access removal, but no further attempt is authorized after withdrawal and app access is rechecked. Opt-out/unlink suppress queued email permanently; re-enabling does not backfill it. Missing SMTP configuration remains retryable for opted-in, allowlisted recipients.

Local Mailpit proves actual SMTP handoff and captured message contents. External recipient/provider delivery and booking reminders remain separate work.

## Synthetic recovery verification

After the cumulative browser suite finishes and its application servers stop, follow [the restore runbook](RESTORE.md) to dump and verify restoration of the dedicated synthetic database. The opt-in check creates and removes its own temporary database, compares schema/data/sequences, and exercises restored account permissions. CI runs it after Playwright. It does not establish production backup retention or recovery objectives.

## Representative scale verification

With DATABASE_URL pointing to the dedicated synthetic test PostgreSQL, run from `services/api`:

```bash
SCALE_VERIFY_SYNTHETIC=true go test -race ./internal/app -run '^TestScaleVerification$' -count=1 -v
```

The test uses an isolated temporary schema and removes it afterward. It represents 10 coaches, 500 registered students, 20 venues and 50 classes per week across 13 weeks. It checks currently implemented API results, permissions, pagination and bounded concurrent operations; it starts no email worker and leaves public application data unchanged. CI runs this check separately from the ordinary suite. Logged durations and response sizes are observations from the test environment, not a production SLA or proof of future booking/payment/attendance performance. See [the stage plan](STAGE-07B-PLAN.md) for fixture assumptions and coverage.

## Packages and simulated purchases

Apply migration9 before using Packages and payments. Administrators maintain offers and prospective credit-policy defaults; parents choose a linked student and request a package. When expiry is enabled, an administrator assigns that purchase an individual expiry date in the purchase queue. The date includes the entire selected Hong Kong day. Parents review the assigned terms before starting a simulated payment. No-expiry purchases can proceed without date assignment.

The mock offers pending, failed and successful outcomes. A failed attempt grants nothing; retry creates another attempt on the same purchase. Repeating a successful outcome returns the existing receipt and credits. All payment and receipt screens are simulated; no money moves. Offer and policy changes affect new purchases, while existing purchases retain their terms.

Credit history shows grants and current available/expired quantities. Booking reservations, attendance and renewal/carry-forward arrive in subsequent stages. AI/voice is deferred beyond first delivery by the user's2026-11-18 decision.

## Durable expiry and credit recovery

Apply migration10. The server sweeps up to100 eligible expired packages on startup and once per minute. Credit-history and finance-credit reads also process the selected student's due packages, so the displayed expiry is recorded in the ledger. Expiry moves only unused available credits; reserved and consumed quantities remain intact.

Administrators and assigned coaches open a student profile's Credit history, select an expired package and enter quantity, reason and a new inclusive Hong Kong expiry date. Review and confirm the transfer. Recovery creates a linked descendant package and preserves the original receipt and source ledger. Partial recovery leaves the remaining expired quantity recoverable; a later recovery cannot reuse already-transferred credits. Guardians can read their children's history but cannot recover credits. Coach history excludes prices and receipt data.

For an uncertain response, retry the same recovery. A changed quantity/reason/date needs a fresh reviewed request; current scope is checked even on replay. System expiry entries identify the system rather than a staff actor. No additional credits or refunds are created by recovery.

## Exercise flexible booking with synthetic accounts

1. As an administrator, schedule a future class at the student's level and link the student to a guardian. Use mock checkout to give the student a paid package with an available credit.
2. As the guardian, open the student's profile and select **Book a lesson**. Select the class and the package to charge. Review the selection, then confirm. The lesson should appear under **Booked lessons**, and one credit should move from available to reserved. If the result is uncertain, **Retry same booking** checks the original request.
3. Sign in as the student's linked account and open the profile to view the lesson. Sign in as the assigned class coach, find the same class on the schedule, and select **View roster**.
4. As an administrator, open audit history to inspect the `booking.created` event. An administrator booking across a level mismatch must supply a reason. Once a class has reservations, it retains its time, venue and type. Administrators can reassign the coach or adjust capacity while preserving enough places for existing reservations.

## Seed the synthetic demo school

Use a database reserved for synthetic demo data. Confirm the target, take a database backup if it already contains demo activity, and run migrations before seeding. From `services/api` with that database's `DATABASE_URL` set:

```sh
go run ./cmd/migrate
ALLOW_DEMO_SEED=true go run ./cmd/seed -school
```

Omit `-school` to continue using the minimal seed. The richer seed installs a single fixture with a timetable from two weeks before through two weeks after the current Hong Kong week at first install. Repeat the same command to verify idempotency. It keeps the original dates, existing profiles, and later edits. The transaction rolls back if it detects a timetable collision or fails to install any of the records.

A fresh fixture has 20 students, 20 guardians, three coaches, and three five-session weekly series at Harbour Pool, Garden Pool and Seaside Pool. Each student has one simulated eight-credit purchase and receipt, a progress note, one attended lesson from the previous week and two reserved upcoming lessons. The fixture sends no email and creates no email deliveries.

- **Administrator** can browse all students, the timetable, package offer, and seeded audit entry.
- **Coach** Alex can open Harbour Pool rosters and the linked beginners. The previous week's roster shows attendance.
- **Guardian** can open Amy's bookings, credits, progress and simulated receipt. Her starting balance is five available, two reserved and one consumed.
- **Student** Amy can see her own timetable and progress. Ben and the other swimmers remain outside her account scope.

On an existing demo database, count fixture records by their `demo-` IDs where applicable. Do not expect whole-table totals to match a fresh fixture. One `demo.school.seeded` audit event records the fixture version and anchor week. A fresh disposable database is needed to demonstrate with a new anchor week.

### Add the demo lesson-change examples

After installing the synthetic school, confirm the database target and take a backup. Keep demo mail delivery off. Run the separate opt-in command from `services/api` against that same database.

```sh
ALLOW_DEMO_SEED=true go run ./cmd/seed -change-history
```

The fixture uses the original Hong Kong seed week and actual clock. It uses the first future Harbour Pool reservation for which the guardian request window remains open, with the replacement one day later at 17:00. If both candidate windows closed before the fixture started, the command stops without adding examples. Start with a fresh disposable school for a current anchor or plan an explicit extension to the existing demo schedule.

The command leaves Amy’s request pending, approves Anson’s move, and rejects Daisy’s request using the regular guardian and coach operations. The replacement class, requests, and decisions commit in separate transactions. If interrupted, rerun the same command while the outstanding examples are still eligible. It resumes matching records and reports a conflict when an unexpected booking, decision, or class edit needs review. When it finds the exact previously installed live examples, it records completion without rewriting them.

The separate `demo.change_history.seeded` audit marker records the class and request IDs. Replaying after completion makes no writes, even after the lessons pass or a demo visitor decides Amy’s request. Verify one marker, three seeded requests and at most one added class, with no additional accounts, credit movements, receipts, notifications, or deliveries. The ordinary seed without flags remains suitable for the minimal synthetic accounts.
