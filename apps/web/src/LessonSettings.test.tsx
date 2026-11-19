import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { StrictMode } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import LessonSettings from './LessonSettings';

const defaults = { parentChangeCutoffHours: 24, attendanceOpenMinutes: 15, correctionWindowDays: 3, cancellationEmail: true, reminderHours: 24, reminderEmail: true, version: 1 };
const props = { csrfToken: 'csrf-value', expired: () => false, t: (en: string) => en };
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });

afterEach(() => vi.unstubAllGlobals());

it('loads and saves all three integer settings, sends CSRF and updates the version', async () => {
  const fetch = vi.fn(async (_path, options?: RequestInit) => {
    if (!options?.body) return response(defaults);
    const changed = JSON.parse(String(options.body));
    return response({ ...changed, version: changed.version + 1 });
  });
  vi.stubGlobal('fetch', fetch);
  render(<StrictMode><LessonSettings {...props}/></StrictMode>);
  const user = userEvent.setup();
  const cutoff = await screen.findByLabelText('Parent change cutoff (hours)');
  const attendance = screen.getByLabelText('Attendance opens after start (minutes)');
  const correction = screen.getByLabelText('Attendance correction window (days)');
  expect(cutoff).toHaveValue(24);
  expect(attendance).toHaveValue(15);
  expect(correction).toHaveValue(3);
  const reminder = screen.getByLabelText('Lesson reminder (hours before)');
  expect(reminder).toHaveValue(24);
  expect(screen.getByLabelText('Also email lesson reminders')).toBeChecked();
  const save = screen.getByRole('button', { name: 'Save lesson timing' });
  fireEvent.change(cutoff, { target: { value: '8761' } });
  expect(cutoff).toBeInvalid();
  await user.click(save);
  expect(fetch.mock.calls.some(([, options]) => options?.method === 'PATCH')).toBe(false);
  fireEvent.change(cutoff, { target: { value: '0' } });
  fireEvent.change(attendance, { target: { value: '1440' } });
  fireEvent.change(correction, { target: { value: '365' } });
  fireEvent.change(reminder, { target: { value: '169' } });
  expect(reminder).toBeInvalid();
  fireEvent.change(reminder, { target: { value: '0' } });
  await user.click(screen.getByLabelText('Also email lesson reminders'));
  await user.click(save);
  expect(await screen.findByText('Lesson timing saved.')).toHaveAttribute('role', 'status');
  expect(fetch).toHaveBeenCalledWith('/api/v1/settings/lessons', expect.objectContaining({
    method: 'PATCH',
    headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf-value' }),
    body: JSON.stringify({ parentChangeCutoffHours: 0, attendanceOpenMinutes: 1440, correctionWindowDays: 365, reminderHours: 0, cancellationEmail: true, reminderEmail: false, version: 1 }),
  }));
  fireEvent.change(cutoff, { target: { value: '29' } });
  await user.click(save);
  await screen.findByText('Lesson timing saved.');
  expect(JSON.parse(String(fetch.mock.calls.at(-1)?.[1]?.body)).version).toBe(2);
});

it('blocks stale saves and rebases only edited fields onto the latest settings', async () => {
  const recent = { ...defaults, parentChangeCutoffHours: 36, attendanceOpenMinutes: 30, version: 2 };
  const fetch = vi.fn()
    .mockResolvedValueOnce(response(defaults))
    .mockResolvedValueOnce(response({}, 409))
    .mockResolvedValueOnce(response(recent))
    .mockResolvedValueOnce(response({ ...recent, parentChangeCutoffHours: 48, version: 3 }));
  vi.stubGlobal('fetch', fetch);
  render(<LessonSettings {...props}/>);
  const user = userEvent.setup();
  const cutoff = await screen.findByLabelText('Parent change cutoff (hours)');
  fireEvent.change(cutoff, { target: { value: '48' } });
  const save = screen.getByRole('button', { name: 'Save lesson timing' });
  await user.click(save);
  expect(await screen.findByRole('alert')).toHaveTextContent('Lesson timing changed');
  expect(cutoff).toHaveValue(48);
  expect(save).toBeDisabled();
  await user.click(screen.getByRole('button', { name: 'Reload lesson timing' }));
  expect(cutoff).toHaveValue(48);
  expect(cutoff).toHaveAccessibleDescription(/Saved value: 36/);
  expect(screen.getByLabelText('Attendance opens after start (minutes)')).toHaveValue(30);
  await user.click(save);
  await screen.findByText('Lesson timing saved.');
  expect(JSON.parse(fetch.mock.calls.at(-1)![1].body)).toEqual({ ...recent, parentChangeCutoffHours: 48 });
});

it('can retry a loading failure and preserves a draft after a failed save', async () => {
  const fetch = vi.fn()
    .mockResolvedValueOnce(response({}, 500))
    .mockResolvedValueOnce(response(defaults))
    .mockResolvedValueOnce(response({}, 500));
  vi.stubGlobal('fetch', fetch);
  render(<LessonSettings {...props}/>);
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not load lesson timing');
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Reload lesson timing' }));
  const correction = await screen.findByLabelText('Attendance correction window (days)');
  fireEvent.change(correction, { target: { value: '6' } });
  await user.click(screen.getByRole('button', { name: 'Save lesson timing' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Your entries are preserved');
  expect(correction).toHaveValue(6);
});

it('clears controls after a forbidden save and translates the denial', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(response(defaults)).mockResolvedValueOnce(response({}, 403)));
  render(<LessonSettings {...props} t={(_en, zh) => zh}/>);
  expect(await screen.findByLabelText('家長更改申請期限（小時）')).toHaveValue(24);
  await userEvent.setup().click(screen.getByRole('button', { name: '儲存課堂時間規則' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('目前權限無法存取課堂時間規則。');
  expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument();
});
