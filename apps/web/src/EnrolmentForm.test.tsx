import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import EnrolmentForm from './EnrolmentForm';
import { hkDate } from './scheduling';

const student = { id: 'amy', name: 'Amy', level: 'Beginner', version: 1 };
const props = { student, admin: false, csrfToken: 'csrf', expired: () => false, t: (en: string) => en };
const date = (days: number) => hkDate(new Date(Date.now() + days * 86400000));
const week = (id: string, days: number, seriesId: string | null = 'series') => ({
  id, seriesId, version: 1, classTypeVersion: 2, classTypeName: 'Swim lessons',
  startsAt: `${date(days)}T09:00:00+08:00`, endsAt: `${date(days)}T10:00:00+08:00`,
  venueName: 'Pool', coachName: 'Alex', level: 'Beginner',
});
const first = week('c1', 3);
const skipped = week('c2', 10);
const third = week('c3', 17);
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });

afterEach(() => vi.unstubAllGlobals());

it('confirms exactly the selected nonconsecutive weeks and safely retries the same request', async () => {
  const writes: Record<string, unknown>[] = [];
  const onBooked = vi.fn();
  const fetch = vi.fn(async (path: string, options?: RequestInit) => {
    if (path.includes('/classes?')) return response({ items: [first, skipped, third, week('standalone', 17, null), { ...week('advanced', 10, 'advanced-series'), level: 'Advanced' }] });
    if (path.endsWith('/credit-history')) return response({ bookingValidity: { validityRule: 'lesson_end', version: 3 }, items: [
      { id: 'expired-before-last', available: 2, expiresOn: date(10) },
      { id: 'one-credit', available: 1, expiresOn: null }, { id: 'exact-package', available: 2, expiresOn: null },
    ] });
    writes.push(JSON.parse(String(options?.body)));
    return writes.length === 1 ? response({}, 500) : response({ bookings: [{ class: first }, { class: third }] });
  });
  vi.stubGlobal('fetch', fetch);
  render(<EnrolmentForm {...props} onBooked={onBooked}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Enrol in weekly lessons' }));
  await user.selectOptions(await screen.findByLabelText('Weekly series'), 'series');
  expect(screen.getAllByRole('checkbox')).toHaveLength(3);
  expect(screen.queryByRole('option', { name: /Advanced/ })).not.toBeInTheDocument();
  await user.click(screen.getByRole('checkbox', { name: new RegExp(date(3)) }));
  await user.click(screen.getByRole('checkbox', { name: new RegExp(date(17)) }));
  const packages = screen.getByLabelText('Enrolment credit package');
  expect(screen.queryByRole('option', { name: /expired-before-last/ })).not.toBeInTheDocument();
  await user.selectOptions(packages, 'one-credit');
  expect(screen.getByRole('button', { name: 'Review enrolment' })).toBeDisabled();
  await user.selectOptions(packages, 'exact-package');
  await user.click(screen.getByRole('button', { name: 'Review enrolment' }));
  expect(screen.getByText(/Confirm to reserve 2 credits/)).toBeVisible();
  expect(screen.getAllByRole('listitem').map(item => item.textContent)).toEqual([expect.stringContaining(date(3)), expect.stringContaining(date(17))]);
  expect(writes).toHaveLength(0);
  await user.click(screen.getByRole('button', { name: 'Confirm weekly enrolment' }));
  await user.click(await screen.findByRole('button', { name: 'Retry same enrolment' }));
  await screen.findByText(/Weekly enrolment confirmed/);
  expect(writes).toHaveLength(2);
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0]).toEqual({ seriesId: 'series', sessions: [
    { classId: 'c1', classVersion: 1, classTypeVersion: 2 },
    { classId: 'c3', classVersion: 1, classTypeVersion: 2 },
  ], creditPackageId: 'exact-package', bookingValidityVersion: 3, idempotencyKey: expect.any(String) });
  expect(onBooked).toHaveBeenCalledWith([first.startsAt, third.startsAt]);
  expect(fetch).toHaveBeenCalledWith('/api/v1/students/amy/enrolments', expect.objectContaining({ method: 'POST', headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }) }));
});

it('requires an administrator reason for a level mismatch and resets stale choices for a fresh review', async () => {
  const writes: Record<string, unknown>[] = [];
  const onBooked = vi.fn();
  vi.stubGlobal('fetch', vi.fn(async (path: string, options?: RequestInit) => {
    if (path.includes('/classes?')) return response({ items: [{ ...first, level: 'Advanced', classTypeVersion: writes.length + 1 }] });
    if (path.endsWith('/credit-history')) return response({ bookingValidity: { validityRule: 'lesson_end', version: writes.length + 1 }, items: [{ id: 'package', available: 1, expiresOn: null }] });
    writes.push(JSON.parse(String(options?.body)));
    return response({ error: { code: 'version_conflict' } }, 409);
  }));
  render(<EnrolmentForm {...props} admin onBooked={onBooked}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Enrol in weekly lessons' }));
  for (const version of [1, 2]) {
    await user.selectOptions(await screen.findByLabelText('Weekly series'), 'series');
    await user.click(screen.getByRole('checkbox'));
    await user.selectOptions(screen.getByLabelText('Enrolment credit package'), 'package');
    expect(screen.getByRole('button', { name: 'Review enrolment' })).toBeDisabled();
    await user.type(screen.getByLabelText('Level override reason'), 'Coach assessed swimmer');
    await user.click(screen.getByRole('button', { name: 'Review enrolment' }));
    await user.click(screen.getByRole('button', { name: 'Confirm weekly enrolment' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('A selected lesson changed.');
    await waitFor(() => expect(screen.getByLabelText('Weekly series')).toHaveValue(''));
    expect(writes.at(-1)).toMatchObject({
      sessions: [{ classId: 'c1', classVersion: 1, classTypeVersion: version }], bookingValidityVersion: version, overrideReason: 'Coach assessed swimmer',
    });
  }
  expect(writes).toHaveLength(2);
  expect(writes[0].idempotencyKey).not.toBe(writes[1].idempotencyKey);
  expect(onBooked).not.toHaveBeenCalled();
});
