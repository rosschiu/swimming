import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import App from './App';
import AccountAccess from './AccountAccess';
import UserManagement from './UserManagement';
import Relationships from './Relationships';
import AuditViewer from './AuditViewer';

const t = (en: string) => en;
const amy = { id: 'amy', name: 'Amy Chan', level: 'Beginner', version: 1 };
const parent = { id: 'parent', name: 'Parent', email: 'parent@swim.test', role: 'parent', active: true, version: 1, invitationPending: false };
const coach = { ...parent, id: 'coach', name: 'Coach', role: 'coach' };
const student = { ...parent, id: 'student', name: 'Student', role: 'student' };
function response(body: unknown, status = 200) { return new Response(status === 204 ? null : JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }); }
beforeEach(() => { vi.unstubAllGlobals(); window.localStorage.clear(); window.history.replaceState(null, '', '/'); });

it('preserves invitation details on failure and requires a student profile for student access', async () => {
  const api = vi.fn(async (_path: string, options?: RequestInit) => options?.method === 'POST' ? response({ error: { message: 'Failed' } }, 500) : response({ items: [] }));
  vi.stubGlobal('fetch', api); const user = userEvent.setup();
  render(<UserManagement csrfToken="csrf" expired={() => false} t={t} students={[amy]}/>);
  await user.click(screen.getByRole('tab', { name: 'Invite' }));
  await user.type(screen.getByLabelText('Member name'), 'New Student');
  await user.type(screen.getByLabelText('Member email'), 'new@swim.test');
  await user.selectOptions(screen.getByLabelText('Role'), 'student');
  expect(screen.getByLabelText('Student profile (required)')).toBeRequired();
  await user.selectOptions(screen.getByLabelText('Student profile (required)'), 'amy');
  await user.click(screen.getByRole('button', { name: 'Send invitation' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Your entries are preserved');
  expect(screen.getByLabelText('Member name')).toHaveValue('New Student');
  expect(screen.getByLabelText('Student profile (required)')).toHaveValue('amy');
  expect(api).toHaveBeenCalledWith('/api/v1/invitations', expect.objectContaining({ body: JSON.stringify({ email: 'new@swim.test', name: 'New Student', role: 'student', studentId: 'amy' }), headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf' }) }));
});

it('offers only accounts matching the selected relationship and performs unlink', async () => {
  let linked = false;
  const api = vi.fn(async (path: string, options?: RequestInit) => {
    if (path.endsWith('/users')) return response({ items: [parent, coach, student] });
    if (options?.method === 'PUT') { linked = true; return response({ userId: 'coach', name: 'Coach', role: 'coach', kind: 'coach' }); }
    if (options?.method === 'DELETE') { linked = false; return response(null, 204); }
    return response({ items: linked ? [{ userId: 'coach', name: 'Coach', role: 'coach', kind: 'coach' }] : [] });
  });
  vi.stubGlobal('fetch', api); const user = userEvent.setup();
  render(<Relationships studentId="amy" csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('option', { name: /Parent ·/ });
  expect(screen.queryByRole('option', { name: /Coach ·/ })).not.toBeInTheDocument();
  await user.selectOptions(screen.getByLabelText('Relationship'), 'coach');
  expect(screen.queryByRole('option', { name: /Parent ·/ })).not.toBeInTheDocument();
  await user.selectOptions(screen.getByLabelText('Account to link'), 'coach');
  await user.click(screen.getByRole('button', { name: 'Link account' }));
  await user.click(await screen.findByRole('button', { name: 'Unlink Coach' }));
  expect(await screen.findByRole('status')).toHaveTextContent('Account unlinked');
  expect(api).toHaveBeenCalledWith('/api/v1/students/amy/relationships/coach', expect.objectContaining({ method: 'DELETE' }));
});

it('strips invitation tokens from the address and activates without automatic login', async () => {
  window.history.replaceState(null, '', '/accept-invitation?token=secret-invite');
  const api = vi.fn(async (path: string) => path.endsWith('/auth/me') ? response({}, 401) : response(null, 204));
  vi.stubGlobal('fetch', api); const user = userEvent.setup(); render(<App/>);
  expect(window.location.search).toBe('');
  await user.type(screen.getByLabelText('New password'), 'A-new-password-2026');
  await user.type(screen.getByLabelText('Confirm password'), 'A-new-password-2026');
  await user.click(screen.getByRole('button', { name: 'Save password' }));
  expect(await screen.findByRole('status')).toHaveTextContent('Sign in to continue');
  expect(api).toHaveBeenCalledWith('/api/v1/auth/accept-invitation', expect.objectContaining({ body: JSON.stringify({ token: 'secret-invite', password: 'A-new-password-2026' }) }));
  await user.click(screen.getByRole('button', { name: 'Back to sign in' }));
  expect(await screen.findByRole('button', { name: 'Sign in' })).toBeVisible();
});

it('rejects overly long UTF-8 passwords before sending a token', async () => {
  const api = vi.fn(); vi.stubGlobal('fetch', api); const user = userEvent.setup();
  render(<AccountAccess link={{ kind: 'reset', token: 'reset-token' }} t={t} onBack={() => {}}/>);
  await user.type(screen.getByLabelText('New password'), '泳'.repeat(25));
  await user.type(screen.getByLabelText('Confirm password'), '泳'.repeat(25));
  await user.click(screen.getByRole('button', { name: 'Save password' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('12–72 bytes');
  expect(api).not.toHaveBeenCalled();
});

it('keeps invalid-token feedback distinct from password validation', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ error: { code: 'invalid_token', message: 'Invalid token' } }, 400)));
  const user = userEvent.setup(); render(<AccountAccess link={{ kind: 'reset', token: 'expired-token' }} t={t} onBack={() => {}}/>);
  await user.type(screen.getByLabelText('New password'), 'A-new-password-2026');
  await user.type(screen.getByLabelText('Confirm password'), 'A-new-password-2026');
  await user.click(screen.getByRole('button', { name: 'Save password' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('expired or already been used');
  expect(screen.getByLabelText('New password')).toHaveValue('A-new-password-2026');
});

it('shows neutral password recovery confirmation without revealing account existence', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ message: 'Request accepted' }, 202)));
  const user = userEvent.setup(); render(<AccountAccess link={null} t={t} onBack={() => {}}/>);
  await user.type(screen.getByLabelText('Email'), 'unknown@swim.test');
  await user.click(screen.getByRole('button', { name: 'Send reset instructions' }));
  expect(await screen.findByRole('status')).toHaveTextContent('If an eligible account exists');
});

it('renders filtered audit details as text and not markup', async () => {
  const api = vi.fn(async () => response({ items: [{ id: 1, actorId: 'admin', action: 'student.updated', entityId: 'amy', details: { name: '<script>alert(1)</script>' }, createdAt: '2026-11-17T08:00:00Z' }] }));
  vi.stubGlobal('fetch', api); const user = userEvent.setup(); render(<AuditViewer csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('heading', { name: 'student.updated' });
  await user.type(screen.getByLabelText('Filter by record ID'), 'amy');
  await user.click(screen.getByRole('button', { name: 'Apply filter' }));
  await waitFor(() => expect(api).toHaveBeenLastCalledWith('/api/v1/audit?limit=25&entityId=amy', expect.anything()));
  expect(screen.getByText(/<script>alert\(1\)<\/script>/)).toBeInTheDocument();
  expect(document.querySelector('script')).toBeNull();
});

it('ends a protected view on unauthorised requests', async () => {
  const expired = vi.fn(() => true);
  vi.stubGlobal('fetch', vi.fn(async () => response({}, 401)));
  const view = render(<UserManagement csrfToken="csrf" expired={expired} t={t} students={[]}/>);
  await waitFor(() => expect(expired).toHaveBeenCalled());
  expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  view.unmount();
});


it('aborts pending protected reads when their account view unmounts', () => {
  const signals: AbortSignal[] = [];
  vi.stubGlobal('fetch', vi.fn((_path: string, options: RequestInit) => {
    signals.push(options.signal as AbortSignal);
    return new Promise<Response>(() => {});
  }));
  const view = render(<UserManagement csrfToken="csrf" expired={() => false} t={t} students={[]}/>);
  expect(signals).toHaveLength(2);
  view.unmount();
  expect(signals.every(signal => signal.aborted)).toBe(true);
});

it('allows reissuing a revoked invitation without offering another revoke', async () => {
  const invitation = { id: 'invite-1', userId: 'new', email: 'new@swim.test', role: 'parent', status: 'revoked', deliveryStatus: 'sent', expiresAt: '2026-12-01T00:00:00Z' };
  const api = vi.fn(async (path: string, options?: RequestInit) => {
    if (options?.method === 'POST') return response({ ...invitation, status: 'pending' });
    return response({ items: path.endsWith('/invitations') ? [invitation] : [] });
  });
  vi.stubGlobal('fetch', api); const user = userEvent.setup();
  render(<UserManagement csrfToken="csrf" expired={() => false} t={t} students={[]}/>);
  await user.click(screen.getByRole('tab', { name: 'Invitations' }));
  await user.click(await screen.findByRole('button', { name: 'Reissue invitation new@swim.test' }));
  expect(api).toHaveBeenCalledWith('/api/v1/invitations/invite-1/resend', expect.objectContaining({ method: 'POST' }));
  expect(screen.queryByRole('button', { name: 'Revoke invitation new@swim.test' })).not.toBeInTheDocument();
});

it('exhausts audit pages using the last numeric ID and clears paging on a new filter', async () => {
  const paths: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (path: string) => { paths.push(path); const q = new URL(path, 'http://test').searchParams; const after = Number(q.get('afterId') || 0); return response({ items: q.has('entityId') || after >= 125 ? [] : Array.from({ length: 25 }, (_, i) => ({ id: after + i + 1, actorId: 'admin', action: `Recorded ${after + i + 1}`, entityId: 'amy', details: {}, createdAt: '2030-01-01T00:00:00Z' })) }); }));
  const user = userEvent.setup(); render(<AuditViewer csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('heading', { name: 'Recorded 25' });
  for (let page = 1; page < 5; page++) { await user.click(screen.getByRole('button', { name: 'Load more' })); await screen.findByRole('heading', { name: `Recorded ${page * 25 + 25}` }); }
  expect(screen.getAllByRole('article')).toHaveLength(125);
  await user.click(screen.getByRole('button', { name: 'Load more' }));
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Load more' })).not.toBeInTheDocument());
  await user.type(screen.getByLabelText('Filter by record ID'), 'ben'); await user.click(screen.getByRole('button', { name: 'Apply filter' }));
  await screen.findByText('No recorded changes match this filter.');
  expect(new URL(paths.at(-1)!, 'http://test').searchParams.has('afterId')).toBe(false);
});
