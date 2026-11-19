import { expect, test, type Page } from '@playwright/test';
import type { components } from '../apps/web/src/generated/api';
import { expectContract } from './contract';

type Settings = components['schemas']['LessonSettings'];
const path = '/api/v1/settings/lessons';

async function signIn(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(role + '@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const auth = await (await page.request.get('/api/v1/auth/me')).json();
  return { Origin: 'http://localhost:18573', 'X-CSRF-Token': auth.csrfToken };
}

async function read(page: Page): Promise<Settings> {
  const response = await page.request.get(path);
  expect(response.status()).toBe(200);
  const value = await response.json();
  expectContract('LessonSettings', value);
  return value;
}

test('administrator reviews conflicting lesson timing edits, persists values and records both changes', async ({ page, context }) => {
  const headers = await signIn(page, 'admin');
  const original = await read(page);
  let lastAuditId = 0;
  for (;;) {
    const prior = await page.request.get('/api/v1/audit?entityId=lessons&limit=100&afterId=' + lastAuditId);
    expect(prior.status()).toBe(200);
    const { items } = await prior.json();
    if (items.length) lastAuditId = items.at(-1).id;
    if (items.length < 100) break;
  }
  const cutoff = (original.parentChangeCutoffHours + 1) % 8761;
  const minutes = (original.attendanceOpenMinutes + 1) % 1441;
  const first = { ...original, parentChangeCutoffHours: cutoff, version: original.version + 1 };
  const second = { ...first, attendanceOpenMinutes: minutes, version: first.version + 1 };
  await page.getByRole('link', { name: 'School setup' }).click();
  await page.getByRole('tab', { name: 'Lessons' }).click();
  const otherTab = await context.newPage();
  await otherTab.goto('/setup?section=lessons');
  const cutoffInput = page.getByLabel('Parent change cutoff (hours)');
  const minutesInput = otherTab.getByLabel('Attendance opens after start (minutes)');
  await expect(cutoffInput).toHaveValue(String(original.parentChangeCutoffHours));
  await expect(minutesInput).toHaveValue(String(original.attendanceOpenMinutes));
  await minutesInput.fill(String(minutes));
  try {
    await cutoffInput.fill(String(cutoff));
    await page.getByRole('button', { name: 'Save lesson timing' }).click();
    await expect(page.getByText('Lesson timing saved.')).toBeVisible();
    expect(await read(page)).toEqual(first);
    const save = otherTab.getByRole('button', { name: 'Save lesson timing' });
    const conflict = otherTab.waitForResponse(r => r.url().endsWith(path) && r.request().method() === 'PATCH');
    await save.click();
    expect((await conflict).status()).toBe(409);
    await expect(otherTab.getByRole('alert')).toContainText('Lesson timing changed.');
    await expect(save).toBeDisabled();
    expect(await read(page)).toEqual(first);
    await otherTab.getByRole('button', { name: 'Reload lesson timing' }).click();
    await expect(otherTab.getByLabel('Parent change cutoff (hours)')).toHaveValue(String(cutoff));
    await expect(minutesInput).toHaveValue(String(minutes));
    await save.click();
    await expect(otherTab.getByText('Lesson timing saved.')).toBeVisible();
    await page.reload();
    await expect(cutoffInput).toHaveValue(String(cutoff));
    await expect(page.getByLabel('Attendance opens after start (minutes)')).toHaveValue(String(minutes));
    expect(await read(page)).toEqual(second);
    const audit = await page.request.get('/api/v1/audit?entityId=lessons&afterId=' + lastAuditId);
    expect(audit.status()).toBe(200);
    const { items } = await audit.json();
    expect(items).toHaveLength(2);
    expect(items[0]).toMatchObject({ action: 'lesson.settings.updated', actorId: 'admin', details: { before: original, after: first } });
    expect(items[1]).toMatchObject({ action: 'lesson.settings.updated', actorId: 'admin', details: { before: first, after: second } });
  } finally {
    const latest = await read(page);
    if (latest.version !== original.version) {
      const restored = await page.request.patch(path, { headers, data: { ...original, version: latest.version } });
      expect(restored.status()).toBe(200);
      expect(await read(page)).toEqual({ ...original, version: latest.version + 1 });
    }
    await otherTab.close();
  }
});

for (const role of ['parent', 'student', 'coach']) {
  test(role + ' cannot read or change lesson timing', async ({ page }) => {
    const headers = await signIn(page, role);
    await expect(page.getByRole('link', { name: 'School setup' })).toHaveCount(0);
    await page.goto('/setup');
    await expect(page.getByRole('heading', { name: 'Page unavailable' })).toBeVisible();
    expect((await page.request.get(path)).status()).toBe(403);
    expect((await page.request.patch(path, { headers, data: {
      parentChangeCutoffHours: 24, attendanceOpenMinutes: 15, correctionWindowDays: 3, version: 1,
    } })).status()).toBe(403);
  });
}
