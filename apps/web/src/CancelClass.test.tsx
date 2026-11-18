import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import CancelClass from './CancelClass';
import type { components } from './generated/api';

type Impact = components['schemas']['CancellationImpact'];
const lesson = { id: 'c1', version: 3, startsAt: '2099-01-07T02:00:00Z', endsAt: '2099-01-07T03:00:00Z', classTypeName: 'Water Confidence', venueName: 'Harbour Pool', coachName: 'Alex' };
const impact = { lesson, reviewedAt: '2099-01-01T00:00:00Z', bookedSwimmers: 2, unmarkedBookings: 2, recordedAttendance: 0, pendingInbound: 1, pendingOutbound: 0,
  bookings: [{ bookingId: 'b-ben', studentName: 'Ben' }], requests: [], nextCursor: null } as unknown as Impact;
const props = { csrfToken: 'csrf', expired: () => false, t: (en: string) => en };

afterEach(() => vi.unstubAllGlobals());

it('asks for a new expiry for an expired package, then cancels and reports the result', async () => {
  const bodies: Record<string, unknown>[] = [];
  vi.stubGlobal('fetch', vi.fn(async (_path: string, options?: RequestInit) => {
    bodies.push(JSON.parse(String(options?.body)));
    if (bodies.length === 1) return new Response(JSON.stringify({ error: { code: 'expiry_required', details: { bookingIds: ['b-ben'] } } }), { status: 409 });
    return new Response(JSON.stringify({ lesson: { ...lesson, cancelledAt: '2099-01-01T00:00:00Z' }, reason: 'Pool closed', cancelledAt: '2099-01-01T00:00:00Z', cancelledBookings: 2, restoredCredits: 1, closedRequests: 1 }));
  }));
  const onCancelled = vi.fn();
  render(<CancelClass impact={impact} {...props} onCancelled={onCancelled}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Cancel this lesson for everyone' }));
  expect(screen.getByText(/returns credits to 2 booked swimmers and closes 1 pending/)).toBeVisible();
  const confirm = screen.getByRole('button', { name: 'Confirm school cancellation' });
  expect(confirm).toBeDisabled();
  await user.type(screen.getByLabelText('Reason shown in the audit history'), 'Pool closed');
  await user.click(confirm);
  expect(await screen.findByRole('alert')).toHaveTextContent('Enter a new expiry for each');
  expect(confirm).toBeDisabled();
  await user.type(screen.getByLabelText('New expiry for Ben'), '2099-03-31');
  await user.click(confirm);
  expect(await screen.findByText(/2 bookings cancelled and their credits returned; 1 restored with a new expiry; 1 lesson change requests closed/)).toBeVisible();
  expect(onCancelled).toHaveBeenCalled();
  expect(bodies[0]).toEqual({ reason: 'Pool closed', version: 3, idempotencyKey: expect.any(String) });
  expect(bodies[1]).toEqual({ reason: 'Pool closed', version: 3, expiries: { 'b-ben': '2099-03-31' }, idempotencyKey: expect.any(String) });
  expect(bodies[1].idempotencyKey).not.toBe(bodies[0].idempotencyKey);
});

it('does not offer cancellation once a lesson has started or is cancelled', () => {
  const { rerender } = render(<CancelClass impact={{ ...impact, reviewedAt: lesson.startsAt }} {...props} onCancelled={vi.fn()}/>);
  expect(screen.getByText('Started lessons are handled through attendance corrections.')).toBeVisible();
  rerender(<CancelClass impact={{ ...impact, lesson: { ...lesson, cancelledAt: '2099-01-01T00:00:00Z' } } as unknown as Impact} {...props} onCancelled={vi.fn()}/>);
  expect(screen.getByText(/This lesson was cancelled by the school on/)).toBeVisible();
  expect(screen.queryByRole('button', { name: 'Cancel this lesson for everyone' })).not.toBeInTheDocument();
});
