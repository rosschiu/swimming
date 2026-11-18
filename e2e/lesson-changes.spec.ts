import { execFileSync } from 'node:child_process';
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
    const queueLink = admin.getByRole('link', { name: 'Pending lesson change requests' });
    await expect(queueLink).toHaveAccessibleDescription('0');
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
      await expect(page.getByRole('button', { name: 'Change dates' }).or(page.getByLabel('Bookings from date'))).toBeVisible(); if (await page.getByRole('button', { name: 'Change dates' }).isVisible()) await page.getByRole('button', { name: 'Change dates' }).click();
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
      await page.locator(`input[type=radio][value="${target.id}"]`).check();
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
    const replay = await page.request.post(saved.url(), { headers: guardianHeaders, data: saved.request().postDataJSON() });
    expect(replay.status()).toBe(200);
    expect((await replay.json()).id).toBe(change.id);
    await expect(page.getByText(/Lesson change request received/)).toBeVisible();
    await assertBookings(page, original.class.id);
    const rescheduled = await write(admin, 'classes/' + target.id, { ...targetInput, version: updated.version }, 'PATCH', 200);

    async function switchRole(role: string) {
      await page.getByRole('button', { name: 'Sign out' }).click();
      await signIn(page, role);
    }
    await switchRole('coach');
    const coachDetail = '/lesson-changes/amy/' + change.id;
    await page.getByRole('link', { name: /^Notifications/ }).click();
    const noticeLink = page.locator('a[href="' + coachDetail + '"]');
    await expect(noticeLink).toHaveAccessibleName('View lesson change request');
    await expect(page.getByRole('article').filter({ has: noticeLink })).toContainText('A guardian requested a lesson change.');
    const initialInbox = await read(page, 'notifications?limit=100');
    const matching = initialInbox.items.filter((entry: { sourceId: string }) => entry.sourceId === change.id);
    expect(matching).toHaveLength(1);
    expect(matching[0]).toMatchObject({ kind: 'lesson_change.requested', readAt: null });
    await noticeLink.focus();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('main')).toBeFocused();
    await page.reload();
    await expect(page.getByRole('button', { name: 'Approve change' })).toBeVisible();
    await page.getByRole('link', { name: 'Lesson changes', exact: true }).click();
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

    await admin.reload();
    await expect(queueLink).toHaveAccessibleDescription('1');
    await expect(queueLink).toHaveAttribute('href', '/lesson-changes?status=pending');
    await queueLink.click();
    const history = admin.getByRole('listitem').filter({ hasText: kind.name });
    await expect(history.getByText('Pending', { exact: true })).toBeVisible();
    await expect(history.getByRole('button')).toHaveCount(0);

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

    await page.goto(coachDetail);
    await expect(page.getByText('Approved', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Approve change' })).toHaveCount(0);
    const newCoach = 'reassigned-' + stamp;
    execFileSync('psql', [process.env.DATABASE_URL!, '-Xq', '-v', 'ON_ERROR_STOP=1', '-v', 'newCoach=' + newCoach], {
      input: `INSERT INTO users(id,email,name,role,password_hash)
        SELECT :'newCoach', :'newCoach'||'@swim.test','Replacement coach','coach',password_hash FROM users WHERE id='coach';`,
    });
    const sourceClass = await read(admin, 'classes/' + original.class.id);
    await write(admin, 'classes/' + sourceClass.id, { ...classInput, coachId: newCoach, startsAt: sourceClass.startsAt,
      endsAt: sourceClass.endsAt, version: sourceClass.version }, 'PATCH', 200);
    const currentCoach = await read(page, 'auth/me');
    const oldInbox = await read(page, 'notifications?limit=100');
    expect(oldInbox.items.some((entry: { sourceId: string }) => entry.sourceId === change.id)).toBe(false);
    expect(oldInbox.unreadCount).toBe(initialInbox.unreadCount - 1);
    const denied = await page.request.put('/api/v1/notifications/' + matching[0].id + '/read', {
      headers: { Origin: 'http://localhost:18573', 'X-CSRF-Token': currentCoach.csrfToken },
    });
    expect(denied.status()).toBe(404);
    await page.reload();
    expect((await page.request.get('/api/v1/students/amy/lesson-changes/' + change.id)).status()).toBe(404);
    await expect(page.getByText(kind.name, { exact: false })).toHaveCount(0);
    await switchRole(newCoach);
    const replacementInbox = await read(page, 'notifications?limit=100');
    expect(replacementInbox.items.some((entry: { sourceId: string }) => entry.sourceId === change.id)).toBe(false);
    expect((await page.request.get('/api/v1/students/amy/lesson-changes/' + change.id)).status()).toBe(200);

    await admin.goBack();
    await expect(queueLink).toHaveAccessibleDescription('0');
    await admin.goForward();
    await expect(admin).toHaveURL(/\/lesson-changes\?status=pending$/);
    await admin.reload();
    await expect(admin.getByText('No pending lesson change requests.', { exact: true })).toBeVisible();
    await admin.getByRole('link', { name: 'View all lesson changes' }).click();
    await expect(admin).toHaveURL(/\/lesson-changes$/);
    await expect(history.getByText(/Requester parent/)).toBeVisible();
    await expect(history.getByText(/Decided by coach/)).toBeVisible();
    await expect(history.getByRole('button', { name: 'Approve change' })).toHaveCount(0);

    for (const role of ['parent', 'student']) {
      await switchRole(role);
      await expect((await openBookings()).getByRole('listitem').filter({ hasText: day(1) })).toContainText(kind.name);
      await assertBookings(page, target.id);
      if (role === 'parent') expect(await credit('amy', amy.purchaseId!)).toEqual(before);
    }
    const detailPath = '/students/amy/lesson-changes/' + change.id;
    let rejectedID = '';
    const relationship = '/api/v1/students/amy/relationships/other-parent';
    expect((await admin.request.put(relationship, { headers: adminHeaders, data: { kind: 'guardian' } })).status()).toBe(200);
    try {
      await switchRole('parent');
      const rejectionTarget = await write(admin, 'classes', { ...classInput, startsAt: day(3) + 'T10:00:00+08:00', endsAt: day(3) + 'T11:00:00+08:00' });
      const guardianSession = await read(page, 'auth/me');
      const secondResponse = await page.request.post('/api/v1/students/amy/bookings/' + sibling.id + '/change-requests', {
        headers: { Origin: 'http://localhost:18573', 'X-CSRF-Token': guardianSession.csrfToken },
        data: { targetClassId: rejectionTarget.id, targetClassVersion: rejectionTarget.version, idempotencyKey: rejectionTarget.id },
      });
      expect(secondResponse.status(), await secondResponse.text()).toBe(201);
      const second = await secondResponse.json();
      rejectedID = second.id;
      await switchRole('coach');
      await page.getByRole('link', { name: 'Lesson changes' }).click();
      const toReject = page.getByRole('listitem').filter({ hasText: kind.name });
      const reason = 'Keep the original week ' + stamp;
      await toReject.getByRole('textbox').fill(reason);
      await toReject.getByRole('button', { name: 'Reject change' }).click();
      await expect(page.getByText('Lesson change rejected.', { exact: true })).toBeVisible();

      await switchRole('parent');
      await page.goto('/notifications');
      for (const [record, label] of [[change, 'Your lesson change was approved.'], [second, 'Your lesson change was rejected.']] as const) {
        const card = page.getByRole('article').filter({ hasText: label }).filter({
          has: page.locator('a[href="/students/amy/lesson-changes/' + record.id + '"]'),
        });
        const detailLink = card.getByRole('link', { name: 'View lesson change decision' });
        const detailPath = '/students/amy/lesson-changes/' + record.id;
        await expect(detailLink).toHaveAttribute('href', detailPath);
        await card.getByRole('button', { name: 'Mark read' }).click();
        await expect(card.getByRole('button', { name: 'Mark read' })).toHaveCount(0);
        await detailLink.click();
        await expect(page).toHaveURL(url => url.pathname === detailPath);
        await page.reload();
        await expect(page.getByText(kind.name, { exact: false }).first()).toBeVisible();
        if (record.id === second.id) await expect(page.getByText(reason, { exact: false })).toBeVisible();
        await page.goBack();
      }
      const noticeList = await read(page, 'notifications?limit=100');
      const decisionNotices = noticeList.items.filter((item: { sourceId: string }) => [change.id, second.id].includes(item.sourceId));
      expect(decisionNotices).toHaveLength(2);
      const requesterLink = '/api/v1/students/amy/relationships/parent';
      expect((await admin.request.delete(requesterLink, { headers: adminHeaders })).status()).toBe(204);
      try {
        await page.reload();
        await expect(page.getByRole('heading', { name: 'Notifications', exact: true })).toBeVisible();
        const revoked = await read(page, 'notifications?limit=100');
        expect(revoked.items.some((item: { sourceId: string }) => [change.id, second.id].includes(item.sourceId))).toBe(false);
        const auth = await read(page, 'auth/me');
        const mark = await page.request.put('/api/v1/notifications/' + decisionNotices[0].id + '/read', {
          headers: { Origin: 'http://localhost:18573', 'X-CSRF-Token': auth.csrfToken },
        });
        expect(mark.status()).toBe(404);
        expect((await page.request.get('/api/v1' + detailPath)).status()).toBe(404);
      } finally {
        expect((await admin.request.put(requesterLink, { headers: adminHeaders, data: { kind: 'guardian' } })).status()).toBe(200);
      }
      await switchRole('other-parent');
      const inbox = await read(page, 'notifications?limit=100');
      expect(inbox.items.some((item: { sourceId: string }) => [change.id, second.id].includes(item.sourceId))).toBe(false);
      const shared = await read(page, 'lesson-changes');
      expect(shared.items.some((item: { id: string }) => item.id === change.id)).toBe(true);
      expect((await page.request.get('/api/v1' + detailPath)).status()).toBe(200);
      expect((await page.request.get('/api/v1/students/ben/lesson-changes/' + change.id)).status()).toBe(404);
      await page.goto(detailPath);
      await expect(page.getByText(kind.name, { exact: false }).first()).toBeVisible();
    } finally {
      expect((await admin.request.delete(relationship, { headers: adminHeaders })).status()).toBe(204);
    }
    await page.reload();
    expect((await page.request.get('/api/v1' + detailPath)).status()).toBe(404);
    await expect(page.getByText(kind.name, { exact: false })).toHaveCount(0);
    const deliveryCount = execFileSync('psql', [process.env.DATABASE_URL!, '-XqtA', '-v', 'ON_ERROR_STOP=1', '-v', 'approved=' + change.id, '-v', 'rejected=' + rejectedID], {
      encoding: 'utf8', input: "SELECT count(*) FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.source_id IN (:'approved', :'rejected');",
    }).trim();
    expect(deliveryCount).toBe('0');
  } finally {
    await admin.close();
  }
});
