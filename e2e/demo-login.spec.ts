import { expect, test } from '@playwright/test';

test('demo role buttons fill editable credentials and use ordinary sign-in', async ({ page }) => {
  await page.goto('/');
  const demo = page.getByRole('group', { name: 'Demo accounts' });
  await expect(demo.getByRole('button')).toHaveCount(4);
  let loginRequests = 0;
  page.on('request', request => {
    if (request.method() === 'POST' && request.url().endsWith('/api/v1/auth/login')) loginRequests++;
  });
  for (const [label, email] of [
    ['Administrator', 'admin@swim.test'],
    ['Coach', 'coach@swim.test'],
    ['Guardian', 'parent@swim.test'],
    ['Student', 'student@swim.test'],
  ]) {
    await demo.getByRole('button', { name: label }).click();
    await expect(page.getByLabel('Email')).toHaveValue(email);
  }
  await expect(page.getByLabel('Email')).toBeFocused();
  await page.getByLabel('Password').fill('incorrect-demo-password');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('alert')).toContainText('Unable to sign in');
  expect(loginRequests).toBe(1);
  await demo.getByRole('button', { name: 'Guardian' }).click();
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { name: /^Hi, / })).toBeVisible();
  expect(loginRequests).toBe(2);
  await page.getByRole('button', { name: 'Sign out' }).click();
  await page.getByRole('button', { name: '繁體中文' }).click();
  const localized = page.getByRole('group', { name: '示範帳戶' });
  for (const label of ['管理員', '教練', '監護人', '學員']) {
    await expect(localized.getByRole('button', { name: label })).toBeVisible();
  }
});
