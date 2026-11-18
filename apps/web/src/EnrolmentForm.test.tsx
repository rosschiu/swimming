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
const settings = { allocationPolicy: 'all_or_none', renewalPolicy: 'same_offer', version: 1 };
const history = { validityRule: 'lesson_end', version: 3 };
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
    if (!options?.method && path.endsWith('/enrolments')) return response({ items: [], settings });
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
  expect(packages.querySelector('option[value="expired-before-last"]')).toBeNull();
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
  ], creditPackageId: 'exact-package', bookingValidityVersion: 3, settingsVersion: 1, idempotencyKey: expect.any(String) });
  expect(onBooked).toHaveBeenCalledWith([first.startsAt, third.startsAt]);
  expect(fetch).toHaveBeenCalledWith('/api/v1/students/amy/enrolments', expect.objectContaining({ method: 'POST', headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }) }));
});

it('requires an administrator reason for a level mismatch and resets stale choices for a fresh review', async () => {
  const writes: Record<string, unknown>[] = [];
  const onBooked = vi.fn();
  vi.stubGlobal('fetch', vi.fn(async (path: string, options?: RequestInit) => {
    if (path.includes('/classes?')) return response({ items: [{ ...first, level: 'Advanced', classTypeVersion: writes.length + 1 }] });
    if (path.endsWith('/credit-history')) return response({ bookingValidity: { validityRule: 'lesson_end', version: writes.length + 1 }, items: [{ id: 'package', available: 1, expiresOn: null }] });
    if (!options?.method && path.endsWith('/enrolments')) return response({ items: [], settings });
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


it('requires a second confirmation for the exact available subset and retries that confirmation', async () => {
  const writes: Record<string, unknown>[] = [];
  const onBooked = vi.fn();
  vi.stubGlobal('fetch', vi.fn(async (path: string, options?: RequestInit) => {
    if (path.includes('/classes?')) return response({ items: [first, skipped] });
    if (path.endsWith('/credit-history')) return response({ bookingValidity: history, items: [{ id: 'one', available: 1, expiresOn: date(3) }] });
    if (!options?.method && path.endsWith('/enrolments')) return response({ items: [], settings: { ...settings, allocationPolicy: 'confirm_available_subset' } });
    writes.push(JSON.parse(String(options?.body)));
    if (writes.length === 1) return response({ error: { code: 'partial_enrolment_confirmation_required', details: { availableSessionIds: [first.id], settingsVersion: 1 } } }, 409);
    if (writes.length === 2) return response({}, 500);
    return response({ bookings: [{ class: first }] });
  }));
  render(<EnrolmentForm {...props} onBooked={onBooked}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Enrol in weekly lessons' }));
  await user.selectOptions(await screen.findByLabelText('Weekly series'), 'series');
  for (const item of screen.getAllByRole('checkbox')) await user.click(item);
  await user.selectOptions(screen.getByLabelText('Enrolment credit package'), 'one');
  await user.click(screen.getByRole('button', { name: 'Review enrolment' }));
  await user.click(screen.getByRole('button', { name: 'Confirm weekly enrolment' }));
  const confirm = await screen.findByRole('button', { name: 'Confirm available weeks' });
  expect(writes).toHaveLength(1);
  expect(writes[0]).not.toHaveProperty('partialConfirmation');
  expect(screen.getAllByRole('listitem')).toHaveLength(1);
  expect(screen.getByText(/Weeks no longer available/)).toHaveTextContent(date(10));
  await user.click(confirm);
  await user.click(await screen.findByRole('button', { name: 'Retry same enrolment' }));
  await screen.findByText(/Weekly enrolment confirmed/);
  expect(writes[1]).toEqual({ ...writes[0], partialConfirmation: { sessionIds: [first.id], settingsVersion: 1 } });
  expect(writes[2]).toEqual(writes[1]);
  expect(onBooked).toHaveBeenCalledWith([first.startsAt]);
});

it.each([
  { allocationPolicy: 'all_or_none', renewalPolicy: 'same_offer', carryForwardEnabled: true, destination: 'next-same', carried: 2 },
  { allocationPolicy: 'all_or_none', renewalPolicy: 'any_paid_package', carryForwardEnabled: false, destination: 'next-other', carried: 0 },
  { allocationPolicy: 'confirm_available_subset', renewalPolicy: 'any_paid_package', carryForwardEnabled: false, destination: 'next-other', carried: 0 },
])('renews a different published series with $renewalPolicy and carry-forward $carryForwardEnabled', async ({ allocationPolicy, renewalPolicy, carryForwardEnabled, destination, carried }) => {
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  const onBooked = vi.fn();
  const next = week('cross-term', 17, 'new-term');
  vi.stubGlobal('fetch', vi.fn(async (path: string, options?: RequestInit) => {
    if (path.includes('/classes?')) return response({ items: [first, next] });
    if (path.endsWith('/credit-history')) return response({ bookingValidity: history, items: [
      { id: 'old-pack', purchaseId: 'old-purchase', offerId: 'same', available: 2, expiresOn: null },
      { id: 'next-same', purchaseId: 'new-purchase', offerId: 'same', available: 0, expiresOn: null },
      { id: 'next-other', purchaseId: 'other-purchase', offerId: 'different', available: 1, expiresOn: null },
    ] });
    if (!options?.method && path.endsWith('/enrolments')) return response({
      items: [{ id: 'source', seriesId: 'old-term', creditPackageId: 'old-pack', purchaseId: 'old-purchase', offerId: 'same', lastLessonStartsAt: skipped.startsAt, carryForwardEnabled }],
      settings: { allocationPolicy, renewalPolicy, version: 5 },
    });
    writes.push({ path, body: JSON.parse(String(options?.body)) });
    return response({ bookings: [{ class: next }], carriedForwardCredits: carried });
  }));
  render(<EnrolmentForm {...props} onBooked={onBooked}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Renew weekly enrolment' }));
  await user.selectOptions(await screen.findByLabelText('Enrolment to renew'), 'source');
  await user.selectOptions(screen.getByLabelText('Weekly series'), 'new-term');
  expect(screen.getAllByRole('checkbox')).toHaveLength(1);
  await user.click(screen.getByRole('checkbox'));
  const packages = screen.getByLabelText('Enrolment credit package');
  const review = screen.getByRole('button', { name: 'Review enrolment' });
  if (renewalPolicy === 'same_offer') expect(document.querySelector('option[value="next-other"]')).toBeNull();
  if (!carryForwardEnabled) {
    await user.selectOptions(packages, 'next-same');
    expect(review).toBeDisabled();
  }
  await user.selectOptions(packages, destination);
  await user.click(review);
  expect(screen.getByText(carryForwardEnabled ? /amount is calculated/ : /keeps its unused credits/)).toBeVisible();
  expect(writes).toHaveLength(0);
  await user.click(screen.getByRole('button', { name: 'Confirm renewal' }));
  expect(await screen.findByText(/Renewal confirmed/)).toHaveTextContent(`${carried} credits carried forward`);
  expect(writes).toEqual([{
    path: '/api/v1/students/amy/enrolments/source/renewals',
    body: {
      seriesId: 'new-term', sourcePackageId: 'old-pack', creditPackageId: destination, bookingValidityVersion: 3, settingsVersion: 5,
      sessions: [{ classId: next.id, classVersion: 1, classTypeVersion: 2 }], idempotencyKey: expect.any(String),
    },
  }]);
});

it('selects every week on request and defaults a new enrolment to the package expiring first', async () => {
  vi.stubGlobal('fetch', vi.fn(async (path: string) => {
    if (path.includes('/classes?')) return response({ items: [first, skipped, third] });
    if (path.endsWith('/credit-history')) return response({ bookingValidity: history, items: [
      { id: 'later', purchaseId: 'later-purchase', available: 5, expiresOn: date(90) },
      { id: 'sooner', purchaseId: 'sooner-purchase', available: 5, expiresOn: date(60) },
      { id: 'open', purchaseId: 'open-purchase', available: 5, expiresOn: null },
    ] });
    return response({ items: [], settings });
  }));
  render(<EnrolmentForm {...props} onBooked={vi.fn()}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Enrol in weekly lessons' }));
  await user.selectOptions(await screen.findByLabelText('Weekly series'), 'series');
  expect(screen.queryAllByRole('checkbox', { checked: true })).toHaveLength(0);
  await user.click(screen.getByRole('button', { name: 'Select all weeks' }));
  expect(screen.getAllByRole('checkbox', { checked: true })).toHaveLength(3);
  expect(screen.queryByRole('button', { name: 'Select all weeks' })).not.toBeInTheDocument();
  const packages = screen.getByLabelText('Enrolment credit package');
  expect(packages).toHaveValue('sooner');
  expect(screen.getByRole('option', { name: `5 lessons left · use by ${date(60)} · #sooner` })).toBeInTheDocument();
  await user.selectOptions(packages, 'open');
  expect(packages).toHaveValue('open');
  expect(screen.getByRole('button', { name: 'Review enrolment' })).toBeEnabled();
});
