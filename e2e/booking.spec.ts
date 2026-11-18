import { expect, test, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import type { components } from '../apps/web/src/generated/api';
import { expectContract } from './contract';

const choice = (target: Page, id: string) => target.locator(`input[type=radio][value="${id}"]`);

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
  const replacement = await create('classes', { venueId: venue.id, classTypeId: beginner.classTypeId, coachId: 'coach', capacity: 3,
    startsAt: date + 'T13:00:00+08:00', endsAt: date + 'T14:00:00+08:00' });
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
    await page.getByLabel('Show classes from').fill(date);
  }
  await openForm();
  await choice(page, beginner.id).check();
  await expect(choice(page, advanced.id)).toHaveCount(0);
  await choice(page, pack.id).check();
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
    const link = card.getByRole('link', { name: 'View booking' });
    const path = '/students/amy/bookings/' + bookingId;
    await expect(link).toHaveAttribute('href', path);
    await link.focus();
    await page.keyboard.press('Enter');
    await expect(page).toHaveURL(url => url.pathname === path);
    await expect(page.getByRole('main')).toBeFocused();
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Booking details' })).toBeVisible();
    return path;
  }
  const firstBooking = await confirm(beginner.classTypeName);
  const permanentPath = await readConfirmation(firstBooking);
  expect(await credit()).toMatchObject({ available: 1, reserved: 1 });
  await page.goto('/students');
  await page.getByRole('link', { name: new RegExp(sibling.name) }).click();
  await openList(date);
  await expect(page.getByText('No booked lessons on these dates.')).toBeVisible();

  await switchRole('admin');
  await openForm();
  await choice(page, advanced.id).check();
  await choice(page, pack.id).check();
  await expect(page.getByRole('button', { name: 'Review booking' })).toBeDisabled();
  await page.getByLabel('Level override reason').fill('Assessment approved for synthetic swimmer');
  await page.getByRole('button', { name: 'Review booking' }).click();
  const secondBooking = await confirm(advanced.classTypeName);
  expect(await credit()).toMatchObject({ available: 0, reserved: 2 });

  async function openList(day: string) {
    if (await page.getByRole('button', { name: 'Change dates' }).isVisible()) await page.getByRole('button', { name: 'Change dates' }).click();
    await page.getByLabel('Bookings from date').fill(day);
    await page.getByLabel('Bookings to date').fill(day);
    await page.getByRole('button', { name: 'Load booked lessons' }).click();
  }
  await switchRole('student');
  await readConfirmation(secondBooking);
  await page.goto('/students/amy');
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
  const family = await switchRole('parent');
  const requested = await page.request.post('/api/v1/students/amy/bookings/' + firstBooking + '/change-requests', {
    headers: family, data: { targetClassId: replacement.id, targetClassVersion: replacement.version, idempotencyKey: firstBooking },
  });
  expect(requested.status(), await requested.text()).toBe(201);
  const change = await requested.json();
  const coach = await switchRole('coach');
  const approved = await page.request.put('/api/v1/lesson-changes/' + change.id + '/decision', { headers: coach, data: { decision: 'approved' } });
  expect(approved.status(), await approved.text()).toBe(200);
  for (const role of ['parent', 'student']) {
    await switchRole(role);
    await page.goto(permanentPath);
    await expect(page.getByRole('heading', { name: 'Booking details' })).toBeVisible();
    await expect(page.getByText(/13:00/).first()).toBeVisible();
    const exact = await page.request.get('/api/v1' + permanentPath);
    expect(exact.status()).toBe(200);
    const current = await exact.json();
    expectContract('StudentBookingDetail', current);
    expect(current).toMatchObject({ id: firstBooking, class: { id: replacement.id } });
    expect((await page.request.get('/api/v1/students/' + sibling.id + '/bookings/' + firstBooking)).status()).toBe(404);
  }
  // Age the approved reservation in this isolated synthetic database.
  const archived = execFileSync('psql', [process.env.DATABASE_URL!, '-XqtA', '-v', 'ON_ERROR_STOP=1', '-v', 'fixture=' + replacement.id], {
    input: "UPDATE class_sessions SET starts_at='2001-01-10T13:00:00+08:00', ends_at='2001-01-10T14:00:00+08:00' WHERE id=:'fixture' RETURNING id;",
    encoding: 'utf8',
  });
  expect(archived.trim()).toBe(replacement.id);
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Hong_Kong' }).format(new Date());
  const recent = await page.request.get('/api/v1/students/amy/bookings?from=' + today + '&to=' + today);
  expect(recent.status()).toBe(200);
  expect((await recent.json()).items).not.toContainEqual(expect.objectContaining({ id: firstBooking }));
  await page.reload();
  await expect(page.getByText(/2001-01-10.*13:00/)).toBeVisible();
  const newAdmin = await switchRole('admin');
  const link = '/api/v1/students/amy/relationships/parent';
  expect((await page.request.delete(link, { headers: newAdmin })).status()).toBe(204);
  try {
    await switchRole('parent');
    await page.goto(permanentPath);
    expect((await page.request.get('/api/v1' + permanentPath)).status()).toBe(404);
    await expect(page.getByRole('alert')).toHaveText('This booking is unavailable for your current access.');
    await expect(page.getByText(beginner.classTypeName, { exact: false })).toHaveCount(0);
    await switchRole('student');
    await page.goto(permanentPath);
    await expect(page.getByText(/13:00/).first()).toBeVisible();
  } finally {
    const restore = await switchRole('admin');
    expect((await page.request.put(link, { headers: restore, data: { kind: 'guardian' } })).status()).toBe(200);
  }
  // The mail worker can send only queued deliveries. Check every recipient of these two bookings.
  const dsn = process.env.DATABASE_URL;
  expect(dsn, 'dedicated synthetic database required').toBeTruthy();
  const queued = execFileSync('psql', [dsn!, '-XtA', '-v', 'ON_ERROR_STOP=1', '-v', 'first=' + firstBooking, '-v', 'second=' + secondBooking], {
    input: "SELECT count(*) FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.kind='booking.created' AND n.source_id IN (:'first', :'second');",
    encoding: 'utf8',
  });
  expect(queued.trim()).toBe('0');
});

test('school validity controls an overnight reservation and preserves it after the rule changes', async ({ page: admin, browser }) => {
  test.setTimeout(60000);
  const headers = await signIn(admin, 'admin');
  const parent = await browser.newPage({ baseURL: 'http://localhost:18573' });
  const coach = await browser.newPage({ baseURL: 'http://localhost:18573' });
  const settings = '/api/v1/settings/booking-validity';
  const original = await (await admin.request.get(settings)).json();
  const rule = admin.getByLabel('Credit validity rule');
  async function setRule(value: string) {
    if (await rule.inputValue() === value) return;
    await rule.selectOption(value);
    const saved = admin.waitForResponse(r => r.url().endsWith(settings) && r.request().method() === 'PATCH');
    await admin.getByRole('button', { name: 'Save booking validity' }).click();
    expect((await saved).status()).toBe(200);
    await expect(admin.getByText('Booking validity saved.', { exact: true })).toBeVisible();
  }
  try {
    const guardianHeaders = await signIn(parent, 'parent');
    const coachHeaders = await signIn(coach, 'coach');
    await coach.goto('/setup');
    await expect(coach.getByLabel('Credit validity rule')).toHaveCount(0);
    expect((await coach.request.patch(settings, { headers: coachHeaders, data: {
      validityRule: 'lesson_start', version: original.version,
    } })).status()).toBe(403);
    await admin.goto('/setup');
    await setRule('lesson_end');

    const suffix = Date.now();
    const midnight = Date.UTC(2200, 0, 1) + (suffix % 100000) * 86400000;
    const expiryDay = new Date(midnight).toISOString().slice(0, 10);
    const nextDay = new Date(midnight + 86400000).toISOString().slice(0, 10);
    async function create(path: string, data: object, actor = admin) {
      const response = await actor.request.post('/api/v1/' + path, { data, headers: actor === admin ? headers : guardianHeaders });
      expect(response.status(), await response.text()).toBe(201);
      return response.json();
    }
    const venue = await create('venues', { name: 'Overnight pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
    const kind = await create('class-types', { name: 'Overnight class ' + suffix, level: 'Beginner' });
    const lesson = await create('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 2,
      startsAt: expiryDay + 'T23:30:00+08:00', endsAt: nextDay + 'T00:30:00+08:00' });
    const offer = await create('package-offers', { name: 'Overnight credit ' + suffix, credits: 1, priceMinor: 10000, currency: 'HKD', active: true,
      policyOverride: { expiryEnabled: true, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: false, absenceRetainsCredit: false, illnessRetainsCredit: true } });
    const purchase = await create('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: offer.id }, parent);
    expect((await admin.request.post('/api/v1/purchases/' + purchase.id + '/expiry', { headers, data: {
      version: purchase.version, expiresOn: expiryDay,
    } })).status()).toBe(200);
    const payment = await create('purchases/' + purchase.id + '/payment-attempts', { idempotencyKey: purchase.id }, parent);
    expect((await parent.request.post('/api/v1/payment-attempts/' + payment.id + '/mock-outcome', {
      headers: guardianHeaders, data: { outcome: 'succeeded' },
    })).status()).toBe(200);
    const credit = async () => (await (await parent.request.get('/api/v1/students/amy/credits')).json())
      .items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id);
    const pack = await credit();
    const denied = await parent.request.post('/api/v1/students/amy/bookings', { headers: guardianHeaders, data: {
      classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion,
      creditPackageId: pack.id, idempotencyKey: purchase.id,
    } });
    expect(denied.status()).toBe(409);
    expect(await denied.json()).toMatchObject({ error: { code: 'credit_unavailable' } });
    expect(await credit()).toMatchObject({ available: 1, reserved: 0 });

    await parent.goto('/students/amy');
    await parent.getByRole('button', { name: 'Book a lesson' }).click();
    async function chooseLesson() {
      // Changing the date reloads classes and credits under the current school rule.
      await parent.getByLabel('Show classes from').fill(nextDay);
      await parent.getByLabel('Show classes from').fill(expiryDay);
      await choice(parent, lesson.id).check();
    }
    await chooseLesson();
    await expect(choice(parent, pack.id)).toHaveCount(0);
    await setRule('lesson_start');
    async function review() {
      await chooseLesson();
      await choice(parent, pack.id).check();
      await parent.getByRole('button', { name: 'Review booking' }).click();
      await expect(parent.getByText('The credit must be valid when the lesson starts.')).toBeVisible();
    }
    await review();
    await setRule('lesson_end');
    await parent.getByRole('button', { name: 'Confirm booking' }).click();
    await expect(parent.getByRole('alert')).toContainText('booking validity rule changed');
    expect(await credit()).toMatchObject({ available: 1, reserved: 0 });
    await setRule('lesson_start');
    await review();
    await parent.getByRole('button', { name: 'Confirm booking' }).click();
    const list = parent.getByRole('region', { name: 'Booked lessons' });
    await expect(list.getByText(kind.name, { exact: true })).toBeVisible();
    const booked = async () => (await (await parent.request.get('/api/v1/students/amy/bookings?from=' + expiryDay + '&to=' + nextDay)).json())
      .items.find((item: { class: { id: string } }) => item.class.id === lesson.id);
    const reservation = await booked();
    expect(reservation.status).toBe('reserved');
    await setRule('lesson_end');
    expect(await booked()).toEqual(reservation);
    expect(await credit()).toMatchObject({ available: 0, reserved: 1 });
  } finally {
    const current = await (await admin.request.get(settings)).json();
    if (current.validityRule !== original.validityRule) {
      expect((await admin.request.patch(settings, { headers, data: { validityRule: original.validityRule, version: current.version } })).status()).toBe(200);
    }
    await parent.close();
    await coach.close();
  }
});
