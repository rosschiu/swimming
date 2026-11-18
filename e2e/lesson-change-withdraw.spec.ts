import { expect, test, type Page } from '@playwright/test';
import { expectContract } from './contract';

async function login(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(`${role}@swim.test`);
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const session = await (await page.request.get('/api/v1/auth/me')).json();
  return { Origin: 'http://localhost:18573', 'X-CSRF-Token': session.csrfToken };
}

test('a guardian withdraws a pending lesson change and the coach queue and history agree', async ({ page }) => {
  test.setTimeout(90000);
  const suffix = Date.now();
  const date = new Date(Date.UTC(2350, 0, 1) + (suffix % 30000) * 86400000).toISOString().slice(0, 10);
  let headers = await login(page, 'admin');
  async function post(path: string, data: object, status = 201) {
    const response = await page.request.post('/api/v1/' + path, { headers, data });
    expect(response.status(), await response.text()).toBe(status);
    return response.json();
  }
  const venue = await post('venues', { name: 'Withdraw pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await post('class-types', { name: 'Withdraw lesson ' + suffix, level: 'Beginner' });
  const lesson = (start: string, end: string) => post('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 4,
    startsAt: `${date}T${start}:00+08:00`, endsAt: `${date}T${end}:00+08:00` });
  const original = await lesson('09:00', '10:00');
  const target = await lesson('11:00', '12:00');
  const offer = await post('package-offers', { name: 'Withdraw credits ' + suffix, credits: 1, priceMinor: 10000, currency: 'HKD', active: true,
    policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: true, absenceRetainsCredit: true, illnessRetainsCredit: true } });
  await page.getByRole('button', { name: 'Sign out' }).click();

  headers = await login(page, 'parent');
  const purchase = await post('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: 'withdraw-' + suffix });
  const attempt = await post(`purchases/${purchase.id}/payment-attempts`, { idempotencyKey: 'withdraw-pay-' + suffix });
  await post(`payment-attempts/${attempt.id}/mock-outcome`, { outcome: 'succeeded' }, 200);
  const credits = await (await page.request.get('/api/v1/students/amy/credits')).json();
  const pack = credits.items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id);
  const booking = await post('students/amy/bookings', { classId: original.id, classVersion: original.version, classTypeVersion: original.classTypeVersion, creditPackageId: pack.id, idempotencyKey: 'withdraw-book-' + suffix });
  const change = await post(`students/amy/bookings/${booking.id}/change-requests`, { targetClassId: target.id, targetClassVersion: target.version, idempotencyKey: 'withdraw-change-' + suffix });

  await page.getByRole('link', { name: 'Lesson changes', exact: true }).click();
  const row = page.getByRole('listitem').filter({ hasText: target.startsAt.slice(0, 10) }).filter({ hasText: '11:00' }).filter({ hasText: kind.name });
  await row.getByRole('button', { name: 'Withdraw request' }).click();
  const withdrawn = page.waitForResponse(r => r.url().endsWith(`/lesson-changes/${change.id}/withdraw`));
  await row.getByRole('button', { name: 'Confirm withdrawal' }).click();
  const body = await (await withdrawn).json();
  expectContract('LessonChange', body);
  expect(body.status).toBe('withdrawn');
  await expect(page.getByText('Request withdrawn. The original lesson stays booked.')).toBeVisible();
  const kept = await (await page.request.get(`/api/v1/students/amy/bookings/${booking.id}`)).json();
  expect(kept.class.id).toBe(original.id);
  const balance = (await (await page.request.get('/api/v1/students/amy/credits')).json()).items.find((item: { id: string }) => item.id === pack.id);
  expect(balance).toMatchObject({ available: 0, reserved: 1 });
  await page.getByRole('button', { name: 'Sign out' }).click();

  headers = await login(page, 'coach');
  expect((await page.request.put(`/api/v1/lesson-changes/${change.id}/decision`, { headers, data: { decision: 'approved' } })).status()).toBe(409);
  await page.goto('/lesson-changes');
  await expect(page.getByRole('status').filter({ hasText: /Loading/ })).toHaveCount(0);
  await expect(page.getByRole('listitem').filter({ hasText: kind.name })).toHaveCount(0);
  await page.getByRole('button', { name: 'Sign out' }).click();

  await login(page, 'admin');
  await page.goto('/lesson-changes');
  await expect(page.getByRole('listitem').filter({ hasText: kind.name }).getByText('Withdrawn', { exact: true })).toBeVisible();
});
