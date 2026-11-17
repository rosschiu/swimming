import { test, expect, type Page } from '@playwright/test';
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

test('atomic series and travel conflicts persist safely through the real API', async ({ page }) => {
  const headers = await login(page, 'admin');
  const suffix = Date.now();
  const venueResponse = await page.request.post('/api/v1/venues', { headers, data: { name: `Travel pool ${suffix}`, address: 'Synthetic test venue', region: 'hk-island' } });
  expect(venueResponse.status()).toBe(201);
  const venue = await venueResponse.json();
  expectContract('Venue', venue);
  const otherVenue = await (await page.request.post('/api/v1/venues', { headers, data: { name: `Second pool ${suffix}`, address: 'Synthetic second venue', region: 'kowloon-east' } })).json();
  const typeResponse = await page.request.post('/api/v1/class-types', { headers, data: { name: `Travel lesson ${suffix}`, level: 'Beginner' } });
  expect(typeResponse.status()).toBe(201);
  const classType = await typeResponse.json();
  expectContract('ClassType', classType);
  // A fresh distant date isolates persisted sessions from previous local runs.
  const date = new Date(Date.UTC(2040, 0, 1) + (suffix % 100000) * 86400000).toISOString().slice(0, 10);
  const base = { venueId: venue.id, classTypeId: classType.id, coachId: 'coach', startsAt: `${date}T10:00:00+08:00`, endsAt: `${date}T11:00:00+08:00`, capacity: 6 };
  const created = await page.request.post('/api/v1/classes', { headers, data: base });
  expect(created.status()).toBe(201);
  const saved = await created.json();
  expectContract('ClassSession', saved);
  const conflicting = await page.request.post('/api/v1/classes', { headers, data: { ...base, venueId: otherVenue.id, startsAt: `${date}T11:29:00+08:00`, endsAt: `${date}T12:00:00+08:00` } });
  expect(conflicting.status()).toBe(409);
  const exactBoundary = await page.request.post('/api/v1/classes', { headers, data: { ...base, venueId: otherVenue.id, startsAt: `${date}T11:30:00+08:00`, endsAt: `${date}T12:00:00+08:00` } });
  expect(exactBoundary.status()).toBe(201);
  const settings = await (await page.request.get('/api/v1/settings/scheduling')).json();
  expectContract('SchedulingSettings', settings);
  expect(settings.travelGapMinutes).toBe(30);
  expect((await page.request.patch('/api/v1/settings/scheduling', { headers, data: { ...settings, travelGapMinutes: 31 } })).status()).toBe(409);
  expect(await (await page.request.get('/api/v1/settings/scheduling')).json()).toEqual(settings);
  const weekday = new Date(`${date}T00:00:00Z`).getUTCDay() || 7;
  const endDate = new Date(Date.parse(`${date}T00:00:00Z`) + 7 * 86400000).toISOString().slice(0, 10);
  const series = await page.request.post('/api/v1/class-series', { headers, data: { venueId: venue.id, classTypeId: classType.id, coachId: 'coach', startDate: date, endDate, weekday, startTime: '10:30', endTime: '11:00', capacity: 6 } });
  expect(series.status()).toBe(409);
  const classes = await (await page.request.get(`/api/v1/classes?from=${date}&to=${endDate}`)).json();
  expect(classes.items.filter((c: {venueId: string}) => [venue.id, otherVenue.id].includes(c.venueId))).toHaveLength(2);
  expect((await page.request.patch(`/api/v1/classes/${saved.id}`, { headers, data: { ...base, capacity: 8, version: saved.version } })).status()).toBe(200);
  expect((await page.request.patch(`/api/v1/classes/${saved.id}`, { headers, data: { ...base, capacity: 7, version: saved.version } })).status()).toBe(409);
  expect((await (await page.request.get(`/api/v1/classes/${saved.id}`)).json()).capacity).toBe(8);
  await page.getByRole('button', { name: 'Sign out' }).click();
  const parentHeaders = await login(page, 'parent');
  expect((await page.request.post('/api/v1/classes', { headers: parentHeaders, data: base })).status()).toBe(403);
  expect((await page.request.get('/api/v1/settings/scheduling')).status()).toBe(403);
});

test('administrator creates a weekly schedule shared with coach and family catalogue', async ({ page }) => {
  await login(page, 'admin');
  const suffix = Date.now();
  const pool = `UI pool ${suffix}`;
  const lesson = `UI swim ${suffix}`;
  await page.getByRole('link', { name: 'School setup', exact: true }).click();
  await page.getByRole('button', { name: 'Add venue', exact: true }).click();
  await page.getByLabel('Venue name', { exact: true }).fill(pool);
  await page.getByLabel('Address', { exact: true }).fill('Synthetic swimming pool');
  await page.getByRole('button', { name: 'Save venue', exact: true }).click();
  await expect(page.getByRole('heading', { name: pool, exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Add class type', exact: true }).click();
  await page.getByLabel('Class type name', { exact: true }).fill(lesson);
  await page.getByLabel('Class level', { exact: true }).fill('Beginner');
  await page.getByRole('button', { name: 'Save class type', exact: true }).click();
  await expect(page.getByRole('heading', { name: lesson, exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Schedule', exact: true }).click();
  await page.getByRole('button', { name: 'Add weekly series', exact: true }).click();
  const date = new Date(Date.UTC(2040, 0, 1) + ((suffix % 100000) + 110000) * 86400000).toISOString().slice(0, 10);
  const endDate = new Date(Date.parse(`${date}T00:00:00Z`) + 7 * 86400000).toISOString().slice(0, 10);
  await page.getByLabel('Class venue', { exact: true }).selectOption({ label: pool });
  await page.getByLabel('Class type', { exact: true }).selectOption({ label: `${lesson} · Beginner` });
  await page.getByLabel('Assigned coach', { exact: true }).selectOption('coach');
  await page.getByLabel('Series start date', { exact: true }).fill(date);
  await page.getByLabel('Series end date', { exact: true }).fill(endDate);
  await page.getByLabel('Repeat every', { exact: true }).selectOption(String(new Date(`${date}T00:00:00Z`).getUTCDay() || 7));
  await page.getByLabel('Start time (Hong Kong)', { exact: true }).fill('14:00');
  await page.getByLabel('End time (Hong Kong)', { exact: true }).fill('15:00');
  await page.getByRole('button', { name: 'Create weekly series', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'New weekly series', exact: true })).toHaveCount(0);
  async function filterSchedule() {
    await page.getByLabel('From date', { exact: true }).fill(date);
    await page.getByLabel('To date', { exact: true }).fill(endDate);
    await page.getByLabel('Filter venue', { exact: true }).selectOption({ label: pool });
    await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
    await expect(page.getByRole('heading', { name: lesson, exact: true })).toHaveCount(2);
  }
  await filterSchedule();
  const list = await (await page.request.get(`/api/v1/classes?from=${date}&to=${endDate}`)).json();
  const sessions = list.items.filter((c: {classTypeName: string}) => c.classTypeName === lesson);
  expect(sessions).toHaveLength(2);
  for (const session of sessions) expectContract('ClassSession', session);
  const series = await (await page.request.get(`/api/v1/class-series/${sessions[0].seriesId}`)).json();
  expectContract('ClassSeries', series);
  await page.getByRole('button', { name: `Edit class ${lesson} ${date} 14:00`, exact: true }).click();
  await page.getByLabel('Capacity', { exact: true }).fill('8');
  await page.getByRole('button', { name: 'Save class', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Edit class', exact: true })).toHaveCount(0);
  expect((await (await page.request.get(`/api/v1/classes/${sessions[1].id}`)).json()).capacity).toBe(6);
  for (const role of ['coach', 'parent', 'student']) {
    await page.getByRole('button', { name: 'Sign out' }).click();
    await login(page, role);
    await page.getByRole('link', { name: 'Schedule', exact: true }).click();
    await filterSchedule();
    await expect(page.getByRole('button', { name: 'Add class', exact: true })).toHaveCount(0);
    await expect(page.getByText('Capacity 8', { exact: false })).toBeVisible();
  }
});
