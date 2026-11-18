import { test, expect, type Page } from '@playwright/test';
import { expectContract } from './contract';

async function login(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(`${role}@swim.test`);
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
}

test('invalid login stays signed out and reports a useful error', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill('parent@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('wrong-password');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Unable to sign in');
  expect((await page.request.get('/api/v1/auth/me')).status()).toBe(401);
});

for (const role of ['parent', 'coach', 'student']) {
  test(`${role} sees only assigned records and cannot write`, async ({ page }) => {
    await login(page, role);
    await page.goto('/students');
    await expect(page.getByRole('link', { name: /Amy/ })).toBeVisible();
    await expect(page.getByRole('link', { name: /Ben/ })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Add student' })).toHaveCount(0);
    expect((await page.request.get('/api/v1/students/ben')).status()).toBe(404);
    const session = await (await page.request.get('/api/v1/auth/me')).json();
    expectContract('Auth', session);
    const denied = await page.request.post('/api/v1/students', { headers: { Origin: 'http://localhost:18573', 'X-CSRF-Token': session.csrfToken }, data: { name: 'Forbidden', level: 'Beginner' } });
    expect(denied.status()).toBe(403);
    await page.reload();
    await expect(page.getByRole('link', { name: /Amy/ })).toBeVisible();
    await page.getByRole('button', { name: 'Sign out' }).click();
    await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
    expect((await page.request.get('/api/v1/students')).status()).toBe(401);
  });
}

test('administrator creates and edits a persistent student with stale-write protection', async ({ page }) => {
  await login(page, 'admin');
  await page.getByRole('link', { name: 'Students', exact: true }).click();
  const name = `E2E swimmer ${Date.now()}`;
  await page.getByRole('button', { name: 'Add student' }).click();
  await page.getByLabel('Student name', { exact: true }).fill(name);
  await page.getByLabel('Swimming level', { exact: true }).fill('Beginner');
  await page.getByRole('button', { name: 'Save student', exact: true }).click();
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  const list = await (await page.request.get('/api/v1/students')).json();
  for (const student of list.items) expectContract('Student', student);
  const saved = list.items.find((student: {name: string}) => student.name === name);
  await page.getByRole('button', { name: 'Edit student', exact: true }).click();
  await page.getByLabel('Swimming level', { exact: true }).fill('Intermediate');
  await page.getByRole('button', { name: 'Save student', exact: true }).click();
  await expect(page.getByText('Intermediate', { exact: true })).toBeVisible();
  const session = await (await page.request.get('/api/v1/auth/me')).json();
    expectContract('Auth', session);
  const stale = await page.request.patch(`/api/v1/students/${saved.id}`, { headers: { Origin: 'http://localhost:18573', 'X-CSRF-Token': session.csrfToken }, data: { name, level: 'Stale', version: saved.version } });
  expect(stale.status()).toBe(409);
  await page.reload();
  await expect(page.getByText('Intermediate', { exact: true })).toBeVisible();
});

test('second guardian sees their own child and mobile language switching works', async ({ page }) => {
  await login(page, 'other-parent');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/students');
  await expect(page.getByRole('link', { name: /Ben/ })).toBeVisible();
  await expect(page.getByRole('link', { name: /Amy/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'More' }).click();
  await page.getByRole('dialog', { name: 'More' }).getByRole('button', { name: '繁體中文' }).click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-Hant-HK');
  await expect(page.getByRole('dialog', { name: '更多' }).getByRole('button', { name: '登出' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
