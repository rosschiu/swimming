import { expect, test, type Page } from '@playwright/test';
import { expectContract } from './contract';

test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

const tabs = (page: Page) => page.getByRole('navigation', { name: 'Main navigation' });

async function signIn(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(role + '@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(tabs(page)).toBeVisible();
  const session = await (await page.request.get('/api/v1/auth/me')).json();
  return { Origin: 'http://localhost:18573', 'X-CSRF-Token': session.csrfToken };
}
async function signOut(page: Page) {
  await tabs(page).getByRole('button', { name: 'More' }).click();
  await page.getByRole('dialog', { name: 'More' }).getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
}
async function expectNoSidewaysScroll(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
}

test('guardian and coach land on a phone home with the next lesson, credits and roster', async ({ page }) => {
  test.setTimeout(90000);
  const suffix = Date.now();
  const date = new Date(Date.UTC(2300, 0, 1) + (suffix % 30000) * 86400000).toISOString().slice(0, 10);
  let headers = await signIn(page, 'admin');
  await expect(page.getByRole('region', { name: 'Action queue' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Today’s lessons' })).toBeVisible();
  await expectNoSidewaysScroll(page);
  async function post(path: string, data: object, status = 201) {
    const response = await page.request.post('/api/v1/' + path, { headers, data });
    expect(response.status(), await response.text()).toBe(status);
    return response.json();
  }
  const venue = await post('venues', { name: 'Mobile pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await post('class-types', { name: 'Mobile lesson ' + suffix, level: 'Beginner' });
  const lesson = await post('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 4,
    startsAt: date + 'T17:00:00+08:00', endsAt: date + 'T18:00:00+08:00' });
  const swimmer = await post('students', { name: 'Mobile swimmer ' + suffix, level: 'Beginner' });
  expect((await page.request.put(`/api/v1/students/${swimmer.id}/relationships/parent`, { headers, data: { kind: 'guardian' } })).status()).toBe(200);
  const offer = await post('package-offers', { name: 'Mobile credits ' + suffix, credits: 2, priceMinor: 10000, currency: 'HKD', active: true,
    policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: true, absenceRetainsCredit: true, illnessRetainsCredit: true } });
  await signOut(page);

  headers = await signIn(page, 'parent');
  const card = page.getByRole('region', { name: swimmer.name });
  await expect(card).toContainText('No upcoming lessons booked.');
  await expect(card.getByRole('link', { name: 'Buy lessons' })).toBeVisible();
  const purchase = await post(`students/${swimmer.id}/purchases`, { offerId: offer.id, offerVersion: offer.version, idempotencyKey: 'mobile-' + suffix });
  const attempt = await post(`purchases/${purchase.id}/payment-attempts`, { idempotencyKey: 'mobile-pay-' + suffix });
  await post(`payment-attempts/${attempt.id}/mock-outcome`, { outcome: 'succeeded' }, 200);
  const credits = await (await page.request.get(`/api/v1/students/${swimmer.id}/credits`)).json();
  const pack = credits.items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id);
  const booking = await post(`students/${swimmer.id}/bookings`, { classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion, creditPackageId: pack.id, idempotencyKey: 'mobile-book-' + suffix });

  const summary = page.waitForResponse(r => r.url().endsWith('/api/v1/home'));
  await page.reload();
  expectContract('Home', await (await summary).json());
  await expect(card).toContainText('1 lesson left to book');
  await expect(card.getByRole('link', { name: 'Buy lessons' })).toHaveCount(0);
  const next = card.getByRole('link', { name: new RegExp(`17:00–18:00.*${kind.name}.*${venue.name}`) });
  await expect(next).toBeVisible();
  await expectNoSidewaysScroll(page);
  await next.click();
  await expect(page).toHaveURL(`/students/${swimmer.id}/bookings/${booking.id}`);

  for (const [label, path] of [['Children', '/students'], ['Packages', '/packages'], ['Home', '/home']] as const) {
    await tabs(page).getByRole('link', { name: label }).click();
    await expect(page).toHaveURL(path);
    await expectNoSidewaysScroll(page);
  }
  await page.goto(`/students/${swimmer.id}`);
  await expect(page.getByRole('heading', { name: swimmer.name })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Lessons' })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('region', { name: 'Booked lessons' })).toBeVisible();
  await page.getByRole('tab', { name: 'Credits' }).click();
  await expect(page.getByRole('heading', { name: 'Credit history' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Booked lessons' })).toBeHidden();
  await expectNoSidewaysScroll(page);
  await tabs(page).getByRole('button', { name: 'More' }).click();
  await page.getByRole('dialog', { name: 'More' }).getByRole('link', { name: 'Schedule' }).click();
  await expect(page).toHaveURL('/schedule');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expectNoSidewaysScroll(page);
  await signOut(page);

  await signIn(page, 'coach');
  await expect(tabs(page).getByRole('link', { name: 'Today' })).toHaveAttribute('aria-current', 'page');
  const lessons = page.getByRole('region', { name: 'Your lessons' });
  const roster = lessons.getByRole('link').first();
  await expect(roster).toBeVisible();
  await roster.click();
  await expect(page).toHaveURL(/\/schedule\/[^/]+\/roster$/);
  await expect(page.getByRole('region', { name: 'Class roster' })).toBeVisible();
  await expectNoSidewaysScroll(page);
});

test('administrator setup and accounts show one area at a time on a phone', async ({ page }) => {
  await signIn(page, 'admin');
  const height = () => page.evaluate(() => document.documentElement.scrollHeight);
  const sizes: Record<string, number> = {};
  await page.goto('/setup');
  await expect(page.getByRole('heading', { name: 'Venues' })).toBeVisible();
  for (const [tab, heading] of [['Places', 'Venues'], ['Skills', 'Skills'], ['Coach hours', 'Availability editing cutoff'], ['Lessons', 'Lesson timing'], ['Bookings', 'Booking validity']]) {
    await page.getByRole('tab', { name: tab }).click();
    await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible();
    await expectNoSidewaysScroll(page);
    sizes['setup ' + tab] = await height();
  }
  await expect(page).toHaveURL(/\/setup\?section=bookings$/);
  await page.reload();
  await expect(page.getByRole('tab', { name: 'Bookings' })).toHaveAttribute('aria-selected', 'true');
  await page.goto('/accounts');
  for (const [tab, heading] of [['Accounts', 'School accounts'], ['Invite', 'Invite a member'], ['Invitations', 'Invitation history']]) {
    await page.getByRole('tab', { name: tab, exact: true }).click();
    await expect(page.getByRole('heading', { name: heading })).toBeVisible();
    await expectNoSidewaysScroll(page);
    sizes['accounts ' + tab] = await height();
  }
  test.info().annotations.push({ type: 'phone page heights', description: JSON.stringify(sizes) });
  expect(sizes['setup Lessons']).toBeLessThan(2500);
  expect(sizes['accounts Invite']).toBeLessThan(2000);
});
