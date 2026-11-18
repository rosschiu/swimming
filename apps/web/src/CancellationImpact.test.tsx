import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import CancellationImpact from './CancellationImpact';

const props = { classId: 'original', csrfToken: 'csrf', expired: () => false, t: (en: string) => en };
const lesson = {
  id: 'original', classTypeName: 'Morning group', venueName: 'Harbour Pool', coachName: 'Coach Alex',
  startsAt: '2030-01-07T17:00:00+08:00', endsAt: '2030-01-07T10:00:00.100Z',
};
const result = {
  lesson, reviewedAt: '2030-01-07T09:00:00Z', bookedSwimmers: 0,
  unmarkedBookings: 0, recordedAttendance: 0, pendingInbound: 0, pendingOutbound: 0,
  bookings: [], requests: [], nextCursor: null,
};
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
afterEach(() => vi.unstubAllGlobals());

it.each([
  ['2030-01-07T08:59:59.999Z', 'Upcoming'],
  ['2030-01-07T09:00:00Z', 'Started'],
  ['2030-01-07T10:00:00Z', 'Started'],
  ['2030-01-07T18:00:00.100+08:00', 'Ended'],
])('labels the lesson at %s as %s', async (reviewedAt, state) => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ ...result, reviewedAt })));
  render(<CancellationImpact {...props}/>);
  expect(await screen.findByText(new RegExp(` · ${state}$`))).toBeVisible();
});

it('ignores an old section response and refreshes the currently selected section', async () => {
  let resolve!: (value: Response) => void;
  const earlier = new Promise<Response>(done => { resolve = done; });
  const fetch = vi.fn(async (url: string) => url.includes('section=bookings') ? earlier : response({
    ...result, pendingInbound: 1,
    requests: [{ id: 'req', bookingId: 'booking', studentName: 'Amy', otherClassId: 'other',
      otherStartsAt: lesson.startsAt, otherEndsAt: lesson.endsAt, otherVenue: 'Garden Pool' }],
  }));
  vi.stubGlobal('fetch', fetch);
  render(<CancellationImpact {...props}/>);
  const user = userEvent.setup();
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
  await user.selectOptions(screen.getByLabelText('Impact details'), 'inbound');
  expect(await screen.findByText('Amy')).toBeVisible();
  expect(screen.getByText(/Garden Pool/)).toBeVisible();
  await act(async () => resolve(response({ ...result, lesson: { ...lesson, classTypeName: 'Out of date' } })));
  expect(screen.queryByText(/Out of date/)).not.toBeInTheDocument();
  await user.click(screen.getByRole('button', { name: 'Refresh impact' }));
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(3));
  expect(fetch.mock.calls[2][0]).toContain('section=inbound');
});
