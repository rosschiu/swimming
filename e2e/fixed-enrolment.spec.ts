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
  await expect(page.getByRole('button', { name: 'Change dates' }).or(page.getByLabel('Bookings from date'))).toBeVisible(); if (await page.getByRole('button', { name: 'Change dates' }).isVisible()) await page.getByRole('button', { name: 'Change dates' }).click();
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

test('weekly enrolment rechecks the school rule across HK midnight and preserves an authorised replay', async ({ page: admin, browser }) => {
  test.setTimeout(60000);
  const headers = await login(admin, 'admin');
  const parent = await browser.newPage({ baseURL: 'http://localhost:18573' });
  const guardian = await login(parent, 'parent');
  const settings = '/api/v1/settings/booking-validity';
  const original = await (await admin.request.get(settings)).json();
  async function setRule(validityRule: string) {
    const current = await (await admin.request.get(settings)).json();
    if (current.validityRule === validityRule) return current;
    const saved = await admin.request.patch(settings, { headers, data: { validityRule, version: current.version } });
    expect(saved.status()).toBe(200);
    return saved.json();
  }
  async function create(path: string, data: object, actor = admin) {
    const res = await actor.request.post('/api/v1/' + path, { headers: actor === admin ? headers : guardian, data });
    expect(res.status(), await res.text()).toBe(201);
    return res.json();
  }
  try {
    await setRule('lesson_start');
    const stamp = Date.now();
    const first = Date.UTC(3400, 0, 1) + (stamp % 100000) * 86400000;
    const day = (offset: number) => new Date(first + offset * 86400000).toISOString().slice(0, 10);
    const venue = await create('venues', { name: 'Expiry pool ' + stamp, address: 'Synthetic pool', region: 'hk-island' });
    const kind = await create('class-types', { name: 'Expiry series ' + stamp, level: 'Beginner' });
    const group = { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 2 };
    const series = await create('class-series', { ...group, startDate: day(0), endDate: day(7), weekday: new Date(first).getUTCDay() || 7,
      startTime: '15:00', endTime: '16:00' });
    const later = series.sessions[1];
    const moved = await admin.request.patch('/api/v1/classes/' + later.id, { headers, data: { ...group, version: later.version,
      startsAt: day(7) + 'T23:30:00+08:00', endsAt: day(8) + 'T00:30:00+08:00' } });
    expect(moved.status(), await moved.text()).toBe(200);
    const offer = await create('package-offers', { name: 'Expiry credits ' + stamp, credits: 2, priceMinor: 10000, currency: 'HKD', active: true,
      policyOverride: { expiryEnabled: true, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: false, absenceRetainsCredit: false, illnessRetainsCredit: true } });
    const order = await create('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: offer.id }, parent);
    expect((await admin.request.post('/api/v1/purchases/' + order.id + '/expiry', { headers, data: { version: order.version, expiresOn: day(7) } })).status()).toBe(200);
    const payment = await create('purchases/' + order.id + '/payment-attempts', { idempotencyKey: order.id }, parent);
    expect((await parent.request.post('/api/v1/payment-attempts/' + payment.id + '/mock-outcome', { headers: guardian, data: { outcome: 'succeeded' } })).status()).toBe(200);
    async function balance() {
      const history = await (await parent.request.get('/api/v1/students/amy/credits')).json();
      return history.items.find((item: { purchaseId: string }) => item.purchaseId === order.id);
    }
    const pack = await balance();
    const path = '/api/v1/students/amy/enrolments';
    await parent.goto('/students/amy');
    await parent.getByRole('button', { name: 'Enrol in weekly lessons' }).click();
    await parent.getByLabel('Find weekly lessons from (Hong Kong)').fill(day(0));
    async function selectBoth() {
      await parent.getByRole('button', { name: 'Load weekly lessons and credits' }).click();
      await parent.getByLabel('Weekly series', { exact: true }).selectOption(series.id);
      for (const offset of [0, 7]) await parent.getByRole('checkbox', { name: new RegExp(day(offset)) }).check();
    }
    const packages = parent.getByLabel('Enrolment credit package');
    async function review() {
      await packages.selectOption(pack.id);
      await parent.getByRole('button', { name: 'Review enrolment' }).click();
      await expect(parent.getByText('The credit must be valid when each lesson starts.')).toBeVisible();
    }
    async function confirm() {
      const response = parent.waitForResponse(r => r.url().endsWith(path) && r.request().method() === 'POST');
      await parent.getByRole('button', { name: 'Confirm weekly enrolment' }).click();
      return response;
    }
    await selectBoth();
    await review();
    const strict = await setRule('lesson_end');
    const stale = await confirm();
    expect(stale.status()).toBe(409);
    await expect(parent.getByRole('alert')).toContainText('booking validity rule changed');
    await selectBoth();
    await expect(packages.locator(`option[value="${pack.id}"]`)).toHaveCount(0);
    const denied = await parent.request.post(path, { headers: guardian, data: { ...stale.request().postDataJSON(), bookingValidityVersion: strict.version } });
    expect(denied.status()).toBe(409);
    expect(await denied.json()).toMatchObject({ error: { code: 'credit_unavailable' } });
    expect(await balance()).toMatchObject({ available: 2, reserved: 0 });
    const bookings = await (await parent.request.get('/api/v1/students/amy/bookings?from=' + day(0) + '&to=' + day(8))).json();
    expect(bookings.items.filter((item: { class: { seriesId: string } }) => item.class.seriesId === series.id)).toHaveLength(0);
    await setRule('lesson_start');
    await selectBoth();
    await review();
    const saved = await confirm();
    expect(saved.status()).toBe(201);
    const result = await saved.json();
    expect(result.bookings).toHaveLength(2);
    const data = saved.request().postDataJSON();
    await setRule('lesson_end');
    const replay = await parent.request.post(path, { headers: guardian, data });
    expect(replay.status()).toBe(200);
    expect(await replay.json()).toEqual(result);
    expect(await balance()).toMatchObject({ available: 0, reserved: 2 });
    const link = '/api/v1/students/amy/relationships/parent';
    expect((await admin.request.delete(link, { headers })).ok()).toBe(true);
    try {
      expect((await parent.request.post(path, { headers: guardian, data })).status()).toBe(404);
    } finally {
      expect((await admin.request.put(link, { headers, data: { kind: 'guardian' } })).ok()).toBe(true);
    }
  } finally {
    await setRule(original.validityRule);
    await parent.close();
  }
});

const renewalSettingsPath = '/api/v1/settings/fixed-enrolment';
type RenewalSettings = components['schemas']['FixedEnrolmentSettings'];

const renewalTest = test.extend<{ renewalPolicy: void }>({
  renewalPolicy: async ({ playwright, baseURL }, use) => {
    const context = await playwright.request.newContext({ baseURL });
    try {
      const login = await context.post('/api/v1/auth/login', {
        headers: { Origin: baseURL! },
        data: { email: 'admin@swim.test', password: 'Swim-test-2026!' },
      });
      expect(login.ok(), await login.text()).toBe(true);
      const headers = { Origin: baseURL!, 'X-CSRF-Token': (await login.json()).csrfToken };
      async function readPolicy(): Promise<RenewalSettings> {
        const response = await context.get(renewalSettingsPath, { timeout: 10000 });
        expect(response.ok(), await response.text()).toBe(true);
        return response.json();
      }
      async function setPolicy(wanted: RenewalSettings, current: RenewalSettings) {
        if (wanted.allocationPolicy === current.allocationPolicy && wanted.renewalPolicy === current.renewalPolicy) return;
        const response = await context.patch(renewalSettingsPath, {
          headers, data: { ...wanted, version: current.version }, timeout: 10000,
        });
        expect(response.ok(), await response.text()).toBe(true);
      }
      const initial = await readPolicy();
      try {
        await setPolicy({ ...initial, allocationPolicy: 'all_or_none', renewalPolicy: 'same_offer' }, initial);
        await use();
      } finally {
        await setPolicy(initial, await readPolicy());
      }
    } finally {
      await context.dispose();
    }
  },
});

for (const carry of [true, false]) {
  renewalTest(`guardian confirms only available renewal weeks with carry-forward ${carry ? 'enabled' : 'disabled'}`, async ({ page: admin, browser, renewalPolicy: _renewalPolicy }) => {
    test.setTimeout(90000);
    const headers = await login(admin, 'admin');
    const parent = await browser.newPage({ baseURL: 'http://localhost:18573' });
    const guardian = await login(parent, 'parent');
    const path = '/api/v1/students/amy/enrolments';
    const stamp = Date.now();
    const start = Date.UTC(carry ? 3800 : 4200, 0, 1) + (stamp % 100000) * 86400000;
    const day = (week: number) => new Date(start + week * 7 * 86400000).toISOString().slice(0, 10);
    async function create(endpoint: string, data: object, actor = admin) {
      const response = await actor.request.post('/api/v1/' + endpoint, { headers: actor === admin ? headers : guardian, data });
      expect(response.status(), await response.text()).toBe(201);
      return response.json();
    }
    async function buy(name: string, credits: number) {
      const offer = await create('package-offers', { name: name + stamp, credits, priceMinor: 10000, currency: 'HKD', active: true,
        policyOverride: { expiryEnabled: false, carryForwardEnabled: carry, earlyCancellationRetainsCredit: true,
          lateCancellationRetainsCredit: false, absenceRetainsCredit: false, illnessRetainsCredit: true } });
      const order = await create('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: offer.id }, parent);
      const attempt = await create('purchases/' + order.id + '/payment-attempts', { idempotencyKey: order.id }, parent);
      expect((await parent.request.post('/api/v1/payment-attempts/' + attempt.id + '/mock-outcome',
        { headers: guardian, data: { outcome: 'succeeded' } })).status()).toBe(200);
      const { items } = await (await parent.request.get('/api/v1/students/amy/credits')).json();
      return items.find((pack: components['schemas']['CreditPackage']) => pack.purchaseId === order.id).id as string;
    }
    async function balances() {
      const { items } = await (await parent.request.get('/api/v1/students/amy/credits')).json();
      return Object.fromEntries(items.map((p: components['schemas']['CreditPackage']) => [p.id, [p.available, p.reserved]]));
    }
    try {
      await admin.getByRole('link', { name: 'School setup' }).click();
      await admin.getByRole('tab', { name: 'Bookings' }).click();
      const allocation = admin.getByLabel('When some weeks are unavailable');
      const renewal = admin.getByLabel('Packages eligible for renewal');
      await expect(allocation).toHaveValue('all_or_none');
      await expect(renewal).toHaveValue('same_offer');
      const venue = await create('venues', { name: 'Renewal pool ' + stamp, address: 'Synthetic pool', region: 'hk-island' });
      const kind = await create('class-types', { name: 'Renewal series ' + stamp, level: 'Beginner' });
      const group = { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', weekday: new Date(start).getUTCDay() || 7,
        startTime: '15:00', endTime: '16:00', capacity: 2 };
      const first = await create('class-series', { ...group, startDate: day(0), endDate: day(0) });
      const next = await create('class-series', { ...group, startDate: day(2), endDate: day(5) });
      const oldPack = await buy('Original credits ', 3);
      const newPack = await buy('Replacement credits ', 1);
      const lesson = first.sessions[0];
      const source = await create('students/amy/enrolments', { seriesId: first.id, creditPackageId: oldPack, idempotencyKey: first.id,
        sessions: [{ classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion }] }, parent);
      await parent.goto('/students/amy');
      await parent.getByRole('button', { name: 'Renew weekly enrolment' }).click();
      await parent.getByLabel('Find weekly lessons from (Hong Kong)').fill(day(2));
      async function selectWeeks() {
        await parent.getByRole('button', { name: 'Load weekly lessons and credits' }).click();
        await parent.getByLabel('Enrolment to renew').selectOption(source.id);
        await parent.getByLabel('Weekly series', { exact: true }).selectOption(next.id);
        for (const week of [2, 3, 4, 5]) await parent.getByRole('checkbox', { name: new RegExp(day(week)) }).check();
      }
      await selectWeeks();
      const packages = parent.getByLabel('Enrolment credit package');
      await expect(packages.locator('option', { hasText: newPack })).toHaveCount(0);
      await allocation.selectOption('confirm_available_subset');
      await renewal.selectOption('any_paid_package');
      const save = admin.getByRole('button', { name: 'Save fixed enrolment settings' });
      async function savePolicy(renewalPolicy: RenewalSettings['renewalPolicy']) {
        const pending = admin.waitForResponse(response => response.url().endsWith(renewalSettingsPath) && response.request().method() === 'PATCH');
        await save.click();
        const response = await pending;
        expect(response.ok(), await response.text()).toBe(true);
        expect(await response.json()).toMatchObject({ allocationPolicy: 'confirm_available_subset', renewalPolicy });
        await expect(admin.getByText('Fixed enrolment settings saved.')).toBeVisible();
      }
      await savePolicy('any_paid_package');
      const endpoint = path + '/' + source.id + '/renewals';
      async function submit(label: string) {
        const result = parent.waitForResponse(r => r.url().endsWith(endpoint) && r.request().method() === 'POST');
        await parent.getByRole('button', { name: label, exact: true }).click();
        return result;
      }
      async function preview() {
        await selectWeeks();
        await packages.selectOption(newPack);
        await parent.getByRole('button', { name: 'Review enrolment' }).click();
        const response = await submit('Confirm renewal');
        expect(response.status(), await response.text()).toBe(409);
        await expect(parent.getByText('Availability changed. Review the weeks below and confirm again.')).toBeVisible();
        const list = parent.getByRole('region', { name: 'Weekly enrolment' }).getByRole('listitem');
        await expect(list).toHaveCount(carry ? 3 : 1);
        await expect(list.last()).toContainText(day(carry ? 4 : 2));
        expect(await balances()).toMatchObject({ [oldPack]: [2, 1], [newPack]: [1, 0] });
      }
      await preview();
      await renewal.selectOption('same_offer');
      await savePolicy('same_offer');
      const stale = await submit('Confirm available weeks');
      expect(stale.status()).toBe(409);
      await expect(parent.getByRole('alert')).toContainText('fixed enrolment settings changed');
      await renewal.selectOption('any_paid_package');
      await savePolicy('any_paid_package');
      await preview();
      const saved = await submit('Confirm available weeks');
      expect(saved.status(), await saved.text()).toBe(201);
      const result = await saved.json();
      expectContract('EnrolmentRenewal', result);
      expect(result.carriedForwardCredits).toBe(carry ? 2 : 0);
      expect(result.bookings.map((booking: components['schemas']['StudentBooking']) => booking.class.id))
        .toEqual(next.sessions.slice(0, carry ? 3 : 1).map((c: {id: string}) => c.id));
      await expect(parent.getByText(new RegExp('Renewal confirmed.*' + (carry ? 2 : 0) + ' credits carried forward'))).toBeVisible();
      const replay = await parent.request.post(endpoint, { headers: guardian, data: saved.request().postDataJSON() });
      expect(replay.status()).toBe(200);
      expect(await replay.json()).toEqual(result);
      expect(await balances()).toMatchObject({ [oldPack]: [carry ? 0 : 2, 1], [newPack]: [0, carry ? 3 : 1] });
    } finally {
      await parent.close();
    }
  });
}
