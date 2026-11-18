import { expect, test, type Page } from '@playwright/test';
import type { components } from '../apps/web/src/generated/api';
import { expectContract } from './contract';

async function login(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(role + '@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const auth = await (await page.request.get('/api/v1/auth/me')).json();
  return { Origin: 'http://localhost:18573', 'X-CSRF-Token': auth.csrfToken };
}

test('guardian reserves two selected weeks with purchased credits and leaves the intervening week open', async ({ page }) => {
  const headers = await login(page, 'admin');
  const stamp = Date.now();
  const start = Date.UTC(2700, 0, 1) + (stamp % 100000) * 86400000;
  const day = (week: number) => new Date(start + week * 7 * 86400000).toISOString().slice(0, 10);
  async function create(path: string, data: object) {
    const response = await page.request.post('/api/v1/' + path, { headers, data });
    expect(response.status(), await response.text()).toBe(201);
    return response.json();
  }
  const venue = await create('venues', { name: 'Series pool ' + stamp, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await create('class-types', { name: 'Weekly group ' + stamp, level: 'Beginner' });
  const series = await create('class-series', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', startDate: day(0), endDate: day(2),
    weekday: new Date(start).getUTCDay() || 7, startTime: '15:00', endTime: '16:00', capacity: 2 });
  expect(series.sessions).toHaveLength(3);
  const offer = await create('package-offers', { name: 'Weekly credits ' + stamp, credits: 3, priceMinor: 10000, currency: 'HKD', active: true,
    policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: false, absenceRetainsCredit: false, illnessRetainsCredit: true } });
  async function switchRole(role: string) {
    await page.getByRole('button', { name: 'Sign out' }).click();
    return login(page, role);
  }
  const guardianHeaders = await switchRole('parent');
  const purchase = await page.request.post('/api/v1/students/amy/purchases', { headers: guardianHeaders, data: {
    offerId: offer.id, offerVersion: offer.version, idempotencyKey: offer.id,
  } });
  expect(purchase.status()).toBe(201);
  const order = await purchase.json();
  await page.getByRole('link', { name: 'Packages and payments' }).click();
  await page.getByLabel('Choose child').selectOption('amy');
  const checkout = page.getByRole('article').filter({ hasText: order.id });
  await checkout.getByRole('button', { name: 'Start mock payment' }).click();
  await checkout.getByRole('button', { name: 'Mark succeeded' }).click();
  await expect(checkout.getByText('Paid (simulated)', { exact: true })).toBeVisible();
  async function credit() {
    const response = await page.request.get('/api/v1/students/amy/credits');
    expect(response.status()).toBe(200);
    const items: components['schemas']['CreditPackage'][] = (await response.json()).items;
    const pack = items.find(item => item.purchaseId === order.id);
    expect(pack).toBeDefined();
    return pack!;
  }
  const pack = await credit();
  await page.goto('/students/amy');
  await page.getByRole('button', { name: 'Enrol in weekly lessons' }).click();
  await page.getByLabel('Find weekly lessons from (Hong Kong)').fill(day(0));
  await page.getByRole('button', { name: 'Load weekly lessons and credits' }).click();
  await page.getByLabel('Weekly series', { exact: true }).selectOption(series.id);
  const weeks = page.getByRole('group', { name: 'Select lesson weeks' });
  await expect(weeks.getByRole('checkbox', { checked: true })).toHaveCount(0);
  await weeks.getByRole('checkbox', { name: new RegExp(day(0)) }).check();
  await weeks.getByRole('checkbox', { name: new RegExp(day(2)) }).check();
  await page.getByLabel('Enrolment credit package').selectOption(pack.id);
  await page.getByRole('button', { name: 'Review enrolment' }).click();
  const review = page.getByRole('region', { name: 'Weekly enrolment' });
  await expect(review.getByRole('listitem')).toHaveCount(2);
  await expect(review.getByRole('list')).toContainText(day(0));
  await expect(review.getByRole('list')).toContainText(day(2));
  expect(await credit()).toMatchObject({ available: 3, reserved: 0 });
  const path = '/api/v1/students/amy/enrolments';
  const created = page.waitForResponse(r => r.url().endsWith(path) && r.request().method() === 'POST');
  await review.getByRole('button', { name: 'Confirm weekly enrolment' }).click();
  const saved = await created;
  expect(saved.status()).toBe(201);
  const result = await saved.json();
  expectContract('Enrolment', result);
  expect(result.bookings.map((b: components['schemas']['StudentBooking']) => b.class.id)).toEqual([series.sessions[0].id, series.sessions[2].id]);
  await expect(review.getByText('Weekly enrolment confirmed. 2 lessons and credits reserved.')).toBeVisible();
  const payload = saved.request().postDataJSON();
  const replay = await page.request.post(path, { headers: guardianHeaders, data: payload });
  expect(replay.status()).toBe(200);
  expect(await replay.json()).toEqual(result);
  const after = await credit();
  expect(after).toMatchObject({ available: 1, reserved: 2, consumed: 0 });
  expect(after.movements.filter(m => m.available === -1 && m.reserved === 1)).toHaveLength(2);

  const studentHeaders = await switchRole('student');
  expect((await page.request.post(path, { headers: studentHeaders, data: payload })).status()).toBe(403);
  await page.goto('/students/amy');
  await page.getByLabel('Bookings from date').fill(day(0));
  await page.getByLabel('Bookings to date').fill(day(2));
  await page.getByRole('button', { name: 'Load booked lessons' }).click();
  const lessons = page.getByRole('region', { name: 'Booked lessons' }).getByRole('listitem').filter({ hasText: kind.name });
  await expect(lessons).toHaveCount(2);
  await expect(lessons.first()).toContainText(day(0));
  await expect(lessons.last()).toContainText(day(2));
  await expect(page.getByRole('button', { name: 'Enrol in weekly lessons' })).toHaveCount(0);

  await switchRole('coach');
  await page.getByRole('link', { name: 'Schedule', exact: true }).click();
  await page.getByLabel('From date', { exact: true }).fill(day(0));
  await page.getByLabel('To date', { exact: true }).fill(day(2));
  await page.getByRole('button', { name: 'Apply filters' }).click();
  const roster = page.getByRole('region', { name: 'Class roster' });
  for (const week of [0, 1, 2]) {
    await page.getByRole('button', { name: 'View roster ' + kind.name + ' ' + day(week) + ' 15:00' }).click();
    if (week === 1) await expect(roster.getByText('No reserved places for this class.')).toBeVisible();
    else await expect(roster.getByText('Amy', { exact: true })).toBeVisible();
  }
});
