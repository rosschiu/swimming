# Local development and validation

Requirements: Go 1.26, Node.js 24 or newer, npm and PostgreSQL 17 (or Docker Compose). No public deployment is needed.

## Start

1. `docker compose up -d db` starts only this project's database. If a dedicated local PostgreSQL already runs on port 55439, use it instead; do not stop unrelated services.
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
- `npx playwright install chromium`, then `npm run test:e2e` with DATABASE_URL exported. Playwright starts the API/web servers if absent and uses real PostgreSQL records.
- Browser failures retain screenshots and traces in ignored `test-results/` and `playwright-report/`; CI uploads them.

The current foundation exposes authentication and scoped student records only. Later stages add operational features. The original HTML mockup is reference material and does not supply runtime records.
