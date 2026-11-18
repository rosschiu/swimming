import { expect, test, type Page } from '@playwright/test';
import { expectContract } from './contract';

async function login(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(`${role}@swim.test`);
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
}
const hkDatePlus = (days: number) => {
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Hong_Kong' }).format(new Date());
  const date = new Date(`${today}T00:00:00Z`);
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
};

test('a guardian pays for an offer with default validity without waiting for an administrator', async ({ page }) => {
  const name = `Thirty-day package ${Date.now()}`;
  await login(page, 'admin');
  await page.getByRole('link', { name: 'Packages and payments', exact: true }).click();
  await page.getByRole('button', { name: 'Add package offer' }).click();
  await page.getByLabel('Package name').fill(name);
  await page.getByLabel('Credits', { exact: true }).fill('3');
  await page.getByLabel('Price (HKD)').fill('300.00');
  await page.getByLabel('Default validity (days from payment)').fill('30');
  const form = page.locator('form').filter({ has: page.getByLabel('Package name') });
  await form.getByLabel('Override school credit policy').check();
  await form.getByLabel('Expiry enabled').check();
  const saved = page.waitForResponse(r => r.url().endsWith('/api/v1/package-offers') && r.request().method() === 'POST');
  await page.getByRole('button', { name: 'Save package offer' }).click();
  const offer = await (await saved).json();
  expectContract('PackageOffer', offer);
  expect(offer.validityDays).toBe(30);
  await expect(page.getByRole('article').filter({ hasText: name })).toContainText('Valid 30 days from payment');
  await page.getByRole('button', { name: 'Sign out' }).click();

  await login(page, 'parent');
  await page.getByRole('link', { name: 'Packages and payments', exact: true }).click();
  await page.getByLabel('Choose child').selectOption('amy');
  await page.getByLabel('Package offer', { exact: true }).selectOption(offer.id);
  await expect(page.getByText('Credits are valid for 30 days from the payment date (Hong Kong).')).toBeVisible();
  const created = page.waitForResponse(r => r.url().endsWith('/students/amy/purchases') && r.request().method() === 'POST');
  await page.getByRole('button', { name: 'Create purchase' }).click();
  const purchase = await (await created).json();
  expectContract('Purchase', purchase);
  expect(purchase.status).toBe('pending_payment');
  const checkout = page.getByRole('article').filter({ has: page.getByRole('heading', { name, exact: true }) });
  await expect(checkout).toContainText('30 days from the payment date');
  await checkout.getByRole('button', { name: 'Start mock payment' }).click();
  await checkout.getByRole('button', { name: 'Mark succeeded' }).click();
  await expect(checkout.getByText('Paid (simulated)', { exact: true })).toBeVisible();
  const expected = [hkDatePlus(30), hkDatePlus(31)];
  const paid = await (await page.request.get(`/api/v1/purchases/${purchase.id}`)).json();
  expectContract('Purchase', paid);
  expect(expected).toContain(paid.expiresOn);
  await checkout.getByRole('button', { name: 'View simulated receipt' }).click();
  await expect(checkout.getByRole('region', { name: 'Simulated receipt' })).toContainText(paid.expiresOn);
  const credits = await (await page.request.get('/api/v1/students/amy/credits')).json();
  const pack = credits.items.find((item: { purchaseId: string }) => item.purchaseId === purchase.id);
  expect(pack.expiresOn).toBe(paid.expiresOn);
  expect(pack.available).toBe(3);
});
