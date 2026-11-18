import { expect, test, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import type { components } from '../apps/web/src/generated/api';
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

test('coach records attendance once and administrator, guardian and student see the saved outcome', async ({ page }) => {
  test.setTimeout(60000);
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
  const date = new Date(Date.UTC(2800, 0, 1) + (suffix % 100000) * 86400000).toISOString().slice(0, 10);
  const venue = await post('venues', { name: 'Attendance pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await post('class-types', { name: 'Attendance lesson ' + suffix, level: 'Beginner' });
  const lesson = await post('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 1,
    startsAt: date + 'T10:00:00+08:00', endsAt: date + 'T11:00:00+08:00' });
  const offer = await post('package-offers', { name: 'Attendance package ' + suffix, credits: 2, priceMinor: 10000,
    currency: 'HKD', active: true, policyOverride: { expiryEnabled: false, carryForwardEnabled: false,
      earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: false, absenceRetainsCredit: false, illnessRetainsCredit: true } });
  await switchRole('parent');
  const purchase = await post('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: 'order-' + suffix });
  const attempt = await post('purchases/' + purchase.id + '/payment-attempts', { idempotencyKey: 'attempt-' + suffix });
  await post('payment-attempts/' + attempt.id + '/mock-outcome', { outcome: 'succeeded' }, 200);
  async function credit() {
    const response = await page.request.get('/api/v1/students/amy/credits');
    expect(response.status()).toBe(200);
    const { items } = await response.json() as { items: components['schemas']['CreditPackage'][] };
    const pack = items.find(item => item.purchaseId === purchase.id);
    expect(pack).toBeDefined();
    return pack!;
  }
  const pack = await credit();
  const booking = await post('students/amy/bookings', { classId: lesson.id, classVersion: lesson.version,
    classTypeVersion: lesson.classTypeVersion, creditPackageId: pack.id, idempotencyKey: 'booking-' + suffix });
  expect(await credit()).toMatchObject({ available: 1, reserved: 1, consumed: 0 });

  // Move only this synthetic lesson past the opening window after a real reservation.
  const dsn = process.env.DATABASE_URL;
  expect(dsn, 'dedicated synthetic database required').toBeTruthy();
  const pastDate = execFileSync('psql', [dsn!, '-XqtA', '-v', 'ON_ERROR_STOP=1', '-v', 'lesson=' + lesson.id, '-v', 'venue=' + venue.id], {
    encoding: 'utf8', input: `UPDATE class_sessions SET starts_at=now()-interval '2 days', ends_at=now()-interval '2 days'+interval '1 hour'
      WHERE id=:'lesson' AND venue_id=:'venue' RETURNING (starts_at AT TIME ZONE 'Asia/Hong_Kong')::date;`,
  }).trim();
  expect(pastDate).toMatch(/^\d{4}-\d{2}-\d{2}$/);

  async function openRoster() {
    await page.getByRole('link', { name: 'Schedule', exact: true }).click();
    await page.getByLabel('From date', { exact: true }).fill(pastDate);
    await page.getByLabel('To date', { exact: true }).fill(pastDate);
    await page.getByRole('button', { name: 'Apply filters' }).click();
    await page.getByRole('button', { name: new RegExp('View roster ' + kind.name) }).click();
    return page.getByRole('region', { name: 'Class roster' });
  }
  await switchRole('coach');
  const roster = await openRoster();
  await expect(roster.getByText('Amy', { exact: true })).toBeVisible();
  const path = '/api/v1/classes/' + lesson.id + '/roster/' + booking.id + '/attendance';
  await roster.getByRole('button', { name: 'Mark present Amy' }).click();
  const saved = page.waitForResponse(response => response.url().endsWith(path) && response.request().method() === 'PUT');
  await roster.getByRole('button', { name: 'Confirm present' }).click();
  const response = await saved;
  expect(response.status()).toBe(200);
  const attendance = await response.json();
  expect(attendance).toMatchObject({ status: 'present', markedAt: expect.any(String) });
  await expect(roster.getByText('Present', { exact: true })).toBeVisible();

  await switchRole('admin');
  const adminRoster = await openRoster();
  await expect(adminRoster.getByText('Present', { exact: true })).toBeVisible();
  const replay = await page.request.put(path, { headers });
  expect(replay.status()).toBe(200);
  expect(await replay.json()).toEqual(attendance);
  const after = await credit();
  expect(after).toMatchObject({ available: 1, reserved: 0, consumed: 1 });
  expect(after.movements.filter(item => item.consumed === 1)).toHaveLength(1);

  for (const role of ['parent', 'student']) {
    await switchRole(role);
    await page.goto('/students/amy');
    await page.getByLabel('Bookings from date').fill(pastDate);
    await page.getByLabel('Bookings to date').fill(pastDate);
    await page.getByRole('button', { name: 'Load booked lessons' }).click();
    const row = page.getByRole('region', { name: 'Booked lessons' }).getByRole('listitem').filter({ hasText: kind.name });
    await expect(row.getByText('Present', { exact: true })).toBeVisible();
    await expect(row.getByRole('button')).toHaveCount(0);
    const list = await page.request.get('/api/v1/students/amy/bookings?from=' + pastDate + '&to=' + pastDate);
    expect(list.status()).toBe(200);
    const item = (await list.json()).items.find((entry: { id: string }) => entry.id === booking.id);
    expectContract('StudentBooking', item);
    expect(item.attendance).toEqual(attendance);
  }
});
