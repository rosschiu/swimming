import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import FixedEnrolmentSettings from './FixedEnrolmentSettings';

afterEach(() => vi.unstubAllGlobals());

it('saves both policies with the current version and requires a reload after a conflict', async () => {
  const defaults = { allocationPolicy: 'all_or_none', renewalPolicy: 'same_offer', version: 1 };
  const updated = { allocationPolicy: 'confirm_available_subset', renewalPolicy: 'any_paid_package' };
  const writes: unknown[] = [];
  const fetch = vi.fn(async (_path: string, request?: RequestInit) => {
    if (request?.method !== 'PATCH') return new Response(JSON.stringify({ ...defaults, version: writes.length + 1 }));
    writes.push(JSON.parse(String(request.body)));
    return writes.length === 1
      ? new Response(JSON.stringify({ error: { code: 'version_conflict' } }), { status: 409 })
      : new Response(JSON.stringify({ ...updated, version: 3 }));
  });
  vi.stubGlobal('fetch', fetch);
  render(<FixedEnrolmentSettings csrfToken="csrf" expired={() => false} t={en => en}/>);
  const user = userEvent.setup();
  const allocation = await screen.findByLabelText('When some weeks are unavailable');
  const renewal = screen.getByLabelText('Packages eligible for renewal');
  const save = screen.getByRole('button', { name: 'Save fixed enrolment settings' });
  expect(allocation).toHaveValue(defaults.allocationPolicy);
  expect(renewal).toHaveValue(defaults.renewalPolicy);
  await user.selectOptions(allocation, updated.allocationPolicy);
  await user.selectOptions(renewal, updated.renewalPolicy);
  await user.click(save);
  expect(await screen.findByRole('alert')).toHaveTextContent('Reload the settings before saving');
  expect(save).toBeDisabled();
  await user.click(screen.getByRole('button', { name: 'Reload fixed enrolment settings' }));
  expect(allocation).toHaveValue(updated.allocationPolicy);
  expect(renewal).toHaveValue(updated.renewalPolicy);
  await user.click(save);
  expect(await screen.findByText('Fixed enrolment settings saved.')).toBeVisible();
  expect(writes).toEqual([{ ...updated, version: 1 }, { ...updated, version: 2 }]);
  expect(fetch).toHaveBeenCalledWith('/api/v1/settings/fixed-enrolment', expect.objectContaining({
    method: 'PATCH', headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }),
  }));
});
