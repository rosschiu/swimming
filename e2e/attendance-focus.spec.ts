import { expect, test, type Locator, type Page } from '@playwright/test';

async function openRoster(page: Page) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill('coach@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const startsAt = '2025-01-20T08:00:00Z';
  const entries = ['Amy', 'Ben', 'Chloe'].map(name => ({
    bookingId: name, studentId: name, studentName: name, level: 'Beginner', status: 'reserved',
    attendance: name === 'Ben' ? { status: 'present', markedAt: startsAt } : null,
  }));
  const path = '**/api/v1/classes/focus-roster';
  await page.route(path, route => route.fulfill({ json: { id: 'focus-roster', startsAt, endsAt: '2025-01-20T09:00:00Z',
    classTypeName: 'Beginners', coachName: 'Alex', venueName: 'Harbour' } }));
  await page.route(path + '/roster', route => route.fulfill({ json: { classId: 'focus-roster', attendanceOpensAt: startsAt, items: entries } }));
  await page.goto('/schedule/focus-roster/roster');
  const roster = page.getByRole('region', { name: 'Class roster' });
  const button = (name: string) => roster.getByRole('button', { name, exact: true });
  return { roster, button, attendancePath: path + '/roster/*/attendance' };
}

async function enter(page: Page, control: Locator) {
  await control.focus();
  await page.keyboard.press('Enter');
}

const attendance = { status: 'present', markedAt: '2025-01-20T08:30:00Z' };

test('attendance cancel and retry preserve a keyboard path through the remaining swimmers', async ({ page }) => {
  const { roster, button, attendancePath } = await openRoster(page);
  const writes: string[] = [];
  await page.route(attendancePath, route => {
    writes.push(route.request().url().split('/').at(-2)!);
    return writes.length === 1
      ? route.fulfill({ status: 409, json: { error: { code: 'credit_unavailable', message: 'Reserved credit unavailable' } } })
      : route.fulfill({ json: attendance });
  });
  const amy = button('Mark present Amy');
  const confirm = button('Confirm present');
  await enter(page, amy);
  await enter(page, button('Cancel'));
  await expect(amy).toBeFocused();
  expect(writes).toEqual([]);
  await enter(page, amy);
  await enter(page, confirm);
  await expect(roster.getByRole('alert')).toContainText('reserved lesson credit is unavailable');
  await expect(confirm).toBeFocused();
  await page.keyboard.press('Enter');
  const badge = (name: string) => roster.getByRole('listitem').filter({ has: page.getByText(name, { exact: true }) }).getByText('Present', { exact: true });
  await expect(badge('Amy')).toBeFocused();
  expect(await badge('Amy').evaluate(el => getComputedStyle(el).outlineStyle)).not.toBe('none');
  // Swimmers marked present (Amy, and Ben before her) offer lesson feedback on the way to the next swimmer.
  await page.keyboard.press('Tab');
  await expect(button('Share feedback for Amy')).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(button('Share feedback for Ben')).toBeFocused();
  await page.keyboard.press('Tab');
  const chloe = button('Mark present Chloe');
  await expect(chloe).toBeFocused();
  await page.keyboard.press('Enter');
  await enter(page, confirm);
  await expect(badge('Chloe')).toBeFocused();
  await expect(roster.getByText('Present', { exact: true })).toHaveCount(3);
  expect(writes).toEqual(['Amy', 'Amy', 'Chloe']);
});

test('an attendance response preserves focus when the coach moves outside the roster', async ({ page }) => {
  const { roster, button, attendancePath } = await openRoster(page);
  let finish!: () => void;
  const gate = new Promise<void>(resolve => { finish = resolve; });
  let writes = 0;
  await page.route(attendancePath, async route => {
    writes++;
    await gate;
    await route.fulfill({ json: attendance });
  });
  await enter(page, button('Mark present Amy'));
  const sent = page.waitForRequest(req => req.url().endsWith('/Amy/attendance') && req.method() === 'PUT');
  await enter(page, button('Confirm present'));
  await sent;
  await page.keyboard.press('Enter');
  await page.keyboard.press('Enter');
  const outside = page.getByRole('link', { name: 'Schedule', exact: true });
  await outside.focus();
  finish();
  await expect(roster.getByText('Attendance recorded for Amy.')).toBeVisible();
  await expect(outside).toBeFocused();
  expect(writes).toBe(1);
});

for (const [status, code, message] of [
  [409, 'attendance_not_open', 'Attendance has yet to open.'],
  [403, 'forbidden', '目前權限無法存取此課堂名單。'],
] as const) {
  test('attendance keyboard focus reaches the terminal alert for ' + code, async ({ page }) => {
    const { button, attendancePath } = await openRoster(page);
    await page.route(attendancePath, route => route.fulfill({ status, json: { error: { code, message } } }));
    await enter(page, button('Mark present Amy'));
    if (code === 'forbidden') {
      await page.getByRole('button', { name: '繁體中文' }).click();
    }
    const confirm = page.getByRole('button', { name: code === 'forbidden' ? '確認出席' : 'Confirm present' });
    await enter(page, confirm);
    const alert = page.getByRole('alert');
    await expect(alert).toContainText(message);
    await expect(alert).toBeFocused();
    await expect(confirm).toHaveCount(0);
  });
}
