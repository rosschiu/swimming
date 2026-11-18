import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import GrantCredits from './GrantCredits';

const props = { csrfToken: 'csrf', expired: () => false, t: (en: string) => en, studentId: 'ben' };
afterEach(() => vi.unstubAllGlobals());

it('requires credits, a future expiry and a reason, then adds complimentary credits once', async () => {
  const bodies: Record<string, unknown>[] = [];
  vi.stubGlobal('fetch', vi.fn(async (_path: string, options?: RequestInit) => {
    bodies.push(JSON.parse(String(options?.body)));
    return new Response(JSON.stringify({ id: 'g1', studentId: 'ben', packageId: 'p1', credits: 3, expiresOn: '2099-03-31', reason: 'Loyalty', actorId: 'admin', createdAt: '2099-01-01T00:00:00Z' }), { status: 201 });
  }));
  const onGranted = vi.fn();
  render(<GrantCredits {...props} onGranted={onGranted}/>);
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Grant extra credits' }));
  const add = screen.getByRole('button', { name: 'Add complimentary credits' });
  await user.clear(screen.getByLabelText('Credits'));
  await user.type(screen.getByLabelText('Credits'), '3');
  await user.type(screen.getByLabelText('Usable until (Hong Kong)'), '2099-03-31');
  expect(add).toBeDisabled();
  await user.type(screen.getByLabelText('Reason shown in the audit history'), 'Loyalty');
  await user.click(add);
  expect(await screen.findByText('3 complimentary credits added, usable until 2099-03-31.')).toBeVisible();
  expect(onGranted).toHaveBeenCalledOnce();
  expect(bodies).toEqual([{ credits: 3, expiresOn: '2099-03-31', reason: 'Loyalty', idempotencyKey: expect.any(String) }]);
});
