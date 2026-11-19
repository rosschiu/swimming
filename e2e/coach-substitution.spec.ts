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

test('an administrator substitutes a coach; the roster moves and families and both coaches are told', async ({ page }) => {
  test.setTimeout(90000);
  const dsn = process.env.DATABASE_URL;
  expect(dsn, 'dedicated synthetic database required').toBeTruthy();
  // A second synthetic coach shares the seeded coach's synthetic password.
  execFileSync('psql', [dsn!, '-Xq', '-v', 'ON_ERROR_STOP=1'], { input:
    `INSERT INTO users(id,email,name,role,password_hash) SELECT 'cover-coach','cover-coach@swim.test','Cover Coach','coach',password_hash FROM users WHERE id='coach' ON CONFLICT (id) DO NOTHING;` });
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
  const date = new Date(Date.UTC(2450, 0, 1) + (suffix % 100000) * 86400000).toISOString().slice(0, 10);
  const venue = await post('venues', { name: 'Cover pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await post('class-types', { name: 'Cover lesson ' + suffix, level: 'Beginner' });
  const lesson = await post('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 3,
    startsAt: date + 'T10:00:00+08:00', endsAt: date + 'T11:00:00+08:00' });
  const offer = await post('package-offers', { name: 'Cover credits ' + suffix, credits: 1, priceMinor: 10000, currency: 'HKD', active: true,
    policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: true, absenceRetainsCredit: true, illnessRetainsCredit: true } });
  await switchRole('parent');
  const purchase = await post('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: 'cover-' + suffix });
  const attempt = await post(`purchases/${purchase.id}/payment-attempts`, { idempotencyKey: 'cover-pay-' + suffix });
  await post(`payment-attempts/${attempt.id}/mock-outcome`, { outcome: 'succeeded' }, 200);
  const pack = (await (await page.request.get('/api/v1/students/amy/credits')).json()).items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id);
  const booking = await post('students/amy/bookings', { classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion, creditPackageId: pack.id, idempotencyKey: 'cover-book-' + suffix });

  await switchRole('admin');
  await page.getByRole('link', { name: 'Schedule', exact: true }).click();
  await page.getByLabel('From date', { exact: true }).fill(date);
  await page.getByLabel('To date', { exact: true }).fill(date);
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await page.getByRole('button', { name: new RegExp('Substitute coach ' + kind.name) }).click();
  const form = page.getByRole('group', { name: 'Substitute coach' });
  await form.getByLabel('New coach').selectOption('cover-coach');
  await form.getByLabel('Reason shown in the audit history').fill('Coach unwell');
  const saved = page.waitForResponse(r => r.url().endsWith(`/classes/${lesson.id}/substitution`));
  await form.getByRole('button', { name: 'Confirm substitute' }).click();
  const substitution = await (await saved).json();
  expectContract('ClassSubstitution', substitution);
  expect(substitution).toMatchObject({ fromCoachId: 'coach', toCoachId: 'cover-coach', lesson: { coachName: 'Cover Coach' } });
  await expect(page.getByText('Cover Coach now teaches this lesson.')).toBeVisible();

  await switchRole('parent');
  const detail = await (await page.request.get(`/api/v1/students/amy/bookings/${booking.id}`)).json();
  expect(detail).toMatchObject({ status: 'reserved', class: { id: lesson.id, coachName: 'Cover Coach' } });
  await page.goto('/notifications');
  await expect(page.getByRole('article').filter({ hasText: 'A lesson has a new coach. The time and place are unchanged.' }).first()).toBeVisible();

  await switchRole('coach');
  expect((await page.request.get(`/api/v1/classes/${lesson.id}/roster`)).status()).toBe(404);
  await page.goto('/notifications');
  await expect(page.getByRole('article').filter({ hasText: 'A lesson has a new coach.' }).first()).toBeVisible();

  await page.getByRole('button', { name: 'Sign out' }).click();
  await signIn(page, 'cover-coach');
  const roster = await (await page.request.get(`/api/v1/classes/${lesson.id}/roster`)).json();
  expect(roster.items.map((item: { bookingId: string }) => item.bookingId)).toEqual([booking.id]);
});
