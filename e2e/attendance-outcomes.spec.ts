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

test('a coach records an absence, corrects it with a reason, and family and audit agree', async ({ page }) => {
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
  const date = new Date(Date.UTC(2700, 0, 1) + (suffix % 100000) * 86400000).toISOString().slice(0, 10);
  const venue = await post('venues', { name: 'Outcome pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await post('class-types', { name: 'Outcome lesson ' + suffix, level: 'Beginner' });
  const lesson = await post('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 2,
    startsAt: date + 'T10:00:00+08:00', endsAt: date + 'T11:00:00+08:00' });
  const offer = await post('package-offers', { name: 'Outcome package ' + suffix, credits: 1, priceMinor: 10000, currency: 'HKD', active: true,
    policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: true, absenceRetainsCredit: true, illnessRetainsCredit: true } });
  await switchRole('parent');
  const purchase = await post('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: 'outcome-' + suffix });
  const attempt = await post(`purchases/${purchase.id}/payment-attempts`, { idempotencyKey: 'outcome-pay-' + suffix });
  await post(`payment-attempts/${attempt.id}/mock-outcome`, { outcome: 'succeeded' }, 200);
  const packageId = (await (await page.request.get('/api/v1/students/amy/credits')).json()).items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id).id;
  const booking = await post('students/amy/bookings', { classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion, creditPackageId: packageId, idempotencyKey: 'outcome-book-' + suffix });
  // Coaches cannot read family balances, so check the ledger directly.
  const pack = async () => {
    const [available, reserved, consumed] = execFileSync('psql', [process.env.DATABASE_URL!, '-XqtA', '-v', 'ON_ERROR_STOP=1', '-v', 'pack=' + packageId], {
      encoding: 'utf8', input: "SELECT sum(available)||' '||sum(reserved)||' '||sum(consumed) FROM credit_movements WHERE package_id=:'pack';" }).trim().split(' ').map(Number);
    return { available, reserved, consumed };
  };

  // Move the synthetic lesson to yesterday so absence is open and the correction window still is.
  const dsn = process.env.DATABASE_URL;
  expect(dsn, 'dedicated synthetic database required').toBeTruthy();
  execFileSync('psql', [dsn!, '-Xq', '-v', 'ON_ERROR_STOP=1', '-v', 'lesson=' + lesson.id], { input:
    `UPDATE class_sessions SET starts_at=((now() AT TIME ZONE 'Asia/Hong_Kong')::date - 1 + time '10:00') AT TIME ZONE 'Asia/Hong_Kong',
       ends_at=((now() AT TIME ZONE 'Asia/Hong_Kong')::date - 1 + time '11:00') AT TIME ZONE 'Asia/Hong_Kong' WHERE id=:'lesson';` });

  await switchRole('coach');
  await page.goto(`/schedule/${lesson.id}/roster`);
  const roster = page.getByRole('region', { name: 'Class roster' });
  await roster.getByRole('button', { name: /^Mark absent / }).click();
  const marked = page.waitForResponse(r => r.url().endsWith(`/roster/${booking.id}/attendance`) && r.request().method() === 'PUT');
  await roster.getByRole('button', { name: 'Confirm absent' }).click();
  const absent = await (await marked).json();
  expectContract('Attendance', absent);
  expect(absent).toMatchObject({ status: 'absent', creditOutcome: 'retained', revision: 1 });
  await expect(roster.getByText('credit returned', { exact: true })).toBeVisible();
  expect(await pack()).toMatchObject({ available: 1, reserved: 0, consumed: 0 });

  await roster.getByRole('button', { name: /^Correct attendance / }).click();
  await roster.getByRole('radio', { name: 'Present' }).check();
  await roster.getByLabel('Reason for correction').fill('Arrived after the register was taken');
  const corrected = page.waitForResponse(r => r.url().endsWith('/attendance/corrections'));
  await roster.getByRole('button', { name: 'Save correction' }).click();
  const present = await (await corrected).json();
  expectContract('Attendance', present);
  expect(present).toMatchObject({ status: 'present', creditOutcome: 'consumed', revision: 2 });
  await expect(roster.getByText('credit used · corrected')).toBeVisible();
  expect(await pack()).toMatchObject({ available: 0, reserved: 0, consumed: 1 });

  await switchRole('parent');
  await page.goto(`/students/amy/bookings/${booking.id}`);
  await expect(page.getByText('Present · credit used')).toBeVisible();
  await expect(page.getByText('Arrived after the register was taken')).toHaveCount(0);

  await switchRole('admin');
  await page.goto(`/audit?entityId=${booking.id}`);
  await expect(page.getByRole('heading', { name: 'attendance.corrected' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'attendance.absent' })).toBeVisible();
});
