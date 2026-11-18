import { execFileSync } from 'node:child_process';
import { expect, test, type Page } from '@playwright/test';

async function login(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(role + '@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const session = await (await page.request.get('/api/v1/auth/me')).json();
  return { Origin: 'http://localhost:18573', 'X-CSRF-Token': session.csrfToken };
}

test('administrator reviews both ends of a pending move without changing either class', async ({ page, browser }) => {
  const parent = await browser.newPage({ baseURL: 'http://localhost:18573' });
  try {
    const adminHeaders = await login(page, 'admin');
    const familyHeaders = await login(parent, 'parent');
    async function create(actor: Page, path: string, data: object, status = 201) {
      const response = await actor.request.post('/api/v1/' + path, { headers: actor === page ? adminHeaders : familyHeaders, data });
      expect(response.status(), await response.text()).toBe(status);
      return response.json();
    }
    const stamp = Date.now();
    const date = new Date(Date.UTC(3600, 0, 1) + (stamp % 100000) * 86400000).toISOString().slice(0, 10);
    const venue = await create(page, 'venues', { name: 'Impact pool ' + stamp, address: 'Synthetic pool', region: 'hk-island' });
    const kind = await create(page, 'class-types', { name: 'Impact group ' + stamp, level: 'Beginner' });
    async function lesson(hour: string, endHour: string) {
      return create(page, 'classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 2,
        startsAt: date + 'T' + hour + ':00:00+08:00', endsAt: date + 'T' + endHour + ':00:00+08:00' });
    }
    const original = await lesson('10', '11');
    const target = await lesson('13', '14');
    const offer = await create(page, 'package-offers', { name: 'Impact credit ' + stamp, credits: 2, priceMinor: 10000, currency: 'HKD', active: true,
      policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: false, absenceRetainsCredit: false, illnessRetainsCredit: true } });
    const purchase = await create(parent, 'students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: offer.id });
    const attempt = await create(parent, 'purchases/' + purchase.id + '/payment-attempts', { idempotencyKey: purchase.id });
    await create(parent, 'payment-attempts/' + attempt.id + '/mock-outcome', { outcome: 'succeeded' }, 200);
    const credits = await (await parent.request.get('/api/v1/students/amy/credits')).json();
    const pack = credits.items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id);
    const booking = await create(parent, 'students/amy/bookings', { classId: original.id, classVersion: original.version, classTypeVersion: original.classTypeVersion,
      creditPackageId: pack.id, idempotencyKey: original.id });
    const change = await create(parent, 'students/amy/bookings/' + booking.id + '/change-requests', {
      targetClassId: target.id, targetClassVersion: target.version, idempotencyKey: booking.id,
    });
    function snapshot() {
      const tables = ['bookings', 'booking_attendance', 'class_sessions', 'credit_movements', 'lesson_change_requests', 'receipts', 'notifications', 'enrolments', 'audit_events'];
      const sql = tables.map(table => "SELECT md5(COALESCE(string_agg(to_jsonb(t)::text, '' ORDER BY to_jsonb(t)::text),'')) FROM " + table + ' t;').join('\n');
      return execFileSync('psql', [process.env.DATABASE_URL!, '-XqtA', '-v', 'ON_ERROR_STOP=1'], { input: sql, encoding: 'utf8' });
    }
    await page.getByRole('link', { name: 'Schedule', exact: true }).click();
    await page.getByLabel('From date', { exact: true }).fill(date);
    await page.getByLabel('To date', { exact: true }).fill(date);
    await page.getByRole('button', { name: 'Apply filters' }).click();
    const buttons = page.getByRole('button', { name: /Review cancellation impact/ });
    await expect(buttons).toHaveCount(2);
    const before = snapshot();
    const readPath = (id: string) => '/api/v1/classes/' + id + '/cancellation-impact';
    const writes: string[] = [];
    page.on('request', request => {
      if (request.url().includes('/api/v1/') && request.method() !== 'GET') writes.push(request.method() + ' ' + request.url());
    });
    for (const [index, id] of [original.id, target.id].entries()) {
      const read = page.waitForResponse(response => new URL(response.url()).pathname === readPath(id));
      await buttons.nth(index).click();
      const response = await read;
      expect(response.status()).toBe(200);
      const impact = await response.json();
      const counts = index === 0 ? [1, 1, 0, 0, 1] : [0, 0, 0, 1, 0];
      expect(impact.counts).toMatchObject(Object.fromEntries(['bookedSwimmers', 'unmarkedBookings', 'recordedAttendance', 'pendingInbound', 'pendingOutbound'].map((key, i) => [key, counts[i]])));
      const panel = page.getByRole('region', { name: 'Cancellation impact' });
      await panel.getByLabel('Impact details').selectOption(index === 0 ? 'outbound' : 'inbound');
      await expect(panel).toContainText('Amy');
      await expect(panel.getByRole('button', { name: /Confirm cancellation|Cancel class/i })).toHaveCount(0);
    }
    expect(writes).toEqual([]);
    expect(snapshot()).toBe(before);
    for (const role of ['parent', 'student', 'coach']) {
      if (role !== 'parent') { await parent.getByRole('button', { name: 'Sign out' }).click(); await login(parent, role); }
      expect((await parent.request.get(readPath(original.id))).status()).toBe(403);
      await parent.getByRole('link', { name: 'Schedule', exact: true }).click();
      await expect(parent.getByRole('button', { name: /Review cancellation impact/ })).toHaveCount(0);
    }
    const coachSession = await (await parent.request.get('/api/v1/auth/me')).json();
    const decision = await parent.request.put('/api/v1/lesson-changes/' + change.id + '/decision', {
      headers: { Origin: 'http://localhost:18573', 'X-CSRF-Token': coachSession.csrfToken }, data: { decision: 'approved' },
    });
    expect(decision.status()).toBe(200);
    for (const [index, id] of [original.id, target.id].entries()) {
      const read = page.waitForResponse(response => new URL(response.url()).pathname === readPath(id));
      await buttons.nth(index).click();
      const impact = await (await read).json();
      expect(impact.counts).toMatchObject({ bookedSwimmers: index, unmarkedBookings: index, recordedAttendance: 0, pendingInbound: 0, pendingOutbound: 0 });
    }
    const exact = await page.request.get('/api/v1/students/amy/bookings/' + booking.id);
    expect(await exact.json()).toMatchObject({ id: booking.id, class: { id: target.id } });
    const after = await (await page.request.get('/api/v1/students/amy/credits')).json();
    expect(after.items.find((item: { id: string }) => item.id === pack.id)).toMatchObject({ available: 1, reserved: 1, consumed: 0 });
  } finally {
    await parent.close();
  }
});
