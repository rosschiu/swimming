import { expect, test, type Page } from '@playwright/test';
import { expectContract } from './contract';

// A fake microphone lets the journey record real audio with the browser's MediaRecorder.
test.use({ launchOptions: { args: ['--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'] }, permissions: ['microphone'] });

const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAIAAABLbSncAAAAEUlEQVR4nGMQz0rFihiGlgQAZJI5gVEuNGoAAAAASUVORK5CYII=', 'base64');
const video = Buffer.concat([Buffer.from('\x00\x00\x00\x18ftypmp42', 'binary'), Buffer.alloc(4000, 1)]);

async function signIn(page: Page, role: string) {
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(role + '@swim.test');
  await page.getByLabel('Password', { exact: true }).fill('Swim-test-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
}

test('a coach shares a photo, a video and a voice recording; the family can open each and others cannot', async ({ page }) => {
  test.setTimeout(90000);
  await signIn(page, 'coach');
  await page.goto('/students/amy');
  const text = 'Watch the push-off ' + Date.now();
  await page.getByLabel('Progress note', { exact: true }).fill(text);
  await page.getByLabel('Add photos or videos').setInputFiles([
    { name: 'kick.png', mimeType: 'image/png', buffer: png },
    { name: 'lap.mp4', mimeType: 'video/mp4', buffer: video },
  ]);
  await page.getByRole('button', { name: 'Record voice' }).click();
  await expect(page.getByText('Recording…')).toBeVisible();
  await page.waitForTimeout(1200);
  await page.getByRole('button', { name: 'Stop recording' }).click();
  const attachments = page.getByRole('list', { name: 'Attachments' });
  await expect(attachments.getByRole('listitem')).toHaveCount(3);
  await expect(attachments).toContainText('Voice recording');

  const uploads: unknown[] = [];
  page.on('response', async response => { if (response.url().endsWith('/note-media') && response.status() === 201) uploads.push(await response.json()); });
  const shared = page.waitForResponse(response => response.url().endsWith('/students/amy/progress-notes') && response.request().method() === 'POST');
  await page.getByRole('button', { name: 'Share progress note', exact: true }).click();
  const note = await (await shared).json();
  expectContract('ProgressNote', note);
  for (const upload of uploads) expectContract('NoteMedia', upload);
  expect(note.media.map((item: { kind: string }) => item.kind)).toEqual(['image', 'video', 'audio']);
  await expect(page.getByText('Progress note shared.')).toBeVisible();

  for (const role of ['parent', 'student']) {
    await page.getByRole('button', { name: 'Sign out' }).click();
    await signIn(page, role);
    await page.goto('/students/amy');
    const item = page.getByRole('listitem').filter({ hasText: text });
    const photo = item.getByRole('img', { name: 'Photo from the lesson' });
    await expect(photo).toBeVisible();
    expect(await photo.evaluate((element: HTMLImageElement) => element.naturalWidth)).toBe(8);
    await expect(item.locator('audio')).toHaveAttribute('src', note.media[2].url);
    await expect(item.locator('video')).toHaveAttribute('src', note.media[1].url);
    const recording = await page.request.get(note.media[2].url);
    expect(recording.status()).toBe(200);
    expect(recording.headers()['content-type']).toMatch(/^audio\//);
    const clip = await page.request.get(note.media[1].url, { headers: { Range: 'bytes=0-15' } });
    expect(clip.status()).toBe(206);
    expect(clip.headers()['x-content-type-options']).toBe('nosniff');
  }

  await page.getByRole('button', { name: 'Sign out' }).click();
  await signIn(page, 'other-parent');
  for (const item of note.media) expect((await page.request.get(item.url)).status()).toBe(404);
});
