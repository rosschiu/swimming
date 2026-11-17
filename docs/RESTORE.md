# Local synthetic backup and restore verification

This drill proves an actual PostgreSQL custom-format dump can restore the currently implemented application's public schema and records into a fresh database. It also checks restored authentication and family access boundaries. It is not a production recovery procedure, scheduled backup service, hosting decision or completion of stage 07.

## Prerequisites

Use only a dedicated **local, synthetic** application database. `DATABASE_URL` must name that database on a loopback host, not `postgres`, `template0` or `template1`. The explicit `RESTORE_VERIFY_SYNTHETIC=true` setting confirms the operator has checked that the entire source database contains synthetic data. Recognizing demo accounts cannot prove there are no real records elsewhere.

The local database role needs access to `pg_control_system()`, all public tables/sequences, and permission to create/drop databases. The existing local Compose PostgreSQL administrative role supports this. Do not grant additional privileges to a production role for this test.

Apply the current migrations and seed synthetic accounts using [DEVELOPMENT.md](DEVELOPMENT.md) before the drill. Run the cumulative real-stack Playwright journeys to populate representative teaching records. The drill requires the five active demo accounts with their documented roles/password, Amy and Ben, their scoped guardian/student access, and nonempty migration, audit, progress, milestone, notification and delivery history, including Amy's progress and milestones. It refuses a sparse seed-only database; it does not create fixture records or apply migrations itself.

**Stop the application's API, notification worker and Playwright processes before verification.** Keep the source public schema quiescent until verification ends. Stop only processes belonging to this local application. PostgreSQL remains running. Concurrent Go integration tests in their independent `swim_test_*` schemas are compatible with the public-only dump. Sequences are not frozen by PostgreSQL MVCC snapshots, so an exported snapshot does not make public writes safe during the drill.

## PostgreSQL clients

`pg_dump`, `pg_restore` and `psql` must all match the server's major version. The application currently uses PostgreSQL 17. The verifier rejects version mismatches before creating a target.

If the host has matching clients, leave `RESTORE_VERIFY_PG_CONTAINER` unset. Otherwise select an **existing** PostgreSQL container with matching clients, for example:

```bash
export RESTORE_VERIFY_PG_CONTAINER=swimming-test-db
```

For the Compose development database, use its existing container ID from `docker compose ps -q db`. In GitHub Actions, set this variable to `${{ job.services.postgres.id }}` for the existing PostgreSQL service. Docker access is required for this option. The verifier neither installs clients nor starts a container.

Container clients connect to `127.0.0.1:5432` inside that container. Source database/user/password come from the parsed `DATABASE_URL`; local client connections disable TLS. Before target creation, the verifier compares the database name, OID and PostgreSQL cluster identifier reported by that client with the original Go connection. Selecting the wrong container fails this check. Target identity is checked again before restoration.

The generated target's empty default public schema is dropped without `CASCADE` after identity checks, so the archive can recreate that schema. This operation is confined to the newly created target and refuses a nonempty inherited schema.

The drill supports libc locale providers. It creates the target from `template0` with the source encoding, `LC_COLLATE` and `LC_CTYPE`, and checks those properties. ICU or other locale providers are rejected. Materialized views, foreign tables and PostgreSQL large objects are also rejected rather than silently excluded from data verification. Supporting these requires extending and independently testing the drill first.

## Run

Export `DATABASE_URL` from the local development environment without echoing it or enabling shell tracing. From the repository root:

```bash
cd services/api
RESTORE_VERIFY_SYNTHETIC=true go test ./internal/app -run '^TestRestoreVerification$' -count=1 -v
```

Run the independent verifier guard tests without database opt-in:

```bash
go test ./internal/app -run '^TestRestore' -count=1
```

No caller-supplied restore target is accepted. The verifier creates a random `swim_restore_*` database, never restores into an existing database, and never uses `--clean` or a forced database drop.

## What a passing drill establishes

The verifier holds a repeatable-read, read-only source transaction and exports its snapshot for `pg_dump`. A custom-format archive streams into a private host temporary directory outside the repository (directory mode 0700, archive mode 0600). Credentials are passed through subprocess environment variables, never command arguments. Raw client errors and record contents are withheld from test output.

`pg_restore --exit-on-error --single-transaction` restores into the newly created database. Before any target login writes, verification compares:

- Every public table's complete row multiset, including duplicates and all columns, using deterministic JSON row hashes and counts. Equal row counts alone do not pass.
- The complete schema-only SQL emitted by the same `pg_dump` client, including defaults, indexes, constraints, identity definitions and sequence configuration. Only the matching outer random `\restrict` / `\unrestrict` tokens are normalized; comments and SQL bodies remain intact.
- Every public sequence's `last_value` and `is_called`, with a second source check to detect sequence movement.

Database reads explicitly use UTC, ISO dates, PostgreSQL interval formatting, consistent floating-point output and the public search path. These settings avoid differences caused by database defaults without changing source records.

The test deliberately alters target row content without changing its count, a default, a constraint and sequence state, and requires each mismatch to be detected. It checks malformed archive rejection and cleanup after a failed restore attempt.

The restored application handler then authenticates the demo parent and student, reads Amy's student and teaching history, and rejects access to Ben. It uses the real application handler without starting the notification worker, applying migrations or seeding. Login sessions are written only to the restored target. Fresh source reads and a fresh source schema dump verify the source public records, schema and sequences remain unchanged.

This public-only recovery intentionally excludes cluster roles, ownership and ACL restoration (`--no-owner --no-privileges`); restored objects belong to the executing local role. It also excludes unrelated test schemas, mail capture, files outside PostgreSQL and future application features. Re-run and extend acceptance evidence as later booking, entitlement, payment, attendance and assistant state is implemented.

## Cleanup and failures

Normal cleanup closes target connections, uses a new bounded context to recheck the original cluster/database identity and the created target OID, then drops only that owned target. It removes the private archive directory. It never drops the source, terminates other database connections or uses `FORCE`. Tests assert the target and archive are gone after successful cleanup.

A failed identity check refuses mutation. A cleanup failure fails the test and identifies the generated target requiring inspection. If a fresh bounded identity lookup fails immediately after database creation, cleanup refuses that unpinned target rather than risking an unsafe drop. An abrupt process termination can likewise leave a `swim_restore_*` target or a `swimming-restore-*` directory beneath the operating system's temporary directory. Before manual removal, confirm the target belongs to this invocation, is not the application source, and has no connections that need preserving. Never use a wildcard database drop or remove another run's temporary directory.

Retain only the test command, revision, client/server versions and pass/fail outcome as stage evidence. Do not upload archives, connection URLs, passwords or raw SQL row data. This drill does not establish production backup retention, encryption, off-host storage, recovery-time/recovery-point objectives or recoverability of features not yet implemented.
