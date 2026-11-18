import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import ClassRoster from './ClassRoster';
import StudentBookings from './StudentBookings';

const props = { expired: () => false, t: (en: string) => en, csrfToken: 'csrf' };
const opensAt = '2026-11-18T01:15:00Z';
const attendance = { status: 'present', markedAt: opensAt, creditOutcome: 'consumed', revision: 1 };
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
  expect(mark).toHaveFocus();
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
  expect(within(marked).getByText('Present · credit used')).toBeVisible();
  expect(within(marked).getByText('Reserved')).toBeVisible();
  expect(within(unmarked).getByText('Unmarked')).toBeVisible();
  expect(screen.queryByRole('button', { name: /Mark present/ })).not.toBeInTheDocument();
});

it('marks an absence after the lesson and corrects it with a reason and the current revision', async () => {
  const bodies: Record<string, unknown>[] = [];
  let stale = true;
  const absent = { status: 'absent', markedAt: opensAt, creditOutcome: 'retained', revision: 1 };
  vi.stubGlobal('fetch', vi.fn(async (path: string, options?: RequestInit) => {
    if (options?.method === 'PUT') { bodies.push(JSON.parse(String(options.body))); return Response.json(absent); }
    if (options?.method === 'POST') {
      bodies.push(JSON.parse(String(options.body)));
      if (stale) { stale = false; return Response.json({ error: { code: 'attendance_changed' } }, { status: 409 }); }
      return Response.json({ status: 'present', markedAt: opensAt, creditOutcome: 'consumed', revision: 2 });
    }
    expect(path).toBe('/api/v1/classes/c1/roster');
    return Response.json({ classId: 'c1', attendanceOpensAt: '2020-01-01T00:00:00Z', absenceOpensAt: '2020-01-01T01:00:00Z', correctionClosesAt: '2999-01-01T00:00:00Z', items: [entry] });
  }));
  render(<ClassRoster classId="c1" {...props}/>);
  const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Mark absent Amy Chan' }));
  expect(screen.getByText(/used or returned under the package's absence policy/)).toBeVisible();
  await user.click(screen.getByRole('button', { name: 'Confirm absent' }));
  expect(await screen.findByText('Absent')).toBeVisible();
  expect(screen.getByText('credit returned')).toBeVisible();
  expect(bodies[0]).toEqual({ status: 'absent' });
  await user.click(screen.getByRole('button', { name: 'Correct attendance Amy Chan' }));
  expect(screen.getAllByRole('radio').map(radio => (radio as HTMLInputElement).value)).toEqual(['present', 'ill']);
  const save = screen.getByRole('button', { name: 'Save correction' });
  await user.click(screen.getByRole('radio', { name: 'Present' }));
  expect(save).toBeDisabled();
  await user.type(screen.getByLabelText('Reason for correction'), 'Arrived late, register missed');
  await user.click(save);
  expect(await screen.findByRole('alert')).toHaveTextContent('Someone changed this attendance');
  expect(screen.getByLabelText('Reason for correction')).toHaveValue('Arrived late, register missed');
  await user.click(save);
  expect(await screen.findByText('Attendance corrected for Amy Chan.')).toBeVisible();
  expect(screen.getByText('Present')).toBeVisible();
  expect(screen.getByText('credit used · corrected')).toBeVisible();
  expect(bodies[1]).toEqual({ status: 'present', reason: 'Arrived late, register missed', revision: 1, idempotencyKey: expect.any(String) });
  expect(bodies[2]).toEqual(bodies[1]);
});
