import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: { baseURL: 'http://localhost:18573', trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    { command: 'go run ./cmd/server', cwd: 'services/api', url: 'http://127.0.0.1:18083/api/v1/ready', env: { ADDR: '127.0.0.1:18083', APP_ORIGIN: 'http://localhost:18573', COOKIE_SECURE: 'false', TEACHING_EMAIL_ALLOWLIST: 'parent@swim.test,student@swim.test' }, reuseExistingServer: false },
    { command: 'npm run dev -- --host localhost', cwd: 'apps/web', url: 'http://localhost:18573', env: { VITE_TEST_DEMO_LOGIN: 'true' }, reuseExistingServer: false },
  ],
});
