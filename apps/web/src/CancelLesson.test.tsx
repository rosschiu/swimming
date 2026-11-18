import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import CancelLesson from './CancelLesson';

const props = { csrfToken: 'csrf', expired: () => false, t: (en: string) => en, studentId: 'amy', bookingId: 'b1' };
const lesson = { id: 'c1', startsAt: '2030-01-07T02:00:00Z', endsAt: '2030-01-07T03:00:00Z', classTypeName: 'Water Confidence', venueName: 'Harbour Pool', coachName: 'Alex' };
const early = { bookingId: 'b1', lesson, timing: 'early', creditOutcome: 'retained', lateFrom: '2030-01-06T02:00:00Z', closesAt: lesson.startsAt };
const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });

afterEach(() => vi.unstubAllGlobals());

it('reviews the consequence, can keep the lesson, and reconfirms when the outcome changed', async () => {
  const bodies: Record<string, unknown>[] = [];
  let changed = true;
  vi.stubGlobal('fetch', vi.fn(async (_path: string, options?: RequestInit) => {
    if (options?.method !== 'POST') return response(early);
    bodies.push(JSON.parse(String(options.body)));
    if (changed) { changed = false; return response({ error: { code: 'cancellation_changed', details: { ...early, timing: 'late', creditOutcome: 'consumed' } } }, 409); }
    return response({ id: 'b1', status: 'cancelled' });
  }));
  const onCancelled = vi.fn();
  render(<CancelLesson {...props} onCancelled={onCancelled}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Cancel lesson' }));
  expect(await screen.findByText('This is an on-time cancellation. The lesson credit goes back to your package.')).toBeVisible();
  expect(screen.getByText('Cancellations after 2030-01-06 10:00 are late.')).toBeVisible();
  await user.click(screen.getByRole('button', { name: 'Keep lesson' }));
  expect(bodies).toHaveLength(0);
  await user.click(screen.getByRole('button', { name: 'Cancel lesson' }));
  await user.click(await screen.findByRole('button', { name: 'Confirm cancellation' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('The details changed since you looked');
  expect(screen.getByText('This is a late cancellation. The lesson credit is used under the school’s cancellation policy.')).toBeVisible();
  expect(onCancelled).not.toHaveBeenCalled();
  await user.click(screen.getByRole('button', { name: 'Confirm cancellation' }));
  await vi.waitFor(() => expect(onCancelled).toHaveBeenCalledWith({ id: 'b1', status: 'cancelled' }));
  expect(bodies[0]).toEqual({ classId: 'c1', timing: 'early', creditOutcome: 'retained', idempotencyKey: expect.any(String) });
  expect(bodies[1]).toEqual({ classId: 'c1', timing: 'late', creditOutcome: 'consumed', idempotencyKey: expect.any(String) });
  expect(bodies[1].idempotencyKey).not.toBe(bodies[0].idempotencyKey);
});

it('explains when a lesson can no longer be cancelled online', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ error: { code: 'cancellation_closed' } }, 409)));
  render(<CancelLesson {...props} onCancelled={vi.fn()}/>);
  await userEvent.setup().click(screen.getByRole('button', { name: 'Cancel lesson' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('can no longer be cancelled online');
});
