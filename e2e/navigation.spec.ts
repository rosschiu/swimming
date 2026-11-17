import { test, expect, type Page } from '@playwright/test';

async function signIn(page: Page, role: string, path: string) {
  await page.goto(path);
  await page.getByLabel('Email', { exact: true }).fill(`${role}@swim.test`);
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out', exact: true })).toBeVisible();
}

test('administrator bookmarks, refreshes and traverses real browser history', async ({ page, context }) => {
  await signIn(page, 'admin', '/schedule');
  await expect(page.getByRole('heading', { name: 'Class schedule', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Class schedule', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Students', exact: true }).click();
  const amy = page.getByRole('link', { name: /Amy/ });
  await expect(amy).toHaveAttribute('href', '/students/amy');
  await amy.focus(); await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/students\/amy$/);
  await expect(page.getByRole('heading', { name: /Amy/ }).first()).toBeVisible();
  await page.goBack(); await expect(page).toHaveURL(/\/students$/);
  await expect(page.getByRole('link', { name: /Amy/ })).toBeVisible();
  await page.goForward(); await expect(page).toHaveURL(/\/students\/amy$/);
  await expect(page.getByRole('heading', { name: /Amy/ }).first()).toBeVisible();
  const newPage = context.waitForEvent('page');
  await page.getByRole('link', { name: 'Schedule', exact: true }).click({ modifiers: ['ControlOrMeta'] });
  const tab = await newPage;
  await expect(tab).toHaveURL(/\/schedule$/);
  await expect(tab.getByRole('heading', { name: 'Class schedule', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/students\/amy$/); await tab.close();
});

test('coach restores availability and cannot mount administrator routes', async ({ page }) => {
  await signIn(page, 'coach', '/availability');
  await expect(page.getByRole('heading', { name: 'Available time slots', exact: true })).toBeVisible();
  await page.reload(); await expect(page.getByRole('heading', { name: 'Available time slots', exact: true })).toBeVisible();
  const requests: string[] = []; page.on('request', r => { if (r.url().includes('/api/v1/')) requests.push(r.url()); });
  await page.goto('/accounts'); await expect(page.getByRole('heading', { name: /unavailable/i })).toBeVisible();
  expect(requests.some(p => /\/api\/v1\/(users|invitations)/.test(p))).toBe(false);
});

test('parent profile links survive reload and scoped denial never displays another family', async ({ page, browser }) => {
  const coachContext = await browser.newContext({ baseURL: 'http://localhost:18573' });
  try {
    // Create a real notification fixture so this test does not depend on suite order.
    const coach = await coachContext.newPage(); await signIn(coach, 'coach', '/students/amy');
    const auth = await (await coach.request.get('/api/v1/auth/me')).json();
    const created = await coach.request.post('/api/v1/students/amy/progress-notes', { headers: { Origin: 'http://localhost:18573', 'X-CSRF-Token': auth.csrfToken }, data: { requestId: `navigation-${Date.now()}`, body: 'Synthetic browser navigation update' } });
    expect(created.status()).toBe(201);
    await signIn(page, 'parent', '/notifications');
    await expect(page.getByRole('heading', { name: 'Notifications', exact: true })).toBeVisible();
    const profile = page.getByRole('link', { name: 'Open current student profile', exact: true }).first();
    await expect(profile).toHaveAttribute('href', '/students/amy'); await profile.click();
    await expect(page).toHaveURL(/\/students\/amy$/); await page.reload();
    await expect(page.getByRole('heading', { name: /Amy/ }).first()).toBeVisible();
    await page.goBack(); await expect(page).toHaveURL(/\/notifications$/); await expect(page.getByRole('heading', { name: 'Notifications', exact: true })).toBeVisible();
    await page.goto('/students/ben'); await expect(page.getByRole('heading', { name: /unavailable/i })).toBeVisible();
    await expect(page.getByRole('heading', { name: /Ben/ })).toHaveCount(0);
    await page.getByRole('button', { name: '繁體中文', exact: true }).click(); await expect(page.locator('html')).toHaveAttribute('lang', 'zh-Hant-HK');
    await page.getByRole('button', { name: '登出', exact: true }).click(); await expect(page.getByRole('button', { name: '登入', exact: true })).toBeVisible();
    await page.goBack(); await expect(page.getByRole('heading', { name: /Amy/ })).toHaveCount(0);
  } finally { await coachContext.close(); }
});
