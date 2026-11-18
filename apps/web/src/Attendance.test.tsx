import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import ClassRoster from './ClassRoster';
import StudentBookings from './StudentBookings';

const props = { expired: () => false, t: (en: string) => en, csrfToken: 'csrf' };
const opensAt = '2026-11-18T01:15:00Z';
const attendance = { status: 'present', markedAt: opensAt };
const entry = { bookingId: 'b1', studentId: 'amy', studentName: 'Amy Chan', level: 'Beginner', status: 'reserved' };

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it('enables marking at the reported opening time', async () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-11-18T01:14:59Z'));
  vi.stubGlobal('fetch', vi.fn(async () => Response.json({ classId: 'c1', attendanceOpensAt: opensAt, items: [entry] })));
  await act(async () => { render(<ClassRoster classId="c1" {...props}/>); });
  const mark = screen.getByRole('button', { name: 'Mark present Amy Chan' });
  expect(mark).toBeDisabled();
  expect(screen.getByText('2026-11-18 09:15')).toBeVisible();
  act(() => vi.advanceTimersByTime(1000));
  expect(mark).toBeEnabled();
});

it('reviews the credit use, handles a rejected mark, and displays a recorded result on retry and reload', async () => {
  let conflict = true;
  let marked = false;
  const api = vi.fn(async (_path: string, options?: RequestInit) => {
    if (options?.method === 'PUT') {
      if (conflict) return Response.json({ error: { code: 'credit_unavailable' } }, { status: 409 });
      marked = true;
      return Response.json(attendance);
    }
    return Response.json({ classId: 'c1', attendanceOpensAt: '2020-01-01T00:00:00Z', items: [{ ...entry, ...(marked && { attendance }) }] });
  });
  vi.stubGlobal('fetch', api);
  render(<ClassRoster classId="c1" {...props}/>);
  const user = userEvent.setup();
  const mark = await screen.findByRole('button', { name: 'Mark present Amy Chan' });
  await user.click(mark);
  expect(screen.getByText(/use one reserved lesson credit/)).toBeVisible();
  await user.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(api).toHaveBeenCalledTimes(1);
  await user.click(mark);
  const confirm = screen.getByRole('button', { name: 'Confirm present' });
  await user.click(confirm);
  expect(await screen.findByRole('alert')).toHaveTextContent('reserved lesson credit is unavailable');
  expect(screen.getByText('Reserved')).toBeVisible();
  conflict = false;
  await user.click(confirm);
  expect(api).toHaveBeenLastCalledWith('/api/v1/classes/c1/roster/b1/attendance', expect.objectContaining({
    method: 'PUT', headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }),
  }));
  expect(await screen.findByText('Present')).toBeVisible();
  expect(mark).not.toBeInTheDocument();
  await user.click(screen.getByRole('button', { name: 'Reload roster' }));
  expect(await screen.findByText('Present')).toBeVisible();
});

it('shows students the attendance outcome separately from the reservation', async () => {
  const lesson = { classTypeName: 'Youth beginners', venueName: 'Harbour Pool', coachName: 'Alex', startsAt: '2026-11-18T01:00:00Z', endsAt: '2026-11-18T02:00:00Z' };
  vi.stubGlobal('fetch', vi.fn(async () => Response.json({ items: [
    { id: 'b1', class: lesson, status: 'reserved', attendance },
    { id: 'b2', class: lesson, status: 'reserved' },
  ] })));
  render(<StudentBookings {...props} role="student" student={{ id: 'amy', name: entry.studentName, level: entry.level, version: 1 }} onBooked={vi.fn()}/>);
  const [marked, unmarked] = await screen.findAllByRole('listitem');
  expect(within(marked).getByText('Present')).toBeVisible();
  expect(within(marked).getByText('Reserved')).toBeVisible();
  expect(within(unmarked).getByText('Unmarked')).toBeVisible();
  expect(screen.queryByRole('button', { name: /Mark present/ })).not.toBeInTheDocument();
});
