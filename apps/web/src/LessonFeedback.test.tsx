import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import LessonFeedback from './LessonFeedback';

const props = { studentId: 'amy', studentName: 'Amy', bookingId: 'b1', csrfToken: 'csrf', expired: () => false, t: (en: string) => en };
afterEach(() => vi.unstubAllGlobals());
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });

it('shares feedback for the attended lesson', async () => {
  const fetch = vi.fn(async () => json({ id: 'n1' }, 201));
  vi.stubGlobal('fetch', fetch);
  const onShared = vi.fn();
  render(<LessonFeedback {...props} onShared={onShared}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Share feedback for Amy' }));
  await user.type(screen.getByLabelText('Feedback for Amy'), ' Great streamline ');
  await user.click(screen.getByRole('button', { name: 'Share feedback' }));
  expect(onShared).toHaveBeenCalledOnce();
  expect(fetch).toHaveBeenCalledWith('/api/v1/students/amy/progress-notes', expect.objectContaining({ method: 'POST' }));
  expect(JSON.parse((fetch.mock.calls[0] as unknown as [string, RequestInit])[1].body as string)).toMatchObject({ body: 'Great streamline', bookingId: 'b1' });
  expect(screen.getByRole('button', { name: 'Share feedback for Amy' })).toBeVisible();
});

it('retries an uncertain share with the same request and explains ineligible lessons', async () => {
  const fetch = vi.fn()
    .mockResolvedValueOnce(json({ error: { code: 'internal', message: 'x' } }, 500))
    .mockResolvedValueOnce(json({ error: { code: 'lesson_not_eligible', message: 'x' } }, 409));
  vi.stubGlobal('fetch', fetch);
  render(<LessonFeedback {...props} onShared={() => {}}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Share feedback for Amy' }));
  await user.type(screen.getByLabelText('Feedback for Amy'), 'Kick');
  await user.click(screen.getByRole('button', { name: 'Share feedback' }));
  expect(await screen.findByText(/Sharing could not be confirmed/)).toBeVisible();
  await user.click(screen.getByRole('button', { name: 'Retry same submission' }));
  expect(await screen.findByText('Feedback can be shared only for a swimmer marked present.')).toBeVisible();
  const ids = fetch.mock.calls.map(call => JSON.parse(call[1].body).requestId);
  expect(ids[0]).toBe(ids[1]);
});
