import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import SubstituteCoach from './SubstituteCoach';
import type { components } from './generated/api';

type ClassSession = components['schemas']['ClassSession'];
const lesson = { id: 'c1', version: 4, coachId: 'coach', coachName: 'Alex', startsAt: '2099-01-07T02:00:00Z', endsAt: '2099-01-07T03:00:00Z' } as ClassSession;
const coaches = [{ id: 'coach', name: 'Alex', active: true }, { id: 'coach2', name: 'Chloe', active: true }, { id: 'gone', name: 'Former', active: false }];
const props = { csrfToken: 'csrf', expired: () => false, t: (en: string) => en };

afterEach(() => vi.unstubAllGlobals());

it('offers only other active coaches, explains a clash, then records the substitute', async () => {
  const bodies: Record<string, unknown>[] = [];
  vi.stubGlobal('fetch', vi.fn(async (_path: string, options?: RequestInit) => {
    bodies.push(JSON.parse(String(options?.body)));
    if (bodies.length === 1) return new Response(JSON.stringify({ error: { code: 'scheduling_conflict' } }), { status: 409 });
    return new Response(JSON.stringify({ id: 's1', lesson: { ...lesson, coachId: 'coach2', coachName: 'Chloe', version: 5 }, fromCoachId: 'coach', toCoachId: 'coach2', reason: 'Unwell', substitutedAt: '2099-01-01T00:00:00Z' }));
  }));
  const onSubstituted = vi.fn();
  render(<SubstituteCoach lesson={lesson} coaches={coaches} {...props} onSubstituted={onSubstituted} onClose={vi.fn()}/>);
  const user = userEvent.setup();
  expect(screen.getAllByRole('option').map(option => option.textContent)).toEqual(['Choose a coach', 'Chloe']);
  const confirm = screen.getByRole('button', { name: 'Confirm substitute' });
  await user.selectOptions(screen.getByLabelText('New coach'), 'coach2');
  expect(confirm).toBeDisabled();
  await user.type(screen.getByLabelText('Reason shown in the audit history'), 'Unwell');
  await user.click(confirm);
  expect(await screen.findByRole('alert')).toHaveTextContent('overlapping lesson or not enough travel time');
  await user.click(confirm);
  await vi.waitFor(() => expect(onSubstituted).toHaveBeenCalledWith(expect.objectContaining({ coachId: 'coach2', version: 5 })));
  expect(bodies[0]).toEqual({ coachId: 'coach2', reason: 'Unwell', version: 4, idempotencyKey: expect.any(String) });
  expect(bodies[1]).toEqual(bodies[0]);
});
