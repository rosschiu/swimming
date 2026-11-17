import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import Availability from './Availability';
import AvailabilitySettings from './AvailabilitySettings';

const t = (en: string) => en;
const slot = { id: 'slot1', coachId: 'coach', coachName: 'Alex', venueId: 'venue', venueName: 'Harbour Pool', startsAt: '2030-01-07T01:00:00Z', endsAt: '2030-01-07T02:00:00Z', capacity: 6, status: 'submitted', version: 1, editable: true };
const venue = { id: 'venue', name: 'Harbour Pool', address: '1 Pool Road', region: 'hk-island', active: true, version: 1 };
function response(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }); }
function mockAPI(editable = true, status = 200) {
  let cancelled = false;
  const api = vi.fn(async (path: string, options?: RequestInit) => {
    if (options?.method) {
      if (status !== 200) return response({ error: { code: 'conflict', message: 'Within cutoff' } }, status);
      if (path.endsWith('/cancel')) cancelled = true;
      return response({ ...slot, ...(cancelled ? { status: 'cancelled', editable: false } : {}) });
    }
    if (path.endsWith('/settings/availability')) return response({ lockHours: 72, version: 1 });
    if (path.endsWith('/venues')) return response({ items: [venue] });
    if (path.includes('/users')) return response({ items: [{ id: 'coach', name: 'Alex', role: 'coach', active: true }] });
    if (path.includes('/availability?')) return response({ items: [{ ...slot, editable, ...(cancelled ? { status: 'cancelled', editable: false } : {}) }] });
    throw new Error(`Unexpected request ${path}`);
  }); vi.stubGlobal('fetch', api); return api;
}
beforeEach(() => vi.unstubAllGlobals());

it('shows an assigned coach planning list without coach impersonation or publishing', async () => {
  const api = mockAPI(); render(<Availability role="coach" csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('heading', { name: 'Harbour Pool' });
  expect(screen.getByText(/does not create or guarantee/)).toBeVisible();
  expect(screen.queryByLabelText('Filter coach')).not.toBeInTheDocument();
  expect(api.mock.calls.some(([path]) => path.includes('/users'))).toBe(false);
  expect(screen.queryByRole('button', { name: /Publish/ })).not.toBeInTheDocument();
});

it('shows administrator read-only review and coach filtering', async () => {
  mockAPI(); render(<Availability role="admin" csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('heading', { name: 'Harbour Pool' });
  expect(screen.getByLabelText('Filter coach')).toBeVisible();
  expect(screen.queryByRole('button', { name: 'Add availability' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Edit availability/ })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Cancel availability/ })).not.toBeInTheDocument();
});

it('explains server-locked slots and disables mutation controls', async () => {
  mockAPI(false); render(<Availability role="coach" csrfToken="csrf" expired={() => false} t={t}/>);
  expect(await screen.findByRole('button', { name: 'Edit availability 2030-01-07 09:00' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Cancel availability 2030-01-07 09:00' })).toBeDisabled();
  expect(screen.getByText('This slot is locked by the editing cutoff.')).toBeVisible();
});

it('preserves a slot edit when the cutoff or version changes', async () => {
  const api = mockAPI(true, 409); const user = userEvent.setup();
  render(<Availability role="coach" csrfToken="csrf" expired={() => false} t={t}/>);
  await user.click(await screen.findByRole('button', { name: 'Edit availability 2030-01-07 09:00' }));
  await user.clear(screen.getByLabelText('Capacity')); await user.type(screen.getByLabelText('Capacity'), '8');
  await user.click(screen.getByRole('button', { name: 'Save availability' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Your entries are preserved');
  expect(screen.getByLabelText('Capacity')).toHaveValue(8);
  expect(api).toHaveBeenCalledWith('/api/v1/availability/slot1', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ venueId: 'venue', startsAt: '2030-01-07T09:00:00+08:00', endsAt: '2030-01-07T10:00:00+08:00', capacity: 8, version: 1 }), headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }) }));
});

it('requires confirmation before cancelling a slot and keeps it visible as cancelled', async () => {
  const api = mockAPI(); const user = userEvent.setup();
  render(<Availability role="coach" csrfToken="csrf" expired={() => false} t={t}/>);
  await user.click(await screen.findByRole('button', { name: 'Cancel availability 2030-01-07 09:00' }));
  expect(api.mock.calls.some(([, options]) => options?.method === 'POST')).toBe(false);
  await user.click(screen.getByRole('button', { name: 'Confirm cancellation' }));
  expect(await screen.findByText('Availability cancelled.')).toBeVisible();
  expect(screen.getByText('Cancelled slots cannot be edited.')).toBeVisible();
  expect(api).toHaveBeenCalledWith('/api/v1/availability/slot1/cancel', expect.objectContaining({ method: 'POST', body: JSON.stringify({ version: 1 }) }));
});

it('preserves the administrator cutoff value after a stale update', async () => {
  mockAPI(true, 409); const user = userEvent.setup();
  render(<AvailabilitySettings csrfToken="csrf" expired={() => false} t={t}/>);
  const hours = await screen.findByLabelText('Availability lock (hours)');
  await user.clear(hours); await user.type(hours, '48');
  await user.click(screen.getByRole('button', { name: 'Save availability cutoff' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('cutoff changed');
  expect(hours).toHaveValue(48);
});

it('filters by explicit dates and clears stale rows after a failed reload', async () => {
  let failed = false; const base = mockAPI();
  const api = vi.fn(async (path: string, options?: RequestInit) => failed && path.includes('/availability?') ? response({}, 500) : base(path, options));
  vi.stubGlobal('fetch', api); const user = userEvent.setup();
  render(<Availability role="coach" csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('heading', { name: 'Harbour Pool' });
  failed = true;
  await user.click(screen.getByRole('button', { name: 'Apply filters' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not load availability');
  await waitFor(() => expect(screen.queryByRole('heading', { name: 'Harbour Pool' })).not.toBeInTheDocument());
});

it('preserves an archived venue when editing an existing slot', async () => {
  const base = mockAPI();
  vi.stubGlobal('fetch', vi.fn(async (path: string, options?: RequestInit) => path.endsWith('/venues') ? response({ items: [] }) : base(path, options)));
  const user = userEvent.setup(); render(<Availability role="coach" csrfToken="csrf" expired={() => false} t={t}/>);
  await user.click(await screen.findByRole('button', { name: 'Edit availability 2030-01-07 09:00' }));
  expect(screen.getByLabelText('Availability venue')).toHaveValue('venue');
  expect(screen.getByRole('option', { name: 'Harbour Pool (archived)' })).toBeVisible();
  await user.click(screen.getByRole('button', { name: 'Save availability' }));
  expect(base).toHaveBeenCalledWith('/api/v1/availability/slot1', expect.objectContaining({ method: 'PATCH', body: expect.stringContaining('"venueId":"venue"') }));
});
