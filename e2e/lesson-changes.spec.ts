import { expect, test, type Page } from '@playwright/test';
import type { components } from '../apps/web/src/generated/api';
import { expectContract } from './contract';

type Booking = components['schemas']['StudentBooking'];
type CreditPackage = components['schemas']['CreditPackage'];

async function signIn(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(role + '@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const session = await (await page.request.get('/api/v1/auth/me')).json();
  return { Origin: 'http://localhost:18573', 'X-CSRF-Token': session.csrfToken };
}

test('fixed booking stays reserved on a full replacement and follows coach approval when a place opens', async ({ page, browser }) => {
  test.setTimeout(60000);
  const admin = await browser.newPage({ baseURL: 'http://localhost:18573' });
  try {
    const adminHeaders = await signIn(admin, 'admin');
    const guardianHeaders = await signIn(page, 'parent');
    async function write(actor: Page, path: string, data: object, method = 'POST', expected = 201) {
      const response = await actor.request.fetch('/api/v1/' + path, {
        method, headers: actor === admin ? adminHeaders : guardianHeaders, data,
      });
      expect(response.status(), await response.text()).toBe(expected);
      return response.json();
    }
    async function read(actor: Page, path: string) {
      const response = await actor.request.get('/api/v1/' + path);
      expect(response.status(), await response.text()).toBe(200);
      return response.json();
    }
    const stamp = Date.now();
    const start = Date.UTC(4000, 0, 1) + (stamp % 100000) * 86_400_000;
    const day = (offset: number) => new Date(start + offset * 86_400_000).toISOString().slice(0, 10);
    const venue = await write(admin, 'venues', { name: 'Lesson change pool ' + stamp, address: 'Synthetic pool', region: 'hk-island' });
    const kind = await write(admin, 'class-types', { name: 'Lesson change group ' + stamp, level: 'Beginner' });
    const classInput = { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 1 };
    const series = await write(admin, 'class-series', { ...classInput, startDate: day(0), endDate: day(7),
      weekday: new Date(start).getUTCDay() || 7, startTime: '10:00', endTime: '11:00' });
    const targetInput = { ...classInput, startsAt: day(1) + 'T10:00:00+08:00', endsAt: day(1) + 'T11:00:00+08:00' };
    const target = await write(admin, 'classes', targetInput);
    const other = await write(admin, 'students', { name: 'Lesson change swimmer ' + stamp, level: 'Beginner' });
    await write(admin, 'students/' + other.id + '/relationships/parent', { kind: 'guardian' }, 'PUT', 200);
    const offer = await write(admin, 'package-offers', { name: 'Lesson change credits ' + stamp, credits: 3, priceMinor: 10000,
      currency: 'HKD', active: true, policyOverride: { expiryEnabled: false, carryForwardEnabled: false,
        earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: false, absenceRetainsCredit: false, illnessRetainsCredit: true } });
    async function credit(student: string, purchaseId: string, actor = page) {
      const { items } = await read(actor, 'students/' + student + '/credits') as { items: CreditPackage[] };
      const result = items.find(item => item.purchaseId === purchaseId);
      expect(result).toBeDefined();
      return result!;
    }
    async function purchase(student: string) {
      const order = await write(page, 'students/' + student + '/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: crypto.randomUUID() });
      const attempt = await write(page, 'purchases/' + order.id + '/payment-attempts', { idempotencyKey: order.id });
      await write(page, 'payment-attempts/' + attempt.id + '/mock-outcome', { outcome: 'succeeded' }, 'POST', 200);
      return credit(student, order.id);
    }
    const amy = await purchase('amy');
    const otherCredit = await purchase(other.id);
    const versions = (lesson: components['schemas']['ClassSession']) => ({ classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion });
    const enrolment = await write(page, 'students/amy/enrolments', { seriesId: series.id, creditPackageId: amy.id,
      idempotencyKey: series.id, sessions: series.sessions.map(versions) });
    const [original, sibling] = enrolment.bookings as Booking[];
    const path = 'students/amy/bookings?from=' + day(0) + '&to=' + day(7);
    async function assertBookings(actor: Page, classId: string) {
      const { items } = await read(actor, path) as { items: Booking[] };
      expect(items.find(item => item.id === original.id)?.class.id).toBe(classId);
      expect(items.find(item => item.id === sibling.id)).toEqual(sibling);
    }
    const before = await credit('amy', amy.purchaseId!);
    expect(before).toMatchObject({ available: 1, reserved: 2, consumed: 0 });

    async function openBookings() {
      await page.goto('/students/amy');
      await page.getByLabel('Bookings from date').fill(day(0));
      await page.getByLabel('Bookings to date').fill(day(7));
      await page.getByRole('button', { name: 'Load booked lessons' }).click();
      return page.getByRole('region', { name: 'Booked lessons' });
    }
    const bookings = await openBookings();
    await bookings.getByRole('listitem').filter({ hasText: day(0) }).getByRole('button', { name: 'Request lesson change' }).click();
    await page.getByLabel('New lesson from date').fill(day(1));
    await page.getByLabel('New lesson to date').fill(day(1));
    async function selectTarget() {
      const choices = page.waitForResponse(res => res.url().includes('/change-options?'));
      await page.getByRole('button', { name: 'Find replacement lessons' }).click();
      expectContract('LessonChangeOptions', await (await choices).json());
      await page.getByLabel('Replacement lesson', { exact: true }).selectOption(target.id);
    }
    await selectTarget();
    const shifted = { ...targetInput, startsAt: day(1) + 'T12:00:00+08:00', endsAt: day(1) + 'T13:00:00+08:00' };
    const updated = await write(admin, 'classes/' + target.id, { ...shifted, version: target.version }, 'PATCH', 200);
    await page.getByRole('button', { name: 'Send change request' }).click();
    await expect(page.getByRole('alert')).toHaveText('The replacement lesson changed. Search again to review the latest details.');
    await selectTarget();
    const requested = page.waitForResponse(res => res.url().endsWith('/change-requests'));
    await page.getByRole('button', { name: 'Send change request' }).click();
    const saved = await requested;
    expect(saved.status()).toBe(201);
    const change = await saved.json();
    expectContract('LessonChange', change);
    await expect(page.getByText(/Lesson change request received/)).toBeVisible();
    await assertBookings(page, original.class.id);
    const rescheduled = await write(admin, 'classes/' + target.id, { ...targetInput, version: updated.version }, 'PATCH', 200);

    async function switchRole(role: string) {
      await page.getByRole('button', { name: 'Sign out' }).click();
      await signIn(page, role);
    }
    await switchRole('coach');
    await page.getByRole('link', { name: 'Lesson changes' }).click();
    const request = page.getByRole('listitem').filter({ hasText: kind.name });
    async function roster(classId: string) {
      const result: components['schemas']['ClassRoster'] = await read(page, 'classes/' + classId + '/roster');
      return result.items.map(item => item.bookingId);
    }
    await request.getByRole('button', { name: 'Approve change' }).click();
    await expect(request.getByRole('alert')).toHaveText('The replacement lesson changed. Reject this request so the guardian can review the new details and submit again.');
    await assertBookings(admin, original.class.id);
    expect(await credit('amy', amy.purchaseId!, admin)).toEqual(before);
    const restored = await write(admin, 'classes/' + target.id, { ...shifted, version: rescheduled.version }, 'PATCH', 200);
    await write(admin, 'students/' + other.id + '/bookings', { ...versions(restored), creditPackageId: otherCredit.id, idempotencyKey: target.id });
    await request.getByRole('button', { name: 'Approve change' }).click();
    await expect(request.getByRole('alert')).toHaveText('The replacement lesson is full. Try again if a place opens.');
    await expect(request.getByText('Pending', { exact: true })).toBeVisible();
    await assertBookings(admin, original.class.id);
    expect(await credit('amy', amy.purchaseId!, admin)).toEqual(before);
    expect(await roster(original.class.id)).toEqual([original.id]);
    expect(await roster(target.id)).not.toContain(original.id);

    await write(admin, 'classes/' + target.id, { ...shifted, version: restored.version, capacity: 2 }, 'PATCH', 200);
    await request.getByRole('button', { name: 'Approve change' }).click();
    await expect(page.getByText('Lesson change approved.', { exact: true })).toBeVisible();
    await expect(request).toHaveCount(0);
    await page.getByRole('checkbox', { name: 'Show pending requests only' }).uncheck();
    await expect(request.getByText('Approved', { exact: true })).toBeVisible();
    await assertBookings(admin, target.id);
    expect(await roster(original.class.id)).toEqual([]);
    expect(await roster(target.id)).toContain(original.id);
    expect(await roster(sibling.class.id)).toEqual([sibling.id]);

    await admin.getByRole('link', { name: 'Lesson changes' }).click();
    const history = admin.getByRole('listitem').filter({ hasText: kind.name });
    await expect(history.getByText(/Requester parent/)).toBeVisible();
    await expect(history.getByText(/Decided by coach/)).toBeVisible();
    await expect(history.getByRole('button', { name: 'Approve change' })).toHaveCount(0);

    for (const role of ['parent', 'student']) {
      await switchRole(role);
      await expect((await openBookings()).getByRole('listitem').filter({ hasText: day(1) })).toContainText(kind.name);
      await assertBookings(page, target.id);
      if (role === 'parent') expect(await credit('amy', amy.purchaseId!)).toEqual(before);
    }
  } finally {
    await admin.close();
  }
});
