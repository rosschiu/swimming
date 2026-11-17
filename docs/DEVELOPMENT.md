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
