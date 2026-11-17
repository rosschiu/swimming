# Go API — stage 01

Requires Go 1.26 and PostgreSQL. All commands below run from `services/api`.

```sh
export DATABASE_URL='postgres://swimming:swimming-local@127.0.0.1:55439/swimming?sslmode=disable'
export APP_ORIGIN='http://localhost:18573'
export ADDR='127.0.0.1:18083'
export COOKIE_SECURE=false # local HTTP only; defaults to true

go run ./cmd/migrate
ALLOW_DEMO_SEED=true go run ./cmd/seed
go run ./cmd/server
```

Migrations are explicit, transactional and repeatable. The API never creates demo accounts at startup. The explicit seed inserts missing synthetic records without overwriting changed existing records. Do not seed an operational database with these public credentials.

Synthetic accounts all use `Swim-test-2026!`:

| Email | Access |
|---|---|
| admin@swim.test | All students and student create/edit |
| parent@swim.test | Amy |
| coach@swim.test | Amy |
| student@swim.test | Amy, read-only |
| other-parent@swim.test | Ben |

The frontend should proxy `/api` to the API so browser requests remain same-origin. Mutations require an exact `Origin` matching `APP_ORIGIN`. Authenticated mutations also require `X-CSRF-Token`, obtained from login or `/auth/me`. Login sets an HttpOnly SameSite cookie; only a SHA-256 hash of its random secret is stored. Sessions expire after 12 hours. Login is throttled per peer IP and email; forwarded headers are not trusted. No cross-origin sharing is enabled.

## Validation

```sh
go vet ./...
go test -race ./...
```

With `DATABASE_URL` set, integration tests create isolated random schemas in that database and remove only those schemas. They never truncate application tables or the seed. CI fails if the database URL is missing; local tests explicitly skip integration cases when it is absent. Use a dedicated test database, not an operational database.

Coverage includes role/record scope, unauthenticated access, CSRF/origin, expiration/revocation, login errors/throttling, seed/migration repeatability, validation, optimistic conflicts, concurrent edits and rollback if audit insertion fails.

API contracts live in the repository's `contracts` directory. Stage 01 includes session auth and student read/create/edit, not invitation/recovery, guardian-link editing, venues or booking. Those remain later delivery stages. Audit writes are transactionally stored; the administrator history UI is a later stage.

## Stage 02a — accounts and family links

Migration 2 preserves the stage-01 accounts and adds explicit guardian/coach/self relationships, account versions, invitation/reset token hashes, and email-delivery metadata. Invited accounts have no password and cannot sign in until acceptance. Student accounts have a unique self link. Administrator account editing cannot remove the last active administrator.

Local SMTP capture configuration (before starting the server):

```sh
export SMTP_ADDR='127.0.0.1:11025'
export SMTP_FROM='school@swim.test'
export SMTP_TLS=none
```

For a real SMTP provider, use `SMTP_TLS=starttls` (default) or `tls`, with optional `SMTP_USER` and `SMTP_PASSWORD`. TLS certificates are verified. Authentication over explicitly unencrypted SMTP is rejected. Provider credentials and real-recipient validation remain outside the local test evidence.

Invitations expire after 48 hours; reset links expire after one hour. Passwords must contain 12–72 UTF-8 bytes and cannot be all whitespace. Links are delivered to `/accept-invitation?token=...` and `/reset-password?token=...` on `APP_ORIGIN`. Tokens are single-use; invitation resend/reissue invalidates earlier links. Revoked invitations can be explicitly reissued by administrators. Password reset revokes existing sessions, including a tested race with a login using the old password.

No raw token or email message body is stored in the database, audit history or API responses. Delivery metadata records `pending`, `sent` (accepted by SMTP, not proof of inbox delivery), or `failed`. Invitation delivery runs after its transaction commits. Reset delivery runs asynchronously so SMTP timing/failure does not reveal whether the account exists. If interrupted, pending metadata remains truthful: resend the invitation or request another reset, generating a new token rather than replaying a stored secret. No general-purpose background queue is claimed by this slice.

The API now provides administrator user/invitation lists, invite/resend/revoke, account name/activation editing, student relationship maintenance and paginated audit history. Public invitation acceptance/reset endpoints require the allowed Origin; authenticated writes additionally require CSRF. User roles/emails remain immutable in this slice. Login throttling counts failed credentials per peer/email and per peer aggregate; successful sign-ins do not consume the limit.

Tests include real local SMTP capture, failed delivery, token expiry/replay/reissue, reset non-enumeration, self-link uniqueness, relationship revocation, last-administrator concurrency, optimistic account edits, audit rollback and reset-versus-login serialization.

## Stage 02b — scheduling

Migration 3 adds venues, class types, sessions, bounded weekly class series and singleton scheduling settings. No enrolment or roster is invented here; guardian/student views show the class catalogue, and coaches see their own assigned sessions. Actual student bookings follow in stage03.

Administrator scheduling supports Hong Kong inclusive date filters (1–93 days), five school operating regions, venue/type archive, individual session editing and atomic weekly series creation. Weekly inputs use ISO weekdays (Monday=1, Sunday=7) and HH:mm Hong Kong local time. An occurrence edit preserves its series ID without changing siblings; there is no bulk-series editing endpoint.

The default different-venue travel gap is 30 minutes, configurable from 0–1440 by administrators. Same-coach overlap is always prohibited; same-venue back-to-back classes and different-coach parallel venue classes are allowed. The travel rule applies in both temporal directions and across midnight. A single transaction advisory lock serializes scheduling changes and settings changes. Row locks on reference records serialize creation with coach disabling or venue/type archival. Increasing the gap rejects conflicts among not-yet-ended classes, including ongoing lessons. Historical ended sessions are not retroactively revalidated by a settings update.

Archiving a venue/type prevents new assignments but does not cancel existing sessions or hide them from the catalogue. Editing an existing session may retain unchanged archived venue/type references; assigning a different archived reference is rejected. An inactive coach must be reassigned to an active coach before any session update. Disabling a coach prevents new assignments and login; existing sessions remain for administrator reassignment in later operational flows. Mutations and their audit events commit together. Stale versions return 409.

Tests cover HK date boundaries, bounded recurrence, role scope, filters, travel equality/both directions/midnight, same-venue parallelism, overlap, atomic series rollback, isolated occurrence editing, concurrent settings/session/series writes, active-reference races and audit rollback.

Stage 02c adds coach planning availability at `/api/v1/availability` and
`/api/v1/settings/availability`. Migration 4 runs through the existing migrate
command. Coaches create, edit and cancel their own submitted slots; administrators
review all slots and configure `lockHours` (default 72, range 0–8760), but cannot
change coach submissions. Parents and students have no availability access.
Lists require inclusive Hong Kong `from`/`to` dates spanning at most 93 days.

The current setting and server time govern each write after scheduling and
eligibility locks are acquired. Equality at the cutoff is allowed; edits require
both the existing and proposed start to satisfy it. Cancelled slots cannot be
edited. Responses calculate `editable` for the requesting coach; administrators
always receive false. Settings changes affect future edit decisions immediately.
Writes use optimistic versions and transactional audits. An unchanged archived
venue can remain on an existing slot, but new assignments require an active venue.

Submitted slots cannot overlap the coach's other submitted slots or assigned
classes (touching endpoints are allowed). They are planning data, not reservations:
there is no travel-gap enforcement across slots, automatic class creation, or
publication flow in this stage. Class scheduling does not consume or modify slots.


## Stage 05c — teaching notifications

Migration 8 adds per-recipient teaching notification history, an opt-in user email
preference and durable delivery jobs. New/corrected progress notes and milestone
transitions create notifications in the same transaction as their source and audit.
Only currently active guardian/self-linked recipients can list, count or mark their
notifications read; source endpoints recheck access again when opened.

Teaching email defaults off. `TEACHING_EMAIL_ALLOWLIST` is a comma-separated list
of exact login addresses (normalized to lowercase); empty disables teaching email.
It is an additional demo boundary, not a substitute for individual opt-in. Missing
SMTP configuration is a retryable transport failure for eligible opted-in addresses;
it does not permanently suppress their delivery. Configure
only named authorized demo recipients. Invitation/reset delivery is unaffected.
Local Mailpit captures SMTP handoff; external inbox delivery requires separately
provided recipients and SMTP access.

The server explicitly runs one cancellable worker; constructing a handler does not
start jobs. Claim transactions authorize current account/link/preference/address,
lease for 60 seconds, then release all locks before bounded SMTP work. Generic
bilingual messages contain only the configured `APP_ORIGIN` plus `/notifications`,
no student identifiers or teaching text. Authorization occurs at claim commit:
a previously authorized generic email may arrive after access is revoked. Pending
and retry deliveries are permanently suppressed on unlink, disable or email opt-out.
Re-link/re-enable does not revive them. Already leased attempts retain a suppression
marker: successful handoff may finish sent, but failure or lease expiry becomes
suppressed and never authorizes another attempt. Roles and login email have no mutation API;
claims still validate their current values. Any future role-changing transaction
must perform the same suppression hook.

SMTP acceptance means `sent`, not inbox receipt. A crash after handoff can duplicate
a delivery; stable Message-ID correlates it but does not promise deduplication.
Expired leases are reclaimed, with five attempts per retry cycle; delays are 30,
60, 120 and 240 seconds. A fifth expired lease becomes failed. Administrators can
review status and retry a failed eligible delivery with current version and reason;
retry resets cycle count, retains lifetime count and is audited atomically. Failure
categories are sanitized; raw provider responses and teaching content are not stored.
Booking reminders and external real-demo delivery remain separate acceptance work.
