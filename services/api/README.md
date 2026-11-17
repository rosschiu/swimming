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
