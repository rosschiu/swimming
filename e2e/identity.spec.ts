import { test, expect, type Page } from '@playwright/test';
import { emailLink } from './mail';
import { expectContract } from './contract';

const origin = 'http://localhost:18573';
const password = 'Swim-new-account-2026!';
async function signIn(page: Page, email: string, value = 'Swim-test-2026!') {
  await page.goto(origin);
  await page.getByLabel('Email', { exact: true }).fill(email);
  await page.getByLabel('Password', { exact: true }).fill(value);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out', exact: true })).toBeVisible();
}
async function savePassword(page: Page, value: string) {
  await page.getByLabel('New password', { exact: true }).fill(value);
  await page.getByLabel('Confirm password', { exact: true }).fill(value);
  await page.getByRole('button', { name: 'Save password', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Your password has been saved');
}

test('invitation delivered over SMTP activates a scoped parent; reset revokes old sessions', async ({ page, browser }) => {
  const email = `guardian-${Date.now()}@swim.test`;
  await signIn(page, 'admin@swim.test');
  await page.getByRole('link', { name: 'Accounts', exact: true }).click();
  await page.getByRole('tab', { name: 'Invite', exact: true }).click();
  await page.getByLabel('Member name', { exact: true }).fill('Invited guardian');
  await page.getByLabel('Member email', { exact: true }).fill(email);
  await page.getByLabel('Role', { exact: true }).selectOption('parent');
  await page.getByLabel('Link student (optional)', { exact: true }).selectOption('amy');
  const delivered = page.waitForResponse(response => response.url().endsWith('/api/v1/invitations') && response.request().method() === 'POST');
  await page.getByRole('button', { name: 'Send invitation', exact: true }).click();
  const response = await delivered;
  expect(response.status()).toBe(201);
  const invitation = await response.json();
  expectContract('Invitation', invitation);
  expect(invitation.deliveryStatus).toBe('sent');
  const invitationMail = await emailLink(page.request, email, '/accept-invitation');
  const context = await browser.newContext({ baseURL: origin });
  const parent = await context.newPage();
  await parent.goto(invitationMail.link);
  await expect(parent).not.toHaveURL(/token=/);
  await savePassword(parent, password);
  const replay = await parent.request.post('/api/v1/auth/accept-invitation', {
    headers: { Origin: origin },
    data: { token: new URL(invitationMail.link).searchParams.get('token'), password },
  });
  expect(replay.ok()).toBe(false);

  await parent.getByRole('button', { name: 'Back to sign in' }).click();
  await signIn(parent, email, password);
  await expect(parent.getByRole('link', { name: /Amy/ })).toBeVisible();
  await expect(parent.getByRole('link', { name: /Ben/ })).toHaveCount(0);

  const resetContext = await browser.newContext({ baseURL: origin });
  const reset = await resetContext.newPage();
  await reset.goto(origin);
  await reset.getByRole('button', { name: 'Forgot password?' }).click();
  await reset.getByLabel('Email', { exact: true }).fill(email);
  await reset.getByRole('button', { name: 'Send reset instructions' }).click();
  await expect(reset.getByRole('status')).toContainText('If an eligible account exists');
  const resetMail = await emailLink(reset.request, email, '/reset-password', invitationMail.messageID);
  await reset.goto(resetMail.link);
  await savePassword(reset, 'Swim-reset-account-2026!');
  expect((await parent.request.get('/api/v1/auth/me')).status()).toBe(401);
  await reset.getByRole('button', { name: 'Back to sign in' }).click();
  await signIn(reset, email, 'Swim-reset-account-2026!');
  await expect(reset.getByRole('link', { name: /Amy/ })).toBeVisible();

  await page.getByRole('link', { name: 'Students', exact: true }).click();
  await page.getByRole('link', { name: /Amy/ }).click();
  await page.getByRole('button', { name: 'Unlink Invited guardian', exact: true }).click();
  await expect(page.getByText('Account unlinked. Its access to this student has been removed.', { exact: true })).toBeVisible();
  expect((await reset.request.get('/api/v1/students/amy')).status()).toBe(404);
  await reset.reload();
  await expect(reset.getByRole('heading', { name: 'No swimmers linked yet' })).toBeVisible();
  await context.close();
  await resetContext.close();
});
