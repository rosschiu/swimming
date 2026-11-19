import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import ImportStudents from './ImportStudents';

const props = { csrfToken: 'csrf', expired: () => false, t: (en: string) => en };
afterEach(() => vi.unstubAllGlobals());
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
const summary = { students: 2, guardiansCreated: 1, guardiansLinked: 0, packages: 1, credits: 8 };
const row = { row: 2, studentName: 'Mei', level: 'Beginner', guardians: [], credits: 8, expiresOn: '2099-01-01', errors: [] as string[] };

it('shows row problems and offers no import until the sheet is fixed', async () => {
  const fetch = vi.fn(async () => json({ fileSha256: 'a'.repeat(64), valid: false, errors: [], ignoredColumns: ['Old ID'], rows: [{ ...row, errors: ['Remaining credits need an expiry date (YYYY-MM-DD).'] }], summary }));
  vi.stubGlobal('fetch', fetch);
  render(<ImportStudents {...props} onImported={() => {}}/>);
  const user = userEvent.setup();
  await user.upload(screen.getByLabelText('Excel file (.xlsx)'), new File(['x'], 'school.xlsx'));
  expect(await screen.findByText('Row 2 · Mei: Remaining credits need an expiry date (YYYY-MM-DD).')).toBeVisible();
  expect(screen.getByText('Ignored columns: Old ID')).toBeVisible();
  expect(screen.queryByRole('button', { name: /Import/ })).not.toBeInTheDocument();
  expect(fetch).toHaveBeenCalledWith('/api/v1/imports/preview', expect.objectContaining({ method: 'POST', headers: expect.objectContaining({ 'Content-Type': 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', 'X-CSRF-Token': 'csrf' }) }));
});

it('imports the checked file with its hash and reports the result', async () => {
  const hash = 'b'.repeat(64);
  const fetch = vi.fn(async (path: string) => path.startsWith('/api/v1/imports/preview')
    ? json({ fileSha256: hash, valid: true, errors: [], ignoredColumns: [], rows: [row, { ...row, row: 3, studentName: 'Kit', credits: 0 }], summary })
    : json({ id: 'i1', fileSha256: hash, ...summary, createdAt: '2099-01-01T00:00:00Z' }, 201));
  vi.stubGlobal('fetch', fetch);
  const onImported = vi.fn();
  render(<ImportStudents {...props} onImported={onImported}/>);
  const user = userEvent.setup();
  await user.upload(screen.getByLabelText('Excel file (.xlsx)'), new File(['x'], 'school.xlsx'));
  await user.click(await screen.findByRole('button', { name: 'Import 2 swimmers' }));
  expect(await screen.findByText('Imported 2 swimmers, 1 new and 0 existing guardians, and 8 credits in 1 migrated balances.')).toBeVisible();
  expect(fetch).toHaveBeenLastCalledWith(`/api/v1/imports?fileSha256=${hash}`, expect.objectContaining({ method: 'POST' }));
  expect(onImported).toHaveBeenCalledOnce();
});
