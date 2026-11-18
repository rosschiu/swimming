import { expect, test, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { expectContract } from './contract';

const queuePath = '/api/v1/admin/action-queue';

async function signIn(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(role + '@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const auth = await (await page.request.get('/api/v1/auth/me')).json();
  return { Origin: 'http://localhost:18573', 'X-CSRF-Token': auth.csrfToken };
}

test('administrator clears an expiry assignment and retries a failed delivery from the action queue', async ({ page, browser }) => {
  const headers = await signIn(page, 'admin');
  const beforeResponse = await page.request.get(queuePath);
  expect(beforeResponse.status()).toBe(200);
  const before = await beforeResponse.json();
  expectContract('AdminActionQueue', before);
  const name = 'Queue expiry ' + Date.now();
  const created = await page.request.post('/api/v1/package-offers', { headers, data: {
    name, credits: 2, priceMinor: 5000, currency: 'HKD', active: true,
  } });
  expect(created.status()).toBe(201);
  const offer = await created.json();
  const parentContext = await browser.newContext({ baseURL: 'http://localhost:18573' });
  const parent = await parentContext.newPage();
  const parentHeaders = await signIn(parent, 'parent');
  const pref = await (await parent.request.get('/api/v1/notification-preferences')).json();
  expect(pref.emailDeliveryAvailable).toBe(true);
  try {
    const order = await parent.request.post('/api/v1/students/amy/purchases', { headers: parentHeaders, data: {
      offerId: offer.id, offerVersion: offer.version, idempotencyKey: offer.id,
    } });
    expect(order.status()).toBe(201);
    const purchase = await order.json();
    expect(purchase.status).toBe('awaiting_expiry');
    expect((await parent.request.put('/api/v1/notification-preferences', { headers: parentHeaders, data: { emailEnabled: true } })).status()).toBe(200);
    const dsn = process.env.DATABASE_URL;
    expect(dsn, 'dedicated synthetic database required').toBeTruthy();
    // Insert a terminal SMTP failure so the test can exercise the real retry without disrupting the shared mail service.
    execFileSync('psql', [dsn!, '-Xq', '-v', 'ON_ERROR_STOP=1', '-v', 'id=' + offer.id], { input:
      `WITH notice AS (INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version)
        VALUES (:'id','parent','amy','progress.created',:'id',1) RETURNING id)
       INSERT INTO notification_deliveries(id,notification_id,status,attempts,cycle_attempts,completed_at,last_failure)
       SELECT id,id,'failed',5,5,clock_timestamp(),'smtp_unavailable' FROM notice;` });
    const queue = page.getByRole('region', { name: 'Action queue' });
    async function counts(purchases: number, failures: number) {
      const row = (label: string) => queue.getByRole('listitem').filter({ has: page.getByRole('link', { name: label, exact: true }) });
      await expect(row('Purchases awaiting expiry').locator('strong')).toHaveText(String(purchases));
      await expect(row('Failed email deliveries').locator('strong')).toHaveText(String(failures));
    }
    await page.reload();
    await counts(before.purchasesAwaitingExpiry + 1, before.failedEmailDeliveries + 1);
    await queue.getByRole('link', { name: 'Purchases awaiting expiry' }).click();
    await expect(page.getByLabel('Purchase queue')).toHaveValue('awaiting_expiry');
    const card = page.getByRole('article').filter({ hasText: purchase.id });
    await card.getByLabel('Expiry date (Hong Kong)').fill('2099-12-31');
    await card.getByRole('button', { name: 'Assign expiry' }).click();
    await expect(card).toHaveCount(0);
    await page.getByRole('link', { name: 'Students', exact: true }).click();
    await counts(before.purchasesAwaitingExpiry, before.failedEmailDeliveries + 1);
    await queue.getByRole('link', { name: 'Failed email deliveries' }).click();
    await expect(page).toHaveURL(/\/notification-deliveries\?status=failed$/);
    await page.reload();
    await expect(page.getByLabel('Delivery status')).toHaveValue('failed');
    const failures = await (await page.request.get('/api/v1/notification-deliveries?status=failed&limit=25')).json();
    const index = failures.items.findIndex((item: { id: string }) => item.id === offer.id);
    expect(index).toBeGreaterThanOrEqual(0);
    await page.getByRole('article').nth(index).getByRole('button', { name: 'Review retry' }).click();
    await page.getByLabel('Reason for retry').fill('Synthetic delivery retry');
    const retry = page.waitForResponse(r => r.url().endsWith('/' + offer.id + '/retry') && r.request().method() === 'POST');
    await page.getByRole('button', { name: 'Retry failed delivery' }).click();
    expect((await retry).status()).toBe(200);
    await page.getByRole('link', { name: 'Students', exact: true }).click();
    await counts(before.purchasesAwaitingExpiry, before.failedEmailDeliveries);
  } finally {
    expect((await parent.request.put('/api/v1/notification-preferences', { headers: parentHeaders, data: { emailEnabled: pref.emailEnabled } })).status()).toBe(200);
    await parentContext.close();
  }
});

for (const role of ['parent', 'student', 'coach']) {
  test(role + ' cannot read the administrator queue or follow the failed-delivery shortcut', async ({ page }) => {
    expect((await page.request.get(queuePath)).status()).toBe(401);
    await signIn(page, role);
    await expect(page.getByRole('heading', { name: 'Action queue' })).toHaveCount(0);
    expect((await page.request.get(queuePath)).status()).toBe(403);
    await page.goto('/notification-deliveries?status=failed');
    await expect(page.getByRole('heading', { name: 'Page unavailable' })).toBeVisible();
    expect((await page.request.get('/api/v1/notification-deliveries?status=failed')).status()).toBe(403);
  });
}
