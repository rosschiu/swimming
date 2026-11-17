# Stage 07a: synthetic backup and restore verification

This slice proves recovery of the current application schema and records into a fresh temporary database. It does not complete operational readiness or the overall web goal. Packages, bookings, lesson lifecycle and provider-backed AI remain required.

## Scope and ownership

- Sol implements test-only restore helpers and the operator runbook.
- Astra independently implements acceptance tests and reviews safety/correctness.
- Lead owns CI integration, stage records, cumulative validation and release.
- No runtime API, migrations, frontend changes, dependencies, production backups, hosting or external email.

Use an explicitly opted-in, dedicated synthetic database after application writers stop. Keep one read-only repeatable-read snapshot open for source comparison and pg_dump. Sequence state requires quiescence in addition to the snapshot. Compare source and subprocess database/cluster identities and client/server major versions before creating anything. Host PostgreSQL16 clients cannot dump this PostgreSQL17 database; an explicit adapter may use clients inside the existing test container.

Create only an internally generated temporary database, with equivalent database creation properties or a clear rejection. Pin its identity, register cleanup immediately after creation, restore transactionally, and never migrate, reseed or start a notification worker there. Cleanup uses a fresh bounded context and only the database created by that invocation. Archive files are private and removed; credentials and record contents stay out of command arguments and logs.

Compare all public table row multisets, schema definitions and sequence state before authenticated application checks. Ownership/grants are excluded by the dump/restore flags. Verify restored parent/student access to Amy and denial of Ben. The source must remain unchanged.

## Acceptance

Actual rich E2E database dump/restore, data/schema/sequence equality, authenticated scoped reads and cleanup must pass. Tests must reject missing opt-in/fixtures, incompatible clients and wrong connection identities; detect altered restored records/schema/sequences; exercise corrupt archives and failures after database creation; verify private artifacts and source preservation. Run the existing cumulative backend/frontend/contracts/Playwright suite in CI, with restore verification after E2E servers stop.

A dedicated synthetic fixture and opt-in flag are prerequisites, not proof that arbitrary source data is synthetic. This check is not a production disaster-recovery plan and does not establish retention, backup scheduling, recovery times or restoration of database roles/grants.
