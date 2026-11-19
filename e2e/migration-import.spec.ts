import { expect, test, type Page } from '@playwright/test';
import { expectContract } from './contract';
import { emailLink } from './mail';
import { xlsx } from './xlsx';

async function signIn(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(role + '@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
}

test('an administrator checks and imports an Excel sheet; the existing guardian sees the migrated balance', async ({ page }) => {
  test.setTimeout(120000);
  await signIn(page, 'admin');
  const template = await page.request.get('/api/v1/imports/template');
  expect(template.status()).toBe(200);
  expect(template.headers()['content-type']).toContain('spreadsheetml');

  const suffix = Date.now();
  const swimmer = 'Migrated swimmer ' + suffix;
  const sibling = 'Migrated sibling ' + suffix;
  const guardian = `migrated-${suffix}@swim.test`;
  const expiry = new Date(Date.now() + 120 * 86400000).toISOString().slice(0, 10);
  const header = ['Student name', 'Level', 'Guardian name', 'Guardian email', 'Second guardian name', 'Second guardian email', 'Credits', 'Credits expire on', 'Old system ID'];

  await page.getByRole('link', { name: 'Accounts', exact: true }).click();
  await page.getByRole('tab', { name: 'Import' }).click();
  const input = page.getByLabel('Excel file (.xlsx)');

  // A sheet with a problem is explained row by row and cannot be imported.
  await input.setInputFiles({ name: 'school.xlsx', mimeType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    buffer: xlsx([header, [swimmer, 'Beginner', 'Migrated Guardian', guardian, '', 'parent@swim.test', '6', '']]) });
  await expect(page.getByText(`Row 2 · ${swimmer}: Remaining credits need an expiry date (YYYY-MM-DD).`)).toBeVisible();
  await expect(page.getByText('Ignored columns: Old system ID')).toBeVisible();
  await expect(page.getByRole('button', { name: /^Import \d+ swimmers$/ })).toHaveCount(0);

  const checked = page.waitForResponse(r => r.url().endsWith('/api/v1/imports/preview'));
  await input.setInputFiles({ name: 'school.xlsx', mimeType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    buffer: xlsx([header, [swimmer, 'Beginner', 'Migrated Guardian', guardian, '', 'parent@swim.test', '6', expiry, 'A1'], [sibling, 'Improver', 'Migrated Guardian', guardian, '', '', '', '', 'A2']]) });
  expectContract('ImportPreview', await (await checked).json());
  await expect(page.getByText('2 swimmers · 1 new guardians · 1 existing guardians · 6 credits')).toBeVisible();
  const imported = page.waitForResponse(r => r.url().includes('/api/v1/imports?fileSha256='));
  await page.getByRole('button', { name: 'Import 2 swimmers' }).click();
  const result = await (await imported).json();
  expectContract('ImportResult', result);
  await expect(page.getByText('Imported 2 swimmers, 1 new and 1 existing guardians, and 6 credits in 1 migrated balances.')).toBeVisible();

  // The new guardian waits unsent until the administrator sends the invitation.
  await page.getByRole('tab', { name: 'Invitations' }).click();
  const invitation = page.getByRole('article').filter({ hasText: guardian });
  await expect(invitation).toContainText('Not sent yet');
  await invitation.getByRole('button', { name: `Send invitation ${guardian}` }).click();
  await expect(invitation).toContainText('Email sent');
  expect((await emailLink(page.request, guardian, '/accept-invitation')).link).toContain('/accept-invitation');

  // The existing guardian sees the imported swimmer and the migrated balance.
  await page.getByRole('button', { name: 'Sign out' }).click();
  await signIn(page, 'parent');
  const students = await (await page.request.get('/api/v1/students')).json();
  const mine = students.items.find((item: { name: string }) => item.name === swimmer);
  expect(mine, 'imported swimmer linked to the existing guardian').toBeTruthy();
  const history = await (await page.request.get(`/api/v1/students/${mine.id}/credit-history`)).json();
  expectContract('CreditHistory', history);
  expect(history.items).toMatchObject([{ migrated: true, complimentary: false, available: 6, expiresOn: expiry }]);
  await page.goto(`/students/${mine.id}`);
  await expect(page.getByRole('heading', { name: 'Migrated balance' })).toBeVisible();
});
