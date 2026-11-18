import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import Schedule from './Schedule';
import SchoolSetup from './SchoolSetup';
import { hkDate, hkTime } from './scheduling';

const t = (en: string) => en;
const venue = { id: 'v1', name: 'Harbour Pool', address: '1 Pool Road', region: 'hk-island', active: true, version: 1 };
const classType = { id: 't1', name: 'Youth beginners', level: 'Beginner', active: true, version: 1 };
const coach = { id: 'coach', name: 'Alex', email: 'coach@swim.test', role: 'coach', active: true, version: 1, invitationPending: false };
const session = { id: 'c1', venueId: 'v1', classTypeId: 't1', classTypeVersion: 1, coachId: 'coach', startsAt: '2026-11-18T01:00:00Z', endsAt: '2026-11-18T02:00:00Z', capacity: 6, venueName: 'Harbour Pool', region: 'hk-island', classTypeName: 'Youth beginners', level: 'Beginner', coachName: 'Alex', seriesId: null, version: 1 };
function response(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }); }
function mockAPI(status = 200, roster?: () => Response) {
  const mock = vi.fn(async (path: string, options?: RequestInit) => {
    if (options?.method === 'POST' || options?.method === 'PATCH') {
      if (status !== 200) return response({ error: { code: 'conflict', message: 'Schedule conflict' } }, status);
      if (path.endsWith('/class-series')) return response({ id: 's1', ...JSON.parse(options.body as string), sessions: [session] });
      return response(path.endsWith('/settings/scheduling') ? { travelGapMinutes: 45, version: 2 } : session);
    }
    if (path.endsWith('/skills')) return response({ items: [] });
    if (path.endsWith('/venues')) return response({ items: [venue] });
    if (path.endsWith('/class-types')) return response({ items: [classType] });
    if (path.includes('/users')) return response({ items: [coach] });
    if (path.includes('/class-series')) return response({ items: [] });
    if (path.endsWith('/roster') && roster) return roster();
    if (path.includes('/classes')) return response({ items: [session] });
    if (path.endsWith('/settings/lessons')) return response({ parentChangeCutoffHours: 24, attendanceOpenMinutes: 15, correctionWindowDays: 3, version: 1 });
    if (path.endsWith('/settings/availability')) return response({ lockHours: 72, version: 1 });
    if (path.endsWith('/settings/scheduling')) return response({ travelGapMinutes: 30, version: 1 });
    throw new Error(`Unexpected ${path}`);
  }); vi.stubGlobal('fetch', mock); return mock;
}
beforeEach(() => vi.unstubAllGlobals());

it('renders Hong Kong dates independently of local timezone', () => {
  expect(hkDate(new Date('2026-11-17T20:00:00Z'))).toBe('2026-11-18');
  expect(hkTime('2026-11-18T01:00:00Z')).toBe('09:00');
});

it.each(['parent', 'student'] as const)('shows %s a catalogue without booking or administration controls', async role => {
  mockAPI(); render(<Schedule role={role} csrfToken="csrf" expired={() => false} t={t}/>);
  expect(await screen.findByRole('heading', { name: 'Youth beginners' })).toBeVisible();
  expect(screen.getByText('This is the school’s class catalogue, not your bookings.')).toBeVisible();
  expect(screen.queryByRole('button', { name: /View roster/ })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Add class' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Edit class/ })).not.toBeInTheDocument();
});

it('uses backend-scoped coach schedule without exposing coach switching', async () => {
  const api = mockAPI(); render(<Schedule role="coach" csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('heading', { name: 'Youth beginners' });
  expect(screen.getByRole('heading', { name: 'Your teaching schedule' })).toBeVisible();
  expect(screen.queryByLabelText('Filter coach')).not.toBeInTheDocument();
  expect(api.mock.calls.some(([path]) => path.includes('/users'))).toBe(false);
});

it('preserves class edits on conflict and sends Hong Kong timestamps with the original version', async () => {
  const api = mockAPI(409); const user = userEvent.setup();
  render(<Schedule role="admin" csrfToken="csrf" expired={() => false} t={t}/>);
  await user.click(await screen.findByRole('button', { name: 'Edit class Youth beginners 2026-11-18 09:00' }));
  await user.clear(screen.getByLabelText('Capacity'));
  await user.type(screen.getByLabelText('Capacity'), '8');
  await user.click(screen.getByRole('button', { name: 'Save class' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Your entries are preserved');
  expect(screen.getByLabelText('Capacity')).toHaveValue(8);
  expect(api).toHaveBeenCalledWith('/api/v1/classes/c1', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ venueId: 'v1', classTypeId: 't1', coachId: 'coach', capacity: 8, startsAt: '2026-11-18T09:00:00+08:00', endsAt: '2026-11-18T10:00:00+08:00', version: 1 }) }));
});

it('creates a bounded weekly series from explicit dates and weekday', async () => {
  const api = mockAPI(); const user = userEvent.setup();
  render(<Schedule role="admin" csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('heading', { name: 'Youth beginners' });
  await user.click(screen.getByRole('button', { name: 'Add weekly series' }));
  await user.selectOptions(screen.getByLabelText('Class venue'), 'v1');
  await user.selectOptions(screen.getByLabelText('Class type'), 't1');
  await user.selectOptions(screen.getByLabelText('Assigned coach'), 'coach');
  await user.clear(screen.getByLabelText('Series start date')); await user.type(screen.getByLabelText('Series start date'), '2026-11-18');
  await user.clear(screen.getByLabelText('Series end date')); await user.type(screen.getByLabelText('Series end date'), '2026-11-25');
  await user.selectOptions(screen.getByLabelText('Repeat every'), '3');
  await user.click(screen.getByRole('button', { name: 'Create weekly series' }));
  await waitFor(() => expect(screen.getByText('Weekly series created with 1 classes.')).toBeVisible());
  expect(api).toHaveBeenCalledWith('/api/v1/class-series', expect.objectContaining({ method: 'POST', body: JSON.stringify({ venueId: 'v1', classTypeId: 't1', coachId: 'coach', capacity: 6, startDate: '2026-11-18', endDate: '2026-11-25', weekday: 3, startTime: '09:00', endTime: '10:00' }) }));
});

it('preserves school setup inputs on failed save and keeps archive explicit', async () => {
  const api = mockAPI(409); const user = userEvent.setup();
  render(<SchoolSetup csrfToken="csrf" expired={() => false} t={t}/>);
  await user.click(await screen.findByRole('button', { name: 'Edit venue Harbour Pool' }));
  await user.click(screen.getByLabelText('Available for new classes'));
  await user.clear(screen.getByLabelText('Venue name')); await user.type(screen.getByLabelText('Venue name'), 'Updated pool');
  await user.click(screen.getByRole('button', { name: 'Save venue' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Cancel, refresh');
  expect(screen.getByLabelText('Venue name')).toHaveValue('Updated pool');
  expect(screen.getByLabelText('Available for new classes')).not.toBeChecked();
  expect(api).toHaveBeenCalledWith('/api/v1/venues/v1', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ name: 'Updated pool', address: '1 Pool Road', region: 'hk-island', active: false, version: 1 }) }));
});

it('explains travel-time conflicts without discarding the proposed setting', async () => {
  mockAPI(409); const user = userEvent.setup(); render(<SchoolSetup csrfToken="csrf" expired={() => false} t={t}/>);
  const input = await screen.findByLabelText('Travel gap (minutes)');
  await user.clear(input); await user.type(input, '45');
  await user.click(screen.getByRole('button', { name: 'Save travel time' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('existing future classes conflict');
  expect(input).toHaveValue(45);
});

it('rejects an overly wide filter without requesting classes', async () => {
  const api = mockAPI(); const user = userEvent.setup();
  render(<Schedule role="admin" csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('heading', { name: 'Youth beginners' });
  await user.clear(screen.getByLabelText('From date')); await user.type(screen.getByLabelText('From date'), '2026-01-01');
  await user.clear(screen.getByLabelText('To date')); await user.type(screen.getByLabelText('To date'), '2026-12-31');
  const requestsBefore = api.mock.calls.length;
  await user.click(screen.getByRole('button', { name: 'Apply filters' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('at most 93 days');
  expect(api.mock.calls).toHaveLength(requestsBefore);
});

it('preserves a cross-midnight class end date during editing', async () => {
  const night = { ...session, startsAt: '2026-11-18T15:00:00Z', endsAt: '2026-11-18T17:00:00Z' };
  const base = mockAPI();
  const api = vi.fn(async (path: string, options?: RequestInit) => path.includes('/classes?') ? response({ items: [night] }) : base(path, options));
  vi.stubGlobal('fetch', api); const user = userEvent.setup();
  render(<Schedule role="admin" csrfToken="csrf" expired={() => false} t={t}/>);
  expect(await screen.findByText('23:00–2026-11-19 01:00')).toBeVisible();
  await user.click(await screen.findByRole('button', { name: 'Edit class Youth beginners 2026-11-18 23:00' }));
  expect(screen.getByLabelText('Class end date')).toHaveValue('2026-11-19');
  await user.click(screen.getByRole('button', { name: 'Save class' }));
  expect(api).toHaveBeenCalledWith('/api/v1/classes/c1', expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ venueId: 'v1', classTypeId: 't1', coachId: 'coach', capacity: 6, startsAt: '2026-11-18T23:00:00+08:00', endsAt: '2026-11-19T01:00:00+08:00', version: 1 }) }));
});

it('clears old schedule results when a filter reload fails', async () => {
  let failed = false;
  const base = mockAPI();
  vi.stubGlobal('fetch', vi.fn(async (path: string, options?: RequestInit) => failed && path.includes('/classes?') ? response({}, 500) : base(path, options)));
  const user = userEvent.setup(); render(<Schedule role="admin" csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('heading', { name: 'Youth beginners' });
  failed = true;
  await user.click(screen.getByRole('button', { name: 'Apply filters' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not load the schedule');
  expect(screen.queryByRole('heading', { name: 'Youth beginners' })).not.toBeInTheDocument();
});

it('offers all 20 venues and 10 coaches and submits the selected date and catalogue filters', async () => {
  const venues = Array.from({ length: 20 }, (_, i) => ({ ...venue, id: `v${i}`, name: `Pool ${i}` }));
  const coaches = Array.from({ length: 10 }, (_, i) => ({ ...coach, id: `coach${i}`, name: `Coach ${i}` }));
  const week = Array.from({ length: 50 }, (_, i) => ({ ...session, id: `class-${i}`, classTypeName: `Weekly class ${i}`, coachId: coaches[i % 10].id, venueId: venues[i % 20].id }));
  const api = vi.fn(async (path: string) => response({ items: path.endsWith('/venues') ? venues : path.includes('/users') ? coaches : path.endsWith('/class-types') ? [classType] : path.includes('/classes?') ? week : [] }));
  vi.stubGlobal('fetch', api); const user = userEvent.setup();
  render(<Schedule role="admin" csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('option', { name: 'Pool 19' });
  await screen.findByRole('heading', { name: 'Weekly class 49' });
  expect(screen.getAllByRole('heading', { name: /Weekly class \d/ })).toHaveLength(50);
  expect(screen.getByRole('heading', { name: 'Weekly class 0' })).toBeVisible();
  const venueFilter = screen.getByLabelText('Filter venue'), coachFilter = screen.getByLabelText('Filter coach');
  expect(venueFilter.querySelectorAll('option')).toHaveLength(21);
  expect(coachFilter.querySelectorAll('option')).toHaveLength(11);
  await user.selectOptions(venueFilter, 'v19'); await user.selectOptions(coachFilter, 'coach9');
  await user.clear(screen.getByLabelText('From date')); await user.type(screen.getByLabelText('From date'), '2030-01-07');
  await user.clear(screen.getByLabelText('To date')); await user.type(screen.getByLabelText('To date'), '2030-04-09');
  await user.click(screen.getByRole('button', { name: 'Apply filters' }));
  await waitFor(() => expect(api.mock.calls.some(([path]) => { const q = new URL(path, 'http://test').searchParams; return path.includes('/classes?') && q.get('from') === '2030-01-07' && q.get('to') === '2030-04-09' && q.get('venueId') === 'v19' && q.get('coachId') === 'coach9'; })).toBe(true));
});

it.each(['admin', 'coach'] as const)('opens the live roster for the %s and clears names if access changes', async role => {
  let revoked = false;
  const api = mockAPI(200, () => revoked ? response({}, 404) : response({
    classId: session.id,
    items: [{ bookingId: 'b1', studentId: 'amy', studentName: 'Amy Chan', level: 'Beginner', status: 'reserved' }],
  }));
  render(<Schedule role={role} csrfToken="csrf" expired={() => false} t={t}/>);
  const toggle = await screen.findByRole('button', { name: /View roster Youth beginners/ });
  expect(api.mock.calls.some(([path]) => path.endsWith('/roster'))).toBe(false);
  const user = userEvent.setup();
  await user.click(toggle);
  expect(toggle).toHaveAttribute('aria-expanded', 'true');
  expect(await screen.findByText('Amy Chan')).toBeVisible();
  expect(api).toHaveBeenCalledWith('/api/v1/classes/c1/roster', expect.objectContaining({ signal: expect.any(AbortSignal) }));
  revoked = true;
  await user.click(screen.getByRole('button', { name: 'Reload roster' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('This roster is unavailable for your current access.');
  expect(screen.queryByText('Amy Chan')).not.toBeInTheDocument();
  await user.click(toggle);
  expect(toggle).toHaveAttribute('aria-expanded', 'false');
  expect(screen.queryByRole('region', { name: 'Class roster' })).not.toBeInTheDocument();
});
