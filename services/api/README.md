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
