import { expect, test, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
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

test('the class coach marks a swimmer present and shares lesson feedback the family sees at once', async ({ page }) => {
  test.setTimeout(90000);
  let headers = await signIn(page, 'admin');
  async function switchRole(role: string) {
    await page.getByRole('button', { name: 'Sign out' }).click();
    headers = await signIn(page, role);
  }
  async function post(path: string, data: object, status = 201) {
    const response = await page.request.post('/api/v1/' + path, { headers, data });
    expect(response.status(), await response.text()).toBe(status);
    return response.json();
  }
  const suffix = Date.now();
  const date = new Date(Date.UTC(2800, 0, 1) + (suffix % 100000) * 86400000).toISOString().slice(0, 10);
  const venue = await post('venues', { name: 'Feedback pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await post('class-types', { name: 'Feedback lesson ' + suffix, level: 'Beginner' });
  const lesson = await post('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 1,
    startsAt: date + 'T10:00:00+08:00', endsAt: date + 'T11:00:00+08:00' });
  const offer = await post('package-offers', { name: 'Feedback package ' + suffix, credits: 1, priceMinor: 10000, currency: 'HKD', active: true,
    policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: false, absenceRetainsCredit: false, illnessRetainsCredit: true } });
  await switchRole('parent');
  const purchase = await post('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: 'feedback-order-' + suffix });
  const attempt = await post('purchases/' + purchase.id + '/payment-attempts', { idempotencyKey: 'feedback-attempt-' + suffix });
  await post('payment-attempts/' + attempt.id + '/mock-outcome', { outcome: 'succeeded' }, 200);
  const credits = await (await page.request.get('/api/v1/students/amy/credits')).json();
  const pack = credits.items.find((item: { purchaseId?: string }) => item.purchaseId === purchase.id);
  await post('students/amy/bookings', { classId: lesson.id, classVersion: lesson.version,
    classTypeVersion: lesson.classTypeVersion, creditPackageId: pack.id, idempotencyKey: 'feedback-booking-' + suffix });

  // Move the synthetic lesson into the past so attendance is open.
  const dsn = process.env.DATABASE_URL;
  expect(dsn, 'dedicated synthetic database required').toBeTruthy();
  execFileSync('psql', [dsn!, '-XqtA', '-v', 'ON_ERROR_STOP=1', '-v', 'lesson=' + lesson.id], {
    encoding: 'utf8', input: `UPDATE class_sessions SET starts_at=now()-interval '2 days', ends_at=now()-interval '2 days'+interval '1 hour' WHERE id=:'lesson';`,
  });

  await switchRole('coach');
  await page.goto('/schedule/' + lesson.id + '/roster');
  const roster = page.getByRole('region', { name: 'Class roster' });
  await expect(roster.getByRole('button', { name: 'Share feedback for Amy' })).toHaveCount(0);
  await roster.getByRole('button', { name: 'Mark present Amy' }).click();
  await roster.getByRole('button', { name: 'Confirm present' }).click();
  await expect(roster.getByText('Present', { exact: true })).toBeVisible();
  await roster.getByRole('button', { name: 'Share feedback for Amy' }).click();
  const text = 'Great streamline off the wall ' + suffix;
  await roster.getByLabel('Feedback for Amy').fill(text);
  const shared = page.waitForResponse(response => response.url().endsWith('/api/v1/students/amy/progress-notes') && response.request().method() === 'POST');
  await roster.getByRole('button', { name: 'Share feedback', exact: true }).click();
  const note = await (await shared).json();
  expectContract('ProgressNote', note);
  expect(note.lesson).toMatchObject({ name: kind.name });
  await expect(page.getByText("Feedback shared with Amy's family.")).toBeVisible();

  for (const role of ['parent', 'student']) {
    await switchRole(role);
    const list = await (await page.request.get('/api/v1/students/amy/progress-notes')).json();
    for (const item of list.items) expectContract('ProgressNote', item);
    await page.goto('/students/amy');
    const item = page.getByRole('listitem').filter({ hasText: text });
    await expect(item).toContainText('Lesson: ' + kind.name);
  }
});
