# Web client

Requires Node 24+ and npm. From the repository root:

```sh
npm --prefix apps/web ci
npm --prefix apps/web run dev
npm --prefix apps/web test
npm --prefix apps/web run build
```

Open http://localhost:18573. Vite proxies `/api` to the Go server at `127.0.0.1:18083`; configure APP_ORIGIN as `http://localhost:18573`. Authentication uses server-issued cookies and CSRF tokens. No role-switching shortcut or client-side demo dataset exists.

Tests use HTTP fixtures to verify frontend behaviour; the root Playwright suite verifies actual backend/database integration. The test command disables Node's experimental Web Storage in worker processes so jsdom provides browser storage consistently.

The web client supports sign-in/out, scoped student profiles, administrator student editing, account invitations and recovery, role-matched student links, audit history, venue/class-type management, coach travel settings, and class schedules with bounded weekly series. Parent/student schedules are catalogues, not bookings. Invitation/reset tokens are removed from the URL after reading and are never persisted in browser storage. Later stages deliver booking, payment and assistant screens.
