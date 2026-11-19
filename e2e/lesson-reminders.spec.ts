import { expect, test, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { expectContract } from './contract';

const mailURL = process.env.MAILPIT_URL || 'http://127.0.0.1:18025';

async function signIn(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(role + '@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const session = await (await page.request.get('/api/v1/auth/me')).json();
  return { Origin: 'http://localhost:18573', 'X-CSRF-Token': session.csrfToken };
}

test('a lesson entering the reminder window reminds the family in-app and by email, once', async ({ page }) => {
  test.setTimeout(180000);
  const dsn = process.env.DATABASE_URL;
  expect(dsn, 'dedicated synthetic database required').toBeTruthy();
  const sql = (input: string) => execFileSync('psql', [dsn!, '-XAtq', '-v', 'ON_ERROR_STOP=1'], { input }).toString().trim();
  // A dedicated synthetic coach keeps this near-term lesson clear of other journeys' schedules.
  sql(`INSERT INTO users(id,email,name,role,password_hash) SELECT 'reminder-coach','reminder-coach@swim.test','Reminder Coach','coach',password_hash FROM users WHERE id='coach' ON CONFLICT (id) DO NOTHING;`);
  let headers = await signIn(page, 'admin');
  async function post(path: string, data: object, status = 201) {
    const response = await page.request.post('/api/v1/' + path, { headers, data });
    expect(response.status(), await response.text()).toBe(status);
    return response.json();
  }

  await page.goto('/setup');
  await expect(page.getByLabel('Lesson reminder (hours before)')).toHaveValue('24');
  await expect(page.getByLabel('Also email lesson reminders')).toBeChecked();
  const settings = await (await page.request.get('/api/v1/settings/lessons')).json();
  expectContract('LessonSettings', settings);

  const suffix = Date.now();
  const start = new Date(Math.ceil((Date.now() + 3 * 3600000) / 3600000) * 3600000 + (suffix % 7) * 60000);
  const end = new Date(start.getTime() + 3600000);
  const venue = await post('venues', { name: 'Reminder pool ' + suffix, address: 'Synthetic pool', region: 'hk-island' });
  const kind = await post('class-types', { name: 'Reminder lesson ' + suffix, level: 'Beginner' });
  const lesson = await post('classes', { venueId: venue.id, classTypeId: kind.id, coachId: 'reminder-coach', capacity: 3,
    startsAt: start.toISOString(), endsAt: end.toISOString() });
  const offer = await post('package-offers', { name: 'Reminder credits ' + suffix, credits: 1, priceMinor: 10000, currency: 'HKD', active: true,
    policyOverride: { expiryEnabled: false, carryForwardEnabled: false, earlyCancellationRetainsCredit: true, lateCancellationRetainsCredit: true, absenceRetainsCredit: true, illnessRetainsCredit: true } });

  await page.getByRole('button', { name: 'Sign out' }).click();
  headers = await signIn(page, 'parent');
  const service = await page.request.put('/api/v1/notification-preferences', { headers, data: { serviceEmailEnabled: true } });
  expect(service.status(), await service.text()).toBe(200);
  const purchase = await post('students/amy/purchases', { offerId: offer.id, offerVersion: offer.version, idempotencyKey: 'reminder-' + suffix });
  const attempt = await post(`purchases/${purchase.id}/payment-attempts`, { idempotencyKey: 'reminder-pay-' + suffix });
  await post(`payment-attempts/${attempt.id}/mock-outcome`, { outcome: 'succeeded' }, 200);
  const pack = (await (await page.request.get('/api/v1/students/amy/credits')).json()).items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id);
  const booking = await post('students/amy/bookings', { classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion, creditPackageId: pack.id, idempotencyKey: 'reminder-book-' + suffix });

  // The server's reminder worker scans every minute.
  type Item = { id: string; kind: string; sourceId: string };
  let reminder: Item | undefined;
  await expect.poll(async () => {
    const list = await (await page.request.get('/api/v1/notifications?limit=100')).json();
    reminder = list.items.find((item: Item) => item.kind === 'booking.reminder' && item.sourceId === booking.id);
    return !!reminder;
  }, { timeout: 120000, intervals: [2000] }).toBe(true);
  expectContract('Notification', reminder);

  await page.goto('/notifications');
  const card = page.getByRole('article').filter({ hasText: 'Reminder: a booked lesson is coming up. Open it for the time and place.' }).first();
  await expect(card).toBeVisible();

  await expect.poll(() => sql(`SELECT d.status FROM notification_deliveries d WHERE d.notification_id='${reminder!.id}'`), { timeout: 30000 }).toBe('sent');
  const inbox = await (await page.request.get(`${mailURL}/api/v1/search?query=${encodeURIComponent('subject:"lesson reminder"')}`)).json();
  const message = inbox.messages.find((m: { MessageID: string }) => m.MessageID === `teaching-${reminder!.id}@swimcoach.invalid`);
  expect(message, 'reminder email captured by Mailpit').toBeTruthy();
  const body = await (await page.request.get(`${mailURL}/api/v1/message/${message.ID}`)).json();
  expect(body.Text).toContain('/notifications');
  expect(body.Text).not.toContain(kind.name);
  expect(body.Text).not.toContain(venue.name);
  expect(sql(`SELECT count(*) FROM booking_reminders WHERE booking_id='${booking.id}'`)).toBe('1');
});
