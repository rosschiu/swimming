import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import LessonChanges, { RequestLessonChange } from './LessonChanges';

const props = { csrfToken: 'csrf-value', expired: () => false, t: (en: string) => en };
const originalClass = {
  id: 'original', version: 1, startsAt: '2027-01-05T03:00:00Z', endsAt: '2027-01-05T04:00:00Z',
  classTypeName: 'Beginner', venueName: 'Kowloon Pool', coachName: 'Coach A',
};
const targetClass = { ...originalClass, id: 'target', startsAt: '2027-01-06T03:00:00Z', endsAt: '2027-01-06T04:00:00Z' };
const pending = {
  id: 'change', bookingId: 'booking', studentId: 'student', studentName: 'Amy Chan',
  originalClass, targetClass, status: 'pending', requestedAt: '2026-12-01T03:00:00Z', requestedBy: 'guardian-id',
};
const rejected = { ...pending, status: 'rejected', decidedAt: pending.requestedAt, decidedBy: 'coach-id', decisionReason: 'Series finished' };
const options = { cutoffHours: 24, items: [targetClass] };
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });

afterEach(() => vi.unstubAllGlobals());

it('requires a guardian to select a replacement and safely retries the same intent', async () => {
  const fetch = vi.fn()
    .mockResolvedValueOnce(response(options))
    .mockResolvedValueOnce(response({}, 500))
    .mockResolvedValueOnce(response(pending, 201));
  vi.stubGlobal('fetch', fetch);
  render(<RequestLessonChange {...props} studentId="student" bookingId="booking"/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Request lesson change' }));
  expect(fetch).not.toHaveBeenCalled();
  await user.click(screen.getByRole('button', { name: 'Find replacement lessons' }));
  expect(fetch.mock.calls[0][0]).toMatch(/^\/api\/v1\/students\/student\/bookings\/booking\/change-options\?from=\d{4}-\d{2}-\d{2}&to=\d{4}-\d{2}-\d{2}$/);
  const replacement = await screen.findByLabelText('Replacement lesson');
  const send = screen.getByRole('button', { name: 'Send change request' });
  expect(send).toBeDisabled();
  expect(screen.getByText(/at least 24 hours/)).toBeInTheDocument();
  await user.selectOptions(replacement, 'target');
  await user.click(send);
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not send the request');
  await user.click(send);
  expect(await screen.findByRole('status')).toHaveTextContent('Lesson change request received. Pending');
  const [path, request] = fetch.mock.calls[1];
  expect(path).toBe('/api/v1/students/student/bookings/booking/change-requests');
  expect(request).toMatchObject({ method: 'POST', headers: { 'X-CSRF-Token': 'csrf-value' } });
  expect(JSON.parse(request.body)).toEqual({ targetClassId: 'target', targetClassVersion: 1, idempotencyKey: expect.any(String) });
  expect(fetch.mock.calls[2][1].body).toBe(request.body);
});

it('drops a stale guardian selection and can refresh the options in Chinese', async () => {
  const fetch = vi.fn()
    .mockResolvedValueOnce(response(options))
    .mockResolvedValueOnce(response({ error: { code: 'class_full' } }, 409))
    .mockResolvedValueOnce(response({ ...options, items: [] }));
  vi.stubGlobal('fetch', fetch);
  render(<RequestLessonChange {...props} t={(_en, zh) => zh} studentId="student" bookingId="booking"/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: '申請調課' }));
  const find = screen.getByRole('button', { name: '搜尋可替換課堂' });
  await user.click(find);
  await user.selectOptions(await screen.findByLabelText('替換課堂'), 'target');
  await user.click(screen.getByRole('button', { name: '送出調課申請' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('替換課堂已滿');
  expect(screen.queryByLabelText('替換課堂')).not.toBeInTheDocument();
  await user.click(find);
  expect(await screen.findByRole('status')).toHaveTextContent('沒有可替換的課堂');
});

it('keeps a coach request actionable after failed approval and sends an explicit rejection', async () => {
  const fetch = vi.fn()
    .mockResolvedValueOnce(response({ items: [pending], cutoffHours: 24 }))
    .mockResolvedValueOnce(response({ error: { code: 'class_full' } }, 409))
    .mockResolvedValueOnce(response(rejected));
  vi.stubGlobal('fetch', fetch);
  render(<LessonChanges {...props} follow={vi.fn()} role="coach"/>);
  const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Approve change' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('replacement lesson is full');
  expect(screen.getByText('Pending')).toBeInTheDocument();
  await user.type(screen.getByLabelText('Decision reason (optional)'), 'Series finished');
  await user.click(screen.getByRole('button', { name: 'Reject change' }));
  expect(await screen.findByText('Lesson change rejected.')).toHaveAttribute('role', 'status');
  expect(screen.queryByText('Amy Chan')).not.toBeInTheDocument();
  expect(fetch.mock.calls[1][1].body).toBe(JSON.stringify({ decision: 'approved' }));
  expect(fetch.mock.calls[2]).toEqual(['/api/v1/lesson-changes/change/decision', expect.objectContaining({
    method: 'PUT', headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf-value' }),
    body: JSON.stringify({ decision: 'rejected', reason: 'Series finished' }),
  })]);
});

it('shows read-only administrator history with attribution and can retry a list error', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(response({}, 500))
    .mockResolvedValueOnce(response({ items: [pending, { ...rejected, id: 'past' }], cutoffHours: 24 }));
  vi.stubGlobal('fetch', fetch);
  render(<LessonChanges {...props} follow={vi.fn()} role="admin"/>);
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not load lesson changes');
  await userEvent.setup().click(screen.getByRole('button', { name: 'Refresh requests' }));
  expect(await screen.findByText('Rejected')).toBeInTheDocument();
  expect(screen.getAllByText(/guardian-id/)).toHaveLength(2);
  expect(screen.getByText(/coach-id/)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Approve change' })).not.toBeInTheDocument();
  expect(fetch.mock.calls.map(([path]) => path)).toEqual(['/api/v1/lesson-changes', '/api/v1/lesson-changes']);
});

it('loads older history with an opaque cursor and retains loaded rows', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(response({ items: [pending], nextCursor: 'opaque+cursor', cutoffHours: 24 }))
    .mockResolvedValueOnce(response({ items: [{ ...rejected, id: 'past' }], nextCursor: null, cutoffHours: 24 }));
  vi.stubGlobal('fetch', fetch);
  render(<LessonChanges {...props} follow={vi.fn()} role="admin"/>);
  await userEvent.setup().click(await screen.findByRole('button', { name: 'Load more requests' }));
  expect(await screen.findByText('Rejected')).toBeInTheDocument();
  expect(screen.getByText('Pending')).toBeInTheDocument();
  expect(fetch.mock.calls[1][0]).toBe('/api/v1/lesson-changes?cursor=opaque%2Bcursor');
  expect(screen.queryByRole('button', { name: 'Load more requests' })).not.toBeInTheDocument();
});

it('starts with the coach pending queue and can switch to the complete history', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(response({ items: [], nextCursor: null, cutoffHours: 24 }))
    .mockResolvedValueOnce(response({ items: [rejected], nextCursor: null, cutoffHours: 24 }));
  vi.stubGlobal('fetch', fetch);
  render(<LessonChanges {...props} follow={vi.fn()} role="coach"/>);
  await screen.findByText('No pending lesson change requests.');
  const filter = screen.getByRole('checkbox', { name: 'Show pending requests only' });
  expect(filter).toBeChecked();
  await userEvent.setup().click(filter);
  expect(await screen.findByText('Rejected')).toBeInTheDocument();
  expect(fetch.mock.calls.map(([path]) => path)).toEqual(['/api/v1/lesson-changes?status=pending', '/api/v1/lesson-changes']);
});
