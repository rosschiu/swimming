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

test('an administrator cancels a lesson for the school; families get their credit back and everyone is told', async ({ page }) => {
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
  const date = new Date(Date.UTC(2500, 0, 1) + (suffix % 100000) * 86400000).toISOString().slice(0, 10);
  const venue = await post('venues', { name: 'School cancel pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await post('class-types', { name: 'School cancel lesson ' + suffix, level: 'Beginner' });
  const lesson = await post('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'coach', capacity: 3,
    startsAt: date + 'T10:00:00+08:00', endsAt: date + 'T11:00:00+08:00' });
  const offer = await post('package-offers', { name: 'School cancel credits ' + suffix, credits: 1, priceMinor: 10000, currency: 'HKD', active: true,
    policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: false, lateCancellationRetainsCredit: false, absenceRetainsCredit: false, illnessRetainsCredit: false } });

  await switchRole('parent');
  const purchase = await post('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: 'school-' + suffix });
  const attempt = await post(`purchases/${purchase.id}/payment-attempts`, { idempotencyKey: 'school-pay-' + suffix });
  await post(`payment-attempts/${attempt.id}/mock-outcome`, { outcome: 'succeeded' }, 200);
  const pack = async () => (await (await page.request.get('/api/v1/students/amy/credits')).json()).items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id);
  const booking = await post('students/amy/bookings', { classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion, creditPackageId: (await pack()).id, idempotencyKey: 'school-book-' + suffix });

  await switchRole('admin');
  await page.getByRole('link', { name: 'Schedule', exact: true }).click();
  await page.getByLabel('From date', { exact: true }).fill(date);
  await page.getByLabel('To date', { exact: true }).fill(date);
  await page.getByRole('button', { name: 'Apply filters' }).click();
  await page.getByRole('button', { name: new RegExp('Review cancellation impact ' + kind.name) }).click();
  const impact = page.getByRole('region', { name: 'Cancellation impact' });
  await impact.getByRole('button', { name: 'Cancel this lesson for everyone' }).click();
  await impact.getByLabel('Reason shown in the audit history').fill('Pool maintenance');
  const cancelled = page.waitForResponse(r => r.url().endsWith(`/classes/${lesson.id}/cancellation`) && r.request().method() === 'POST');
  await impact.getByRole('button', { name: 'Confirm school cancellation' }).click();
  const result = await (await cancelled).json();
  expectContract('SchoolCancellationResult', result);
  expect(result).toMatchObject({ cancelledBookings: 1, restoredCredits: 0, reason: 'Pool maintenance' });
  await expect(impact.getByText(/1 bookings cancelled and their credits returned/)).toBeVisible();
  const card = page.getByRole('article').filter({ hasText: kind.name });
  await expect(card.getByText('Cancelled by the school')).toBeVisible();
  await expect(card.getByRole('button', { name: /^Edit class/ })).toHaveCount(0);

  await switchRole('parent');
  // School cancellation returns the credit even though this package's policies would otherwise use it.
  expect(await pack()).toMatchObject({ available: 1, reserved: 0, consumed: 0 });
  const detail = await (await page.request.get(`/api/v1/students/amy/bookings/${booking.id}`)).json();
  expect(detail).toMatchObject({ status: 'cancelled', cancellation: { bySchool: true, creditOutcome: 'retained' } });
  await page.goto('/notifications');
  await expect(page.getByRole('article').filter({ hasText: 'The school cancelled a lesson. The credit has been returned.' }).first()).toBeVisible();

  await switchRole('coach');
  await page.goto('/notifications');
  await expect(page.getByRole('article').filter({ hasText: 'The school cancelled a lesson.' }).first().getByRole('link', { name: 'Open schedule' })).toBeVisible();
});
