import { expect, test, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { expectContract } from './contract';

async function signIn(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(role + '@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const session = await (await page.request.get('/api/v1/auth/me')).json();
  return { Origin: 'http://localhost:18573', 'X-CSRF-Token': session.csrfToken };
}
const sql = (input: string, vars: Record<string, string>) => execFileSync('psql', [process.env.DATABASE_URL!, '-XqtA', '-v', 'ON_ERROR_STOP=1',
  ...Object.entries(vars).flatMap(([key, value]) => ['-v', `${key}=${value}`])], { input, encoding: 'utf8' }).trim();

test('a guardian cancels one lesson on time; the credit returns, the roster frees and coach and family are told', async ({ page }) => {
  test.setTimeout(90000);
  expect(process.env.DATABASE_URL, 'dedicated synthetic database required').toBeTruthy();
  let headers = await signIn(page, 'admin');
  async function switchRole(role: string) {
    await page.getByRole('button', { name: 'Sign out' }).click();
    headers = await signIn(page, role);
  }
  async function post(path: string, data: object, status = 201) {
    const response = await page.request.post('/api/v1/' + path, { headers, data });
    expect(response.status(), await response.text()).toBe(status);
    return response.json();
  }
  const suffix = Date.now();
  const date = new Date(Date.UTC(2600, 0, 1) + (suffix % 100000) * 86400000).toISOString().slice(0, 10);
  const venue = await post('venues', { name: 'Cancel pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await post('class-types', { name: 'Cancel lesson ' + suffix, level: 'Beginner' });
  const lesson = await post('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 1,
    startsAt: date + 'T10:00:00+08:00', endsAt: date + 'T11:00:00+08:00' });
  const offer = await post('package-offers', { name: 'Cancel credits ' + suffix, credits: 1, priceMinor: 10000, currency: 'HKD', active: true,
    policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: false, absenceRetainsCredit: true, illnessRetainsCredit: true } });

  await switchRole('parent');
  const purchase = await post('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: 'cancel-' + suffix });
  const attempt = await post(`purchases/${purchase.id}/payment-attempts`, { idempotencyKey: 'cancel-pay-' + suffix });
  await post(`payment-attempts/${attempt.id}/mock-outcome`, { outcome: 'succeeded' }, 200);
  const pack = async () => (await (await page.request.get('/api/v1/students/amy/credits')).json()).items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id);
  const booking = await post('students/amy/bookings', { classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion, creditPackageId: (await pack()).id, idempotencyKey: 'cancel-book-' + suffix });
  expect(await pack()).toMatchObject({ available: 0, reserved: 1 });

  await page.goto('/students/amy');
  await page.getByRole('button', { name: 'Change dates' }).click();
  await page.getByLabel('Bookings from date').fill(date);
  await page.getByLabel('Bookings to date').fill(date);
  await page.getByRole('button', { name: 'Load booked lessons' }).click();
  const row = page.getByRole('region', { name: 'Booked lessons' }).getByRole('listitem').filter({ hasText: kind.name });
  const preview = page.waitForResponse(r => r.url().endsWith(`/bookings/${booking.id}/cancellation`) && r.request().method() === 'GET');
  await row.getByRole('button', { name: 'Cancel lesson' }).click();
  expectContract('CancellationPreview', await (await preview).json());
  await expect(row.getByText('This is an on-time cancellation. The lesson credit goes back to your package.')).toBeVisible();
  const confirmed = page.waitForResponse(r => r.url().endsWith(`/bookings/${booking.id}/cancellation`) && r.request().method() === 'POST');
  await row.getByRole('button', { name: 'Confirm cancellation' }).click();
  const cancelled = await (await confirmed).json();
  expectContract('StudentBooking', cancelled);
  expect(cancelled).toMatchObject({ status: 'cancelled', cancellation: { timing: 'early', creditOutcome: 'retained' } });
  await expect(row.getByText('Cancelled', { exact: true })).toBeVisible();
  await expect(row.getByText('Cancelled on time · credit returned')).toBeVisible();
  expect(await pack()).toMatchObject({ available: 1, reserved: 0, consumed: 0 });

  // The guardian's generic email is handed to SMTP; the coach gets an in-app notice only.
  await expect.poll(() => sql(`SELECT string_agg(u.id||':'||COALESCE(d.status,'none'), ',' ORDER BY u.id) FROM notifications n JOIN users u ON u.id=n.recipient_id
    LEFT JOIN notification_deliveries d ON d.notification_id=n.id WHERE n.kind='booking.cancelled' AND n.source_id=:'booking'`, { booking: booking.id }), { timeout: 20000 })
    .toMatch(/^coach:none,parent:sent,student:(sent|suppressed)$/);

  await switchRole('coach');
  const roster = await (await page.request.get(`/api/v1/classes/${lesson.id}/roster`)).json();
  expect(roster.items).toHaveLength(0);
  await page.goto('/notifications');
  const notice = page.getByRole('article').filter({ hasText: 'A lesson was cancelled.' }).first();
  await expect(notice).toBeVisible();
  await expect(notice.getByRole('link', { name: 'Open schedule' })).toHaveAttribute('href', '/schedule');
});
