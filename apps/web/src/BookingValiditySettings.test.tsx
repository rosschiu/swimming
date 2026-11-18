import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import BookingValiditySettings from './BookingValiditySettings';

const path = '/api/v1/settings/booking-validity';
const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });

afterEach(() => vi.unstubAllGlobals());

it('retries loading, preserves a stale draft across reload, and saves with the refreshed version', async () => {
  let reads = 0;
  let writes = 0;
  const api = vi.fn(async (_path: string, options?: RequestInit) => {
    if (options?.method === 'PATCH') {
      if (++writes === 1) return response({}, 409);
      if (writes === 2) return response({}, 500);
      return response({ validityRule: 'lesson_start', version: 3 });
    }
    if (++reads === 1) return response({}, 500);
    return response({ validityRule: 'lesson_end', version: reads === 2 ? 1 : 2 });
  });
  vi.stubGlobal('fetch', api);
  const user = userEvent.setup();
  render(<BookingValiditySettings csrfToken="csrf" expired={() => false} t={en => en}/>);
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not load booking validity');
  await user.click(screen.getByRole('button', { name: 'Retry booking validity' }));
  const rule = await screen.findByLabelText('Credit validity rule');
  const save = screen.getByRole('button', { name: 'Save booking validity' });
  expect(rule).toHaveValue('lesson_end');
  expect(save).toBeDisabled();
  await user.selectOptions(rule, 'lesson_start');
  await user.click(save);
  expect(await screen.findByRole('alert')).toHaveTextContent('Booking validity changed');
  expect(rule).toHaveValue('lesson_start');
  expect(save).toBeDisabled();
  await user.click(screen.getByRole('button', { name: 'Reload booking validity' }));
  expect(rule).toHaveValue('lesson_start');
  expect(screen.getByText('Last loaded setting · Through lesson end')).toBeVisible();
  await user.click(save);
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not save booking validity');
  expect(rule).toHaveValue('lesson_start');
  await user.click(save);
  expect(await screen.findByRole('status')).toHaveTextContent('Booking validity saved');
  expect(screen.getByText('Last loaded setting · At lesson start')).toBeVisible();
  expect(save).toBeDisabled();
  const patches = api.mock.calls.filter(([, options]) => options?.method === 'PATCH');
  expect(patches).toEqual([1, 2, 2].map(version => [path, expect.objectContaining({
    body: JSON.stringify({ validityRule: 'lesson_start', version }),
    headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }),
  })]));
});

it('shows both rules in Traditional Chinese', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ validityRule: 'lesson_end', version: 1 })));
  render(<BookingValiditySettings csrfToken="csrf" expired={() => false} t={(_en, zh) => zh}/>);
  expect(await screen.findByLabelText('堂數有效期規則')).toHaveValue('lesson_end');
  expect(screen.getByRole('option', { name: '有效至課堂結束' })).toBeVisible();
  expect(screen.getByRole('option', { name: '課堂開始時有效' })).toBeVisible();
});
