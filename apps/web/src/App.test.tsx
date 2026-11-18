import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import App from './App';

const amy = { id: 'amy', name: 'Amy Chan', level: 'Beginner', version: 1 };
function response(body: unknown, status = 200) { return new Response(status === 204 ? null : JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }); }
function mockAPI(role: 'parent' | 'coach' | 'student' | 'admin', saveStatus = 200) {
  const fetchMock = vi.fn(async (input: string | URL | Request, options?: RequestInit) => {
    const path = String(input);
    if (path.endsWith('/auth/me')) return response({ user: { id: role, name: 'Alex', role }, csrfToken: 'csrf-value' });
    if (path.endsWith('/auth/logout')) return response(null, 204);
    if (options?.method === 'POST' || options?.method === 'PATCH') return response(saveStatus === 200 ? amy : { error: { code: 'error', message: 'Error' } }, saveStatus);
    if (path.endsWith('/admin/action-queue')) return response({ purchasesAwaitingExpiry: 0, failedEmailDeliveries: 0, pendingLessonChanges: 0 });
    if (path.endsWith('/students/amy')) return response(amy);
    if (path.endsWith('/students')) return response({ items: [amy] });
    throw new Error(`Unexpected request ${path}`);
  });
  vi.stubGlobal('fetch', fetchMock); return fetchMock;
}
beforeEach(() => { window.history.replaceState(null, '', '/'); window.localStorage.clear(); vi.unstubAllGlobals(); });

afterEach(() => vi.unstubAllEnvs());

it('keeps demo accounts hidden by default', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({}, 401)));
  render(<App />);
  await screen.findByRole('button', { name: 'Sign in' });
  expect(screen.queryByRole('group', { name: 'Demo accounts' })).not.toBeInTheDocument();
});

it('fills demo accounts without submitting and sends edited values through normal sign-in', async () => {
  vi.stubEnv('VITE_TEST_DEMO_LOGIN', 'true');
  const api = vi.fn(async (_input: string, _options?: RequestInit) => response({}, 401));
  vi.stubGlobal('fetch', api);
  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole('group', { name: 'Demo accounts' });
  for (const [label, role] of [['Administrator', 'admin'], ['Coach', 'coach'], ['Guardian', 'parent'], ['Student', 'student']]) {
    await user.click(screen.getByRole('button', { name: label }));
    expect(screen.getByLabelText('Email')).toHaveValue(`${role}@swim.test`);
    expect(screen.getByLabelText('Password')).not.toHaveValue('');
  }
  expect(api).toHaveBeenCalledTimes(1);
  await user.clear(screen.getByLabelText('Password'));
  await user.type(screen.getByLabelText('Password'), 'edited-password');
  await user.click(screen.getByRole('button', { name: 'Sign in' }));
  expect(api).toHaveBeenLastCalledWith('/api/v1/auth/login', expect.objectContaining({
    method: 'POST',
    body: JSON.stringify({ email: 'student@swim.test', password: 'edited-password' }),
  }));
});

it('reports sign-in failure and keeps credentials available for correction', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ error: { message: 'No access' } }, 401)));
  const user = userEvent.setup(); render(<App />);
  await user.type(await screen.findByLabelText('Email'), 'parent@example.com');
  await user.type(screen.getByLabelText('Password'), 'wrong-password');
  await user.click(screen.getByRole('button', { name: 'Sign in' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Unable to sign in');
  expect(screen.getByLabelText('Email')).toHaveValue('parent@example.com');
});

it.each(['parent', 'coach', 'student'] as const)('shows scoped read-only navigation for %s', async role => {
  mockAPI(role); const user = userEvent.setup(); render(<App />);
  await user.click(await screen.findByRole('link', { name: /Amy Chan/ }));
  expect(await screen.findByRole('heading', { name: 'Amy Chan' })).toBeVisible();
  expect(screen.queryByRole('button', { name: 'Add student' })).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Edit student' })).not.toBeInTheDocument();
});

it('keeps administrator form input after failed save and sends CSRF token', async () => {
  const api = mockAPI('admin', 500); const user = userEvent.setup(); render(<App />);
  await user.click(await screen.findByRole('button', { name: 'Add student' }));
  await user.type(screen.getByLabelText('Student name'), 'Chris Wong');
  await user.type(screen.getByLabelText('Swimming level'), 'Intermediate');
  await user.click(screen.getByRole('button', { name: 'Save student' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Your entries are preserved');
  expect(screen.getByLabelText('Student name')).toHaveValue('Chris Wong');
  expect(screen.getByLabelText('Swimming level')).toHaveValue('Intermediate');
  expect(api).toHaveBeenCalledWith('/api/v1/students', expect.objectContaining({ method: 'POST', headers: expect.objectContaining({ 'X-CSRF-Token': 'csrf-value' }) }));
});

it('explains stale edits without discarding entered changes', async () => {
  mockAPI('admin', 409); const user = userEvent.setup(); render(<App />);
  await user.click(await screen.findByRole('link', { name: /Amy Chan/ }));
  await user.click(await screen.findByRole('button', { name: 'Edit student' }));
  await user.clear(screen.getByLabelText('Swimming level'));
  await user.type(screen.getByLabelText('Swimming level'), 'Advanced');
  await user.click(screen.getByRole('button', { name: 'Save student' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Someone updated this record');
  expect(screen.getByLabelText('Swimming level')).toHaveValue('Advanced');
});

it('switches language and clears protected records on logout', async () => {
  mockAPI('parent'); const user = userEvent.setup(); render(<App />);
  await screen.findByRole('link', { name: /Amy Chan/ });
  await user.click(screen.getByRole('button', { name: '繁體中文' }));
  expect(document.documentElement.lang).toBe('zh-Hant-HK');
  await user.click(screen.getByRole('button', { name: '登出' }));
  await waitFor(() => expect(screen.getByRole('button', { name: '登入' })).toBeVisible());
  expect(screen.queryByText('Amy Chan')).not.toBeInTheDocument();
});

it('clears a private edit after expiry before a different account signs in', async () => {
  let expired = false;
  let role = 'admin';
  vi.stubGlobal('fetch', vi.fn(async (input: string, options?: RequestInit) => {
    if (input.endsWith('/auth/login')) { role = 'parent'; expired = false; return response({ user: { id: 'parent', name: 'Parent', role }, csrfToken: 'new-token' }); }
    if (expired) return response({ error: { message: 'Expired' } }, 401);
    if (input.endsWith('/auth/me')) return response({ user: { id: 'admin', name: 'Admin', role }, csrfToken: 'token' });
    if (input.endsWith('/students/amy')) return role === 'admin' ? response(amy) : response({}, 404);
    if (input.endsWith('/students')) return response({ items: role === 'admin' ? [amy] : [] });
    throw new Error(`Unexpected request ${input} ${options?.method}`);
  }));
  const user = userEvent.setup(); render(<App />);
  await user.click(await screen.findByRole('link', { name: /Amy Chan/ }));
  await user.click(await screen.findByRole('button', { name: 'Edit student' }));
  await user.type(screen.getByLabelText('Student name'), ' Private');
  expired = true;
  await user.click(screen.getByRole('button', { name: 'Save student' }));
  await user.type(await screen.findByLabelText('Email'), 'parent@example.com');
  await user.type(screen.getByLabelText('Password'), 'password');
  await user.click(screen.getByRole('button', { name: 'Sign in' }));
  await screen.findByRole('heading', { name: 'Your swimmers' });
  expect(screen.queryByLabelText('Student name')).not.toBeInTheDocument();
  expect(screen.queryByText('Amy Chan')).not.toBeInTheDocument();
  expect(window.location.pathname).toBe('/students/amy');
});

it('searches all 500 registered students by name and level including the last record', async () => {
  const students = Array.from({ length: 500 }, (_, i) => ({ id: `scale-${i}`, name: `Swimmer ${String(i).padStart(3, '0')}`, level: i === 499 ? 'Advanced' : 'Beginner', version: 1 }));
  vi.stubGlobal('fetch', vi.fn(async (path: string) => { if (path.endsWith('/auth/me')) return response({ user: { id: 'admin', name: 'Admin', role: 'admin' }, csrfToken: 'csrf' }); if (path.endsWith('/students')) return response({ items: students }); if (path.includes('/notifications?')) return response({ items: [], nextCursor: null, unreadCount: 0 }); throw new Error(`Unexpected ${path}`); }));
  const user = userEvent.setup(); render(<App />);
  await screen.findByRole('link', { name: /Swimmer 499/ });
  expect(screen.getAllByRole('link', { name: /Swimmer \d/ })).toHaveLength(500);
  const search = screen.getByLabelText('Search students');
  await user.type(search, '499');
  expect(screen.getAllByRole('link', { name: /Swimmer \d/ })).toHaveLength(1);
  expect(screen.getByRole('link', { name: /Swimmer 499/ })).toBeVisible();
  await user.clear(search); await user.type(search, 'advanced');
  expect(screen.getAllByRole('link', { name: /Swimmer \d/ })).toHaveLength(1);
  await user.clear(search); await user.type(search, 'no such swimmer');
  expect(screen.getByText('No students to display')).toBeVisible();
});
