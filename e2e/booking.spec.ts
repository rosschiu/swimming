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

test('paid credits reserve a guardian lesson and an administrator override, visible to student and class coach', async ({ page }) => {
  test.setTimeout(60000);
  const headers = await signIn(page, 'admin');
  const suffix = Date.now();
  const date = new Date(Date.UTC(2400, 0, 1) + (suffix % 100000) * 86400000).toISOString().slice(0, 10);
  async function create(path: string, data: object) {
    const response = await page.request.post('/api/v1/' + path, { headers, data });
    expect(response.status(), await response.text()).toBe(201);
    return response.json();
  }
  const venue = await create('venues', { name: 'Booking pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  async function lesson(level: string, start: string, end: string) {
    const kind = await create('class-types', { name: level + ' booking ' + suffix, level });
    return create('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 3,
      startsAt: date + 'T' + start + ':00+08:00', endsAt: date + 'T' + end + ':00+08:00' });
  }
  const beginner = await lesson('Beginner', '10:00', '11:00');
  const advanced = await lesson('Advanced', '11:00', '12:00');
  const sibling = await create('students', { name: 'Booking sibling ' + suffix, level: 'Beginner' });
  expect((await page.request.put('/api/v1/students/' + sibling.id + '/relationships/parent', { headers, data: { kind: 'guardian' } })).status()).toBe(200);
  const offer = await create('package-offers', { name: 'Booking credits ' + suffix, credits: 2, priceMinor: 10000, currency: 'HKD', active: true,
    policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: false, absenceRetainsCredit: false, illnessRetainsCredit: true } });
  async function switchRole(role: string) {
    await page.getByRole('button', { name: 'Sign out' }).click();
    return signIn(page, role);
  }
  await switchRole('parent');
  await page.getByRole('link', { name: 'Packages and payments' }).click();
  await page.getByLabel('Choose child').selectOption('amy');
  await page.getByLabel('Package offer', { exact: true }).selectOption(offer.id);
  const created = page.waitForResponse(r => r.url().endsWith('/students/amy/purchases') && r.request().method() === 'POST');
  await page.getByRole('button', { name: 'Create purchase' }).click();
  const purchase = await (await created).json();
  const checkout = page.locator('article').filter({ has: page.getByRole('heading', { name: offer.name, exact: true }) });
  await checkout.getByRole('button', { name: 'Start mock payment' }).click();
  await checkout.getByRole('button', { name: 'Mark succeeded' }).click();
  await expect(checkout.getByText('Paid (simulated)', { exact: true })).toBeVisible();
  async function credit() {
    const history = await (await page.request.get('/api/v1/students/amy/credits')).json();
    return history.items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id);
  }
  const pack = await credit();
  expect(pack.available).toBe(2);
  async function openForm() {
    await page.goto('/students/amy');
    await page.getByRole('button', { name: 'Book a lesson' }).click();
    await page.getByLabel('Find classes from (Hong Kong)').fill(date);
    await page.getByRole('button', { name: 'Load classes and credits' }).click();
  }
  await openForm();
  const classChoice = page.getByLabel('Class to book');
  await classChoice.selectOption(beginner.id);
  await expect(classChoice.getByRole('option', { name: new RegExp(advanced.classTypeName) })).toHaveCount(0);
  await page.getByLabel('Credit package', { exact: true }).selectOption(pack.id);
  await page.getByRole('button', { name: 'Review booking' }).click();
  await expect(page.getByRole('heading', { name: 'Review booking' })).toBeVisible();
  expect((await credit()).reserved).toBe(0);
  async function confirm(className: string) {
    const created = page.waitForResponse(r => r.url().endsWith('/students/amy/bookings') && r.request().method() === 'POST');
    await page.getByRole('button', { name: 'Confirm booking' }).click();
    const response = await created;
    expect(response.status()).toBe(201);
    await expect(page.getByText('Lesson booked. One place and one credit are reserved.')).toBeVisible();
    await expect(page.getByRole('region', { name: 'Booked lessons' }).getByText(className, { exact: true })).toBeVisible();
    return (await response.json()).id as string;
  }
  async function readConfirmation(bookingId: string) {
    await page.getByRole('link', { name: /^Notifications/ }).click();
    const response = await page.request.get('/api/v1/notifications?limit=25');
    expect(response.status()).toBe(200);
    const data = await response.json();
    expectContract('NotificationList', data);
    const items: components['schemas']['Notification'][] = data.items;
    const matches = items.filter(item => item.kind === 'booking.created' && item.sourceId === bookingId);
    expect(matches).toHaveLength(1);
    const [notice] = matches;
    expect(notice).toMatchObject({ studentId: 'amy', studentName: 'Amy', readAt: null });
    expect(Object.keys(notice).sort()).toEqual(['id', 'kind', 'sourceId', 'sourceVersion', 'studentId', 'studentName', 'createdAt', 'readAt'].sort());
    const card = page.getByRole('article').nth(items.findIndex(item => item.id === notice.id));
    await expect(card.getByText('A lesson was booked.', { exact: true })).toBeVisible();
    await expect(card).not.toContainText(pack.id);
    await expect(card).not.toContainText('Assessment approved for synthetic swimmer');
    const marked = page.waitForResponse(r => r.url().endsWith('/notifications/' + notice.id + '/read') && r.request().method() === 'PUT');
    await card.getByRole('button', { name: 'Mark read' }).click();
    expect((await marked).status()).toBe(200);
    await page.reload();
    await expect(card.getByText(/· Read$/)).toBeVisible();
    const link = card.getByRole('link', { name: 'Open current student profile' });
    await expect(link).toHaveAttribute('href', '/students/amy');
    await link.click();
    await expect(page).toHaveURL(/\/students\/amy$/);
    await expect(page.getByRole('heading', { name: 'Booked lessons' })).toBeVisible();
  }
  const firstBooking = await confirm(beginner.classTypeName);
  await readConfirmation(firstBooking);
  expect(await credit()).toMatchObject({ available: 1, reserved: 1 });
  await page.getByRole('link', { name: /Back to directory/ }).click();
  await page.getByRole('link', { name: new RegExp(sibling.name) }).click();
  await openList(date);
  await expect(page.getByText('No booked lessons on these dates.')).toBeVisible();

  await switchRole('admin');
  await openForm();
  await classChoice.selectOption(advanced.id);
  await page.getByLabel('Credit package', { exact: true }).selectOption(pack.id);
  await expect(page.getByRole('button', { name: 'Review booking' })).toBeDisabled();
  await page.getByLabel('Level override reason').fill('Assessment approved for synthetic swimmer');
  await page.getByRole('button', { name: 'Review booking' }).click();
  const secondBooking = await confirm(advanced.classTypeName);
  expect(await credit()).toMatchObject({ available: 0, reserved: 2 });

  async function openList(day: string) {
    await page.getByLabel('Bookings from date').fill(day);
    await page.getByLabel('Bookings to date').fill(day);
    await page.getByRole('button', { name: 'Load booked lessons' }).click();
  }
  await switchRole('student');
  await readConfirmation(secondBooking);
  await openList(date);
  const list = page.getByRole('region', { name: 'Booked lessons' });
  await expect(list.getByText(beginner.classTypeName, { exact: true })).toBeVisible();
  await expect(list.getByText(advanced.classTypeName, { exact: true })).toBeVisible();
  await expect(list.getByRole('button', { name: 'Book a lesson' })).toHaveCount(0);
  const lessons = await (await page.request.get('/api/v1/students/amy/bookings?from=' + date + '&to=' + date)).json();
  lessons.items.forEach((item: unknown) => expectContract('StudentBooking', item));

  await switchRole('coach');
  await page.getByRole('link', { name: 'Schedule', exact: true }).click();
  await page.getByLabel('From date', { exact: true }).fill(date);
  await page.getByLabel('To date', { exact: true }).fill(date);
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await page.getByRole('button', { name: 'View roster ' + advanced.classTypeName + ' ' + date + ' 11:00' }).click();
  const roster = page.getByRole('region', { name: 'Class roster' });
  await expect(roster.getByText('Amy', { exact: true })).toBeVisible();
  await expect(roster.getByText('Reserved', { exact: true })).toBeVisible();
  const savedRoster = await (await page.request.get('/api/v1/classes/' + advanced.id + '/roster')).json();
  expect(savedRoster.classId).toBe(advanced.id);
  expect(savedRoster.items).toHaveLength(1);
  expectContract('RosterEntry', savedRoster.items[0]);
  // The mail worker can send only queued deliveries. Check every recipient of these two bookings.
  const dsn = process.env.DATABASE_URL;
  expect(dsn, 'dedicated synthetic database required').toBeTruthy();
  const queued = execFileSync('psql', [dsn!, '-XtA', '-v', 'ON_ERROR_STOP=1', '-v', 'first=' + firstBooking, '-v', 'second=' + secondBooking], {
    input: "SELECT count(*) FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.kind='booking.created' AND n.source_id IN (:'first', :'second');",
    encoding: 'utf8',
  });
  expect(queued.trim()).toBe('0');
});
