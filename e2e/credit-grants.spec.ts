import { expect, test, type Page } from '@playwright/test';
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

test('an administrator grants complimentary credits to a swimmer with no purchases, who can then book with them', async ({ page }) => {
  test.setTimeout(90000);
  let headers = await signIn(page, 'admin');
  async function post(path: string, data: object, status = 201) {
    const response = await page.request.post('/api/v1/' + path, { headers, data });
    expect(response.status(), await response.text()).toBe(status);
    return response.json();
  }
  const suffix = Date.now();
  const date = new Date(Date.UTC(2550, 0, 1) + (suffix % 100000) * 86400000).toISOString().slice(0, 10);
  const swimmer = await post('students', { name: 'Granted swimmer ' + suffix, level: 'Beginner' });
  expect((await page.request.put(`/api/v1/students/${swimmer.id}/relationships/parent`, { headers, data: { kind: 'guardian' } })).status()).toBe(200);
  const venue = await post('venues', { name: 'Grant pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await post('class-types', { name: 'Grant lesson ' + suffix, level: 'Beginner' });
  const lesson = await post('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 3,
    startsAt: date + 'T10:00:00+08:00', endsAt: date + 'T11:00:00+08:00' });

  await page.goto(`/students/${swimmer.id}`);
  await page.getByRole('button', { name: 'Grant extra credits' }).click();
  const form = page.getByRole('group', { name: 'Grant extra credits' });
  await form.getByLabel('Credits').fill('2');
  await form.getByLabel('Usable until (Hong Kong)').fill(date);
  await form.getByLabel('Reason shown in the audit history').fill('Trial lessons');
  const granted = page.waitForResponse(r => r.url().endsWith(`/students/${swimmer.id}/credit-grants`));
  await form.getByRole('button', { name: 'Add complimentary credits' }).click();
  const grant = await (await granted).json();
  expectContract('CreditGrant', grant);
  await expect(page.getByText(`2 complimentary credits added, usable until ${date}.`)).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Complimentary credits' })).toBeVisible();

  await page.getByRole('button', { name: 'Sign out' }).click();
  headers = await signIn(page, 'parent');
  expect((await (await page.request.get(`/api/v1/students/${swimmer.id}/purchases`)).json()).items).toHaveLength(0);
  const history = await (await page.request.get(`/api/v1/students/${swimmer.id}/credit-history`)).json();
  expectContract('CreditHistory', history);
  expect(history.items).toMatchObject([{ complimentary: true, available: 2 }]);
  await post(`students/${swimmer.id}/bookings`, { classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion, creditPackageId: grant.packageId, idempotencyKey: 'grant-book-' + suffix });
  await page.goto(`/students/${swimmer.id}`);
  await expect(page.getByRole('heading', { name: 'Complimentary credits' })).toBeVisible();
  await expect(page.getByText('Available 1 · Reserved 1 · Consumed 0 · Expired 0')).toBeVisible();
});
