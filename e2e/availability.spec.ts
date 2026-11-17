import { test, expect, type Page } from '@playwright/test';
import { expectContract } from './contract';
async function login(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(`${role}@swim.test`);
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const auth = await (await page.request.get('/api/v1/auth/me')).json();
  return { Origin: 'http://localhost:18573', 'X-CSRF-Token': auth.csrfToken };
}

test('coach availability is persisted, reviewed by admin and cancellable without creating a class', async ({ page }) => {
  const adminHeaders = await login(page, 'admin');
  const suffix = Date.now();
  const venueResponse = await page.request.post('/api/v1/venues', { headers: adminHeaders, data: { name: `Availability pool ${suffix}`, address: 'Synthetic available venue', region: 'hk-island' } });
  expect(venueResponse.status()).toBe(201);
  const venue = await venueResponse.json();
  const date = new Date(Date.UTC(2040, 0, 1) + ((suffix % 100000) + 220000) * 86400000).toISOString().slice(0, 10);
  await page.getByRole('button', { name: 'Sign out' }).click();
  const coachHeaders = await login(page, 'coach');
  await page.getByRole('button', { name: 'Availability', exact: true }).click();
  await page.getByRole('button', { name: 'Add availability', exact: true }).click();
  await page.getByLabel('Availability venue', { exact: true }).selectOption(venue.id);
  await page.getByLabel('Start date', { exact: true }).fill(date);
  await page.getByLabel('End date', { exact: true }).fill(date);
  await page.getByLabel('Start time (Hong Kong)', { exact: true }).fill('13:00');
  await page.getByLabel('End time (Hong Kong)', { exact: true }).fill('15:00');
  await page.getByLabel('Capacity', { exact: true }).fill('4');
  await page.getByRole('button', { name: 'Save availability', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save availability', exact: true })).toHaveCount(0);
  async function filter() {
    await page.getByLabel('From date', { exact: true }).fill(date);
    await page.getByLabel('To date', { exact: true }).fill(date);
    await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
    await expect(page.getByRole('heading', { name: venue.name, exact: true })).toBeVisible();
  }
  await filter();
  const list = await (await page.request.get(`/api/v1/availability?from=${date}&to=${date}`)).json();
  const slot = list.items.find((x: {venueId: string}) => x.venueId === venue.id);
  expectContract('AvailabilitySlot', slot);
  expect(slot.editable).toBe(true);
  await page.getByRole('button', { name: `Edit availability ${date} 13:00`, exact: true }).click();
  await page.getByLabel('Capacity', { exact: true }).fill('5');
  await page.getByRole('button', { name: 'Save availability', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save availability', exact: true })).toHaveCount(0);
  expect((await page.request.post(`/api/v1/availability/${slot.id}/cancel`, { headers: coachHeaders, data: { version: slot.version } })).status()).toBe(409);
  const nearStart = new Date(Date.now() + 60 * 60 * 1000).toISOString();
  const nearEnd = new Date(Date.now() + 2 * 60 * 60 * 1000).toISOString();
  expect((await page.request.post('/api/v1/availability', { headers: coachHeaders, data: { venueId: venue.id, startsAt: nearStart, endsAt: nearEnd, capacity: 4 } })).status()).toBe(409);
  await page.getByRole('button', { name: 'Sign out' }).click();
  await login(page, 'admin');
  await page.getByRole('button', { name: 'Availability', exact: true }).click();
  await filter();
  const reviewed = await (await page.request.get(`/api/v1/availability/${slot.id}`)).json();
  expectContract('AvailabilitySlot', reviewed);
  expect(reviewed.capacity).toBe(5);
  expect(reviewed.editable).toBe(false);
  expect((await (await page.request.get(`/api/v1/classes?from=${date}&to=${date}&venueId=${venue.id}`)).json()).items).toHaveLength(0);
  await page.getByRole('button', { name: 'Sign out' }).click();
  await login(page, 'parent');
  expect((await page.request.get(`/api/v1/availability/${slot.id}`)).status()).toBe(403);
  await expect(page.getByRole('button', { name: 'Availability', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Sign out' }).click();
  await login(page, 'coach');
  await page.getByRole('button', { name: 'Availability', exact: true }).click();
  await filter();
  await page.getByRole('button', { name: `Cancel availability ${date} 13:00`, exact: true }).click();
  await page.getByRole('button', { name: 'Confirm cancellation', exact: true }).click();
  await expect.poll(async () => (await (await page.request.get(`/api/v1/availability/${slot.id}`)).json()).status).toBe('cancelled');
});

test('admin schedules two shorter lessons from a coach window and cancellation preserves them across roles', async ({ page }) => {
  let headers = await login(page, 'admin');
  const suffix = Date.now();
  const venueResponse = await page.request.post('/api/v1/venues', { headers, data: { name: `Window pool ${suffix}`, address: 'Synthetic pool', region: 'hk-island' } });
  expect(venueResponse.status()).toBe(201); const venue = await venueResponse.json();
  const kindResponse = await page.request.post('/api/v1/class-types', { headers, data: { name: `Window lesson ${suffix}`, level: 'Beginner' } });
  expect(kindResponse.status()).toBe(201); const kind = await kindResponse.json();
  const date = new Date(Date.UTC(2040, 0, 1) + ((suffix % 100000) + 350000) * 86400000).toISOString().slice(0, 10);
  await page.getByRole('button', { name: 'Sign out' }).click();
  headers = await login(page, 'coach');
  const created = await page.request.post('/api/v1/availability', { headers, data: { venueId: venue.id, startsAt: `${date}T13:00:00+08:00`, endsAt: `${date}T15:00:00+08:00`, capacity: 6 } });
  expect(created.status()).toBe(201); const slot = await created.json();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await login(page, 'admin');
  await page.getByRole('button', { name: 'Availability', exact: true }).click();
  await page.getByLabel('From date', { exact: true }).fill(date);
  await page.getByLabel('To date', { exact: true }).fill(date);
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  const card = page.locator('article').filter({ has: page.getByRole('heading', { name: venue.name, exact: true }) });
  await card.getByRole('button', { name: 'View / schedule lessons', exact: true }).click();
  await page.getByLabel('Lesson type', { exact: true }).selectOption(kind.id);
  await page.getByLabel('Lesson end (Hong Kong)', { exact: true }).fill(`${date}T14:00`);
  await page.getByRole('button', { name: 'Schedule lesson', exact: true }).click();
  await expect(page.getByText(/Lesson scheduled\. It is now/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Schedule lesson', exact: true })).toBeEnabled();
  await page.getByLabel('Lesson start (Hong Kong)', { exact: true }).fill(`${date}T14:00`);
  await page.getByLabel('Lesson end (Hong Kong)', { exact: true }).fill(`${date}T15:00`);
  await page.getByRole('button', { name: 'Schedule lesson', exact: true }).click();
  await expect(page.locator('li').filter({ hasText: kind.name })).toHaveCount(2);
  const linked = await (await page.request.get(`/api/v1/availability/${slot.id}/classes`)).json();
  expect(linked.items).toHaveLength(2); linked.items.forEach((item: unknown) => expectContract('ClassSession', item));
  await page.getByRole('button', { name: 'Sign out' }).click();
  headers = await login(page, 'coach');
  expect((await page.request.post(`/api/v1/availability/${slot.id}/cancel`, { headers, data: { version: slot.version } })).status()).toBe(200);
  for (const role of ['coach', 'parent', 'admin']) {
    if (role !== 'coach') { await page.getByRole('button', { name: 'Sign out' }).click(); await login(page, role); }
    await page.getByRole('button', { name: 'Schedule', exact: true }).click();
    await page.getByLabel('From date', { exact: true }).fill(date);
    await page.getByLabel('To date', { exact: true }).fill(date);
    await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
    await expect(page.getByRole('heading', { name: kind.name, exact: true })).toHaveCount(2);
  }
});
