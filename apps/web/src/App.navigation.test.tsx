import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import App from './App';

type Role = 'admin' | 'coach' | 'parent' | 'student';
const amy = { id: 'amy', name: 'Amy Chan', level: 'Beginner', version: 1 };
const ben = { ...amy, id: 'ben', name: 'Ben Lee' };
const session = (role: Role) => ({ user: { id: role, name: role, role }, csrfToken: `csrf-${role}` });
const response = (body: unknown, status = 200) => new Response(status === 204 ? null : JSON.stringify(body), { status });
function deferred() { let resolve!: (value: Response) => void; const promise = new Promise<Response>(r => { resolve = r; }); return { promise, resolve }; }
type Override = (path: string, options?: RequestInit) => Response | Promise<Response> | undefined;
function api(role: Role, override?: Override) {
 const mock = vi.fn(async (input: string | URL | Request, options?: RequestInit) => {
  const path = String(input); const custom = override?.(path, options); if (custom !== undefined) return custom;
  if (path.endsWith('/auth/me') || path.endsWith('/auth/login')) return response(session(role));
  if (path.endsWith('/auth/logout')) return response(null, 204);
  if (path.endsWith('/credit-history')) return response({ items: [], recoveries: [] });
  if (path.endsWith('/students/amy')) return response(amy);
  if (path.endsWith('/students/ben')) return response(ben);
  if (path.endsWith('/students')) return response({ items: [amy, ben] });
  if (path.endsWith('/settings/credits')) return response({ policy: {expiryEnabled:true,carryForwardEnabled:true,earlyCancellationRetainsCredit:true,lateCancellationRetainsCredit:true,absenceRetainsCredit:true,illnessRetainsCredit:true}, version:1 });
  if (path.endsWith('/settings/scheduling')) return response({ travelGapMinutes: 30, version: 1 });
  if (path.endsWith('/settings/availability')) return response({ lockHours: 24, version: 1 });
  if (path.endsWith('/notification-preferences')) return response({ emailEnabled: false, emailAddress: 'parent@swim.test', emailDeliveryAvailable: false });
  return response({ items: [], nextCursor: null, unreadCount: 0 });
 }); vi.stubGlobal('fetch', mock); return mock;
}
function at(path: string) { window.history.replaceState(null, '', path); }
function pop(path: string, state: unknown = null) { act(() => { window.history.pushState(state, '', path); window.dispatchEvent(new PopStateEvent('popstate', { state })); }); }
beforeEach(() => { vi.unstubAllGlobals(); window.localStorage.clear(); at('/students'); });

const routes: [string, Role[], (role: Role) => string][] = [
 ['/students', ['admin','coach','parent','student'], r => r === 'parent' ? 'Your swimmers' : r === 'student' ? 'Your profile' : 'Your students'],
 ['/students/amy', ['admin','coach','parent','student'], () => 'Amy Chan'],
 ['/schedule', ['admin','coach','parent','student'], r => r === 'admin' ? 'Class schedule' : r === 'coach' ? 'Your teaching schedule' : 'Class catalogue'],
 ['/availability', ['admin','coach'], () => 'Available time slots'],
 ['/packages', ['admin','parent'], () => 'Packages and payments'],
 ['/accounts', ['admin'], () => 'Accounts & invitations'], ['/setup', ['admin'], () => 'School setup'], ['/audit', ['admin'], () => 'Audit history'],
 ['/notification-deliveries', ['admin'], () => 'Notification deliveries'], ['/notifications', ['parent','student'], () => 'Notifications'],
];
for (const [path, roles, heading] of routes) for (const role of roles) {
 it(`${role} restores ${path} on direct entry, remount, and sign-in`, async () => {
  at(path); api(role); let app = render(<App />);
  await screen.findByRole('heading', { name: heading(role) }); app.unmount();
  app = render(<App />); await screen.findByRole('heading', { name: heading(role) }); app.unmount();
  api(role, p => p.endsWith('/auth/me') ? response({}, 401) : undefined); render(<App />); const user = userEvent.setup();
  await user.type(await screen.findByLabelText('Email'), `${role}@swim.test`); await user.type(screen.getByLabelText('Password'), 'password'); await user.click(screen.getByRole('button', { name: 'Sign in' }));
  await screen.findByRole('heading', { name: heading(role) }); expect(window.location.pathname).toBe(path);
 });
}
for (const [path, roles] of routes) for (const role of ['admin','coach','parent','student'] as const) if (!roles.includes(role)) {
 it(`${role} cannot mount or request ${path}`, async () => {
  at(path); const mock = api(role); render(<App />); await screen.findByRole('heading', { name: /unavailable/i });
  const paths = mock.mock.calls.map(([p]) => String(p));
  expect(paths.every(p => p.endsWith('/auth/me') || p.endsWith('/students') || p.endsWith('/notifications?limit=1'))).toBe(true);
  expect(window.location.pathname).toBe(path);
 });
}
it.each(['/unknown','/students/%E0%A4%A','/students/amy/extra','/students/amy%2Fnotes','/students/amy%5Cnotes'])('rejects malformed/unknown route %s without profile fetch', async path => {
 at(path); const mock = api('admin'); render(<App />); await screen.findByRole('heading', { name: /unavailable/i });
 expect(mock.mock.calls.some(([p]) => String(p).includes('/students/'))).toBe(false);
});
it('uses pathname over forged history state and preserves native links and focus', async () => {
 api('admin'); render(<App />); const user = userEvent.setup(); const link = await screen.findByRole('link', { name: /Amy Chan/ }); expect(link).toHaveAttribute('href', '/students/amy');
 // Observe React's handling before stopping jsdom's unsupported native document navigation.
 for (const modifier of ['ctrlKey','metaKey','shiftKey','altKey']) { const e = new MouseEvent('click', { bubbles: true, cancelable: true, [modifier]: true }); let intercepted = true; document.addEventListener('click', event => { intercepted = event.defaultPrevented; event.preventDefault(); }, { once: true }); act(() => { link.dispatchEvent(e); }); expect(intercepted).toBe(false); expect(window.location.pathname).toBe('/students'); }
 link.focus(); await user.keyboard('{Enter}'); await screen.findByRole('heading', { name: 'Amy Chan' }); expect(window.location.pathname).toBe('/students/amy');
 await user.click(screen.getByRole('link', { name: 'Schedule' })); await screen.findByRole('heading', { name: 'Class schedule' });
 expect(document.activeElement === document.getElementById('main') || document.activeElement?.textContent === 'Class schedule').toBe(true);
 pop('/students/ben', { view: 'audit' }); await screen.findByRole('heading', { name: 'Ben Lee' }); expect(screen.queryByRole('heading', { name: 'Audit history' })).not.toBeInTheDocument();
 expect(JSON.stringify(window.history.state)).not.toContain('Amy Chan');
});
it.each([403,404])('does not reveal a profile after scoped endpoint returns %i', async status => {
 at('/students/amy'); api('parent', p => p.endsWith('/students/amy') ? response({}, status) : undefined); render(<App />);
 await screen.findByRole('heading', { name: /unavailable/i }); expect(screen.queryByRole('heading', { name: 'Amy Chan' })).not.toBeInTheDocument();
});
it('retries a transient profile failure and translates unavailable routes', async () => {
 at('/students/amy'); let fail = true; api('parent', p => p.endsWith('/students/amy') && fail ? response({}, 500) : undefined); render(<App />); const user = userEvent.setup();
 await screen.findByRole('button', { name: /Retry/i }); fail = false; await user.click(screen.getByRole('button', { name: /Retry/i })); await screen.findByRole('heading', { name: 'Amy Chan' });
 pop('/unknown'); await screen.findByRole('heading', { name: /unavailable/i }); await user.click(screen.getByRole('button', { name: '繁體中文' })); expect(document.documentElement.lang).toBe('zh-Hant-HK'); expect(screen.queryByRole('heading', { name: /unavailable/i })).not.toBeInTheDocument();
});
it.each([200,401,500])('ignores obsolete profile response %i after moving to another ID', async status => {
 const old = deferred(); at('/students/amy'); const mock = api('admin', p => p.endsWith('/students/amy') ? old.promise : undefined); render(<App />);
 await waitFor(() => expect(mock).toHaveBeenCalledWith('/api/v1/students/amy', expect.anything())); expect(screen.getByText('Loading student profile…')).toBeVisible(); expect(screen.queryByRole('button', { name: 'Add student' })).not.toBeInTheDocument(); pop('/students/ben'); await screen.findByRole('heading', { name: 'Ben Lee' });
 await act(async () => { old.resolve(response(amy, status)); }); expect(screen.getByRole('heading', { name: 'Ben Lee' })).toBeVisible(); expect(screen.queryByRole('heading', { name: 'Amy Chan' })).not.toBeInTheDocument(); expect(screen.getByRole('button', { name: 'Sign out' })).toBeEnabled();
});
it.each([200,401,500])('ignores save completion %i after navigation without claiming server rollback', async status => {
 const old = deferred(); at('/students/amy'); const mock = api('admin', (p,o) => p.endsWith('/students/amy') && o?.method === 'PATCH' ? old.promise : undefined); render(<App />); const user = userEvent.setup();
 await user.click(await screen.findByRole('button', { name: 'Edit student' })); await user.click(screen.getByRole('button', { name: 'Save student' }));
 await waitFor(() => expect(mock.mock.calls.some(([,o]) => o?.method === 'PATCH')).toBe(true)); await user.click(screen.getByRole('link', { name: 'Schedule' })); await screen.findByRole('heading', { name: 'Class schedule' });
 await act(async () => { old.resolve(response(amy, status)); }); expect(window.location.pathname).toBe('/schedule'); expect(screen.getByRole('heading', { name: 'Class schedule' })).toBeVisible(); expect(screen.queryByRole('heading', { name: 'Amy Chan' })).not.toBeInTheDocument(); expect(screen.getByRole('button', { name: 'Sign out' })).toBeEnabled();
});
it('does not let a previous account profile response or 401 affect the replacement session', async () => {
 const old = deferred(); let first = true; at('/students/amy'); api('parent', p => { if (p.endsWith('/students/amy') && first) return old.promise; if (p.endsWith('/auth/login')) return response({ ...session('parent'), user: { id: 'other-parent', name: 'Other Parent', role: 'parent' } }); if (p.endsWith('/students') && !first) return response({ items: [ben] }); return undefined; }); render(<App />); const user = userEvent.setup();
 await screen.findByRole('button', { name: 'Sign out' }); pop('/students'); await user.click(screen.getByRole('button', { name: 'Sign out' })); first = false;
 await user.type(await screen.findByLabelText('Email'), 'other@swim.test'); await user.type(screen.getByLabelText('Password'), 'password'); await user.click(screen.getByRole('button', { name: 'Sign in' })); await screen.findByRole('link', { name: /Ben Lee/ });
 await act(async () => { old.resolve(response({},401)); }); expect(screen.getByRole('button', { name: 'Sign out' })).toBeVisible(); expect(window.location.pathname).toBe('/students');
});
it('creation selects the saved URL and cancellation keeps the current profile URL', async () => {
 api('admin', (p,o) => p.endsWith('/students') && o?.method === 'POST' ? response(ben,201) : undefined); render(<App />); const user = userEvent.setup();
 await user.click(await screen.findByRole('button',{name:'Add student'})); await user.type(screen.getByLabelText('Student name'),'Ben Lee'); await user.type(screen.getByLabelText('Swimming level'),'Beginner'); await user.click(screen.getByRole('button',{name:'Save student'}));
 await screen.findByRole('heading',{name:'Ben Lee'}); expect(window.location.pathname).toBe('/students/ben'); await user.click(screen.getByRole('button',{name:'Edit student'})); await user.click(screen.getByRole('button',{name:'Cancel'})); expect(window.location.pathname).toBe('/students/ben'); expect(screen.getByRole('heading',{name:'Ben Lee'})).toBeVisible();
});
it.each(['/accept-invitation','/reset-password'])('preserves access-link precedence and token stripping at %s', async path => {
 at(`${path}?token=synthetic-token`); const mock=api('admin', (p,o) => p.includes('/auth/') && o?.method==='POST' ? response({},200) : undefined); render(<App />); const user=userEvent.setup();
 await user.type(await screen.findByLabelText('New password'),'A-long-password-2026!'); await user.type(screen.getByLabelText('Confirm password'),'A-long-password-2026!'); expect(window.location.search).toBe(''); expect(JSON.stringify(window.history.state)).not.toContain('synthetic-token');
 await user.click(screen.getByRole('button',{name:'Save password'})); await screen.findByText('Your password has been saved. Sign in to continue.'); expect(mock.mock.calls.some(([,o])=>o?.body && JSON.parse(String(o.body)).token==='synthetic-token')).toBe(true);
 await user.click(screen.getByRole('button',{name:'Back to sign in'})); await screen.findByRole('button',{name:'Sign in'});
});

it.each(['/accept-invitation','/reset-password'])('keeps missing-token access UI at %s', async path => { at(path); api('admin'); render(<App />); await screen.findByText('This link is missing its access token. Please use the complete link sent by your school.'); expect(screen.queryByRole('heading',{name:/unavailable/i})).not.toBeInTheDocument(); });
it('an older save cannot clear the busy state of a later profile save', async () => {
 const old=deferred(), current=deferred(); at('/students/amy'); const mock=api('admin',(p,o)=>o?.method==='PATCH' ? p.endsWith('/amy') ? old.promise : current.promise : undefined); render(<App />); const user=userEvent.setup();
 await user.click(await screen.findByRole('button',{name:'Edit student'})); await user.click(screen.getByRole('button',{name:'Save student'}));
 await waitFor(()=>expect(mock.mock.calls.some(([,o])=>o?.method==='PATCH')).toBe(true)); pop('/students/ben');
 await user.click(await screen.findByRole('button',{name:'Edit student'})); await user.click(screen.getByRole('button',{name:'Save student'}));
 await waitFor(()=>expect(mock.mock.calls.filter(([,o])=>o?.method==='PATCH')).toHaveLength(2));
 await act(async()=>{old.resolve(response(amy));}); expect(screen.getByRole('button',{name:'Saving…'})).toBeDisabled(); expect(screen.getByLabelText('Student name')).toHaveValue('Ben Lee');
 await act(async()=>{current.resolve(response({...ben,version:2}));}); await screen.findByRole('heading',{name:'Ben Lee'}); expect(window.location.pathname).toBe('/students/ben');
});
it('does not steal focus on directory refresh or validation errors', async()=>{
 api('admin');render(<App/>);const user=userEvent.setup();await screen.findByRole('link',{name:/Amy Chan/});
 const refresh=screen.getByRole('button',{name:'Refresh'});await user.click(refresh);await screen.findByRole('link',{name:/Amy Chan/});expect(refresh).toHaveFocus();
 await user.click(screen.getByRole('button',{name:'Add student'})); const name=screen.getByLabelText('Student name');await user.type(name,'   ');await user.type(screen.getByLabelText('Swimming level'),'Beginner');const save=screen.getByRole('button',{name:'Save student'});await user.click(save);await screen.findByRole('alert');expect(save).toHaveFocus();
});
it('encodes decoded profile IDs as one API path segment',async()=>{
 at('/students/a%3Fb%23c');const mock=api('admin',p=>p==='/api/v1/students/a%3Fb%23c'?response({},404):undefined);render(<App/>);
 await screen.findByRole('heading',{name:'Student profile unavailable'});expect(mock).toHaveBeenCalledWith('/api/v1/students/a%3Fb%23c',expect.anything());
 expect(mock.mock.calls.some(([p])=>String(p).includes('/students/a?b'))).toBe(false);
});
it('cancel after a version conflict refetches the scoped profile without changing URL',async()=>{
 let changed=false;at('/students/amy');api('admin',(p,o)=>{if(p.endsWith('/students/amy')&&o?.method==='PATCH'){changed=true;return response({},409);}if(p.endsWith('/students/amy')&&changed)return response({...amy,level:'Advanced',version:2});return undefined;});render(<App/>);const user=userEvent.setup();
 await user.click(await screen.findByRole('button',{name:'Edit student'}));await user.click(screen.getByRole('button',{name:'Save student'}));await screen.findByRole('alert');await user.click(screen.getByRole('button',{name:'Cancel'}));
 await screen.findByText('Advanced');expect(window.location.pathname).toBe('/students/amy');expect(screen.queryByRole('button',{name:'Add student'})).not.toBeInTheDocument();
});

it.each(['admin', 'parent', 'student', 'coach'] as const)('loads read-only lessons only for an eligible %s profile', async role => {
  at('/students/amy');
  const booking = {
    id: 'b1', studentId: amy.id, createdAt: '2026-11-17T00:00:00Z', status: 'reserved', class: {
      id: 'class1', classTypeVersion: 1, classTypeId: 'type', coachId: 'alex', venueId: 'pool',
      capacity: 6, version: 1, region: 'hk-island', level: 'Beginner', seriesId: null,
      classTypeName: 'Booked youth lesson', startsAt: '2026-11-18T15:30:00Z', endsAt: '2026-11-18T16:30:00Z',
      venueName: 'Harbour Pool', coachName: 'Coach Alex',
    },
  };
  const mock = api(role, path => path.includes('/bookings?') ? response({ items: [booking] }) : undefined);
  render(<App />);
  await screen.findByRole('heading', { name: 'Amy Chan' });
  if (role === 'coach') {
    expect(screen.queryByRole('heading', { name: 'Booked lessons' })).not.toBeInTheDocument();
    expect(mock.mock.calls.some(([path]) => String(path).includes('/bookings'))).toBe(false);
    return;
  }
  expect(await screen.findByText('Booked youth lesson')).toBeVisible();
  if (role === 'student') expect(screen.queryByRole('button', { name: 'Book a lesson' })).not.toBeInTheDocument();
  else expect(screen.getByRole('button', { name: 'Book a lesson' })).toBeVisible();
  expect(screen.getByText('2026-11-18 23:30')).toBeVisible();
  expect(screen.getByText('2026-11-19 00:30')).toBeVisible();
  expect(mock).toHaveBeenCalledWith(expect.stringMatching(/\/students\/amy\/bookings\?from=\d{4}-\d{2}-\d{2}&to=\d{4}-\d{2}-\d{2}$/), expect.objectContaining({ signal: expect.any(AbortSignal) }));
  expect(mock.mock.calls.filter(([path]) => String(path).includes('/bookings')).every(([, options]) => !options?.method)).toBe(true);
});

it('discards a pending child read after navigation and lets the guardian retry the current child', async () => {
  at('/students/amy');
  const old = deferred();
  let unavailable = true;
  const mock = api('parent', path => {
    if (path.includes('/amy/bookings?')) return old.promise;
    if (path.includes('/ben/bookings?')) return unavailable ? response({}, 500) : response({ items: [] });
  });
  render(<App />);
  await screen.findByText('Loading booked lessons…');
  pop('/students/ben');
  expect(await screen.findByText('Could not load booked lessons. Use the button above to retry.')).toHaveAttribute('role', 'alert');
  unavailable = false;
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Load booked lessons' }));
  expect(await screen.findByText('No booked lessons on these dates.')).toBeVisible();
  await act(async () => old.resolve(response({}, 401)));
  expect(screen.getByRole('heading', { name: 'Ben Lee' })).toBeVisible();
  expect(screen.getByRole('button', { name: 'Sign out' })).toBeVisible();
  const oldSignal = mock.mock.calls.find(([path]) => String(path).includes('/amy/bookings?'))?.[1]?.signal;
  expect(oldSignal?.aborted).toBe(true);
});

it.each(['parent', 'admin'] as const)('books with explicit confirmation and refreshes scoped reads for %s', async role => {
  at('/students/amy');
  const startsAt = new Date(Date.now() + 3 * 86400000).toISOString();
  const endsAt = new Date(Date.parse(startsAt) + 3600000).toISOString();
  const lesson = {
    id: 'c1', classTypeId: 't1', classTypeVersion: 3, version: 2, classTypeName: 'Lesson to reserve',
    level: role === 'admin' ? 'Advanced' : amy.level, startsAt, endsAt, venueName: 'Harbour Pool', coachName: 'Alex',
  };
  const bookedLesson = { id: 'b1', status: 'reserved', class: lesson };
  const pack = { id: 'pack-recovered', expiresOn: null, available: 1, reserved: 0, consumed: 0, expired: 0, movements: [] };
  const writes: Record<string, unknown>[] = [];
  let booked = false;
  const mock = api(role, (path, options) => {
    if (path.includes('/classes?')) return response({ items: [lesson, { ...lesson, id: 'c2', level: 'Expert' }] });
    if (path.endsWith('/credit-history')) return response({ items: [{ ...pack, available: booked ? 0 : 1, reserved: booked ? 1 : 0 }], recoveries: [] });
    if (path.includes('/bookings?')) return response({ items: booked ? [bookedLesson] : [] });
    if (path.endsWith('/bookings') && options?.method === 'POST') {
      writes.push(JSON.parse(String(options.body)));
      if (writes.length === 1) return response({}, 500);
      booked = true;
      return response(bookedLesson);
    }
  });
  render(<App />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Book a lesson' }));
  await user.selectOptions(await screen.findByLabelText('Class to book'), 'c1');
  await user.selectOptions(screen.getByLabelText('Credit package'), pack.id);
  if (role === 'admin') {
    expect(screen.getByRole('button', { name: 'Review booking' })).toBeDisabled();
    await user.type(screen.getByLabelText('Level override reason'), 'Coach assessment');
  } else {
    expect(screen.queryByLabelText('Level override reason')).not.toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /Expert/ })).not.toBeInTheDocument();
  }
  await user.click(screen.getByRole('button', { name: 'Review booking' }));
  expect(writes).toHaveLength(0);
  await user.click(screen.getByRole('button', { name: 'Confirm booking' }));
  const retry = await screen.findByRole('button', { name: 'Retry same booking' });
  await user.click(retry);
  await screen.findByText('Lesson booked. One place and one credit are reserved.');
  await screen.findByText('Lesson to reserve');
  expect(writes).toHaveLength(2);
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0]).toEqual({
    classId: lesson.id, classVersion: 2, classTypeVersion: 3, creditPackageId: pack.id, idempotencyKey: expect.any(String),
    ...(role === 'admin' ? { overrideReason: 'Coach assessment' } : {}),
  });
  expect(mock).toHaveBeenCalledWith('/api/v1/students/amy/bookings', expect.objectContaining({ headers: expect.objectContaining({ 'X-CSRF-Token': `csrf-${role}` }) }));
  expect(await screen.findByText(/Available 0 · Reserved 1/)).toBeVisible();
});

it('refreshes stale choices and requires a fresh confirmation before submitting new versions', async () => {
  at('/students/amy');
  const startsAt = new Date(Date.now() + 3 * 86400000).toISOString();
  const endsAt = new Date(Date.parse(startsAt) + 3600000).toISOString();
  const writes: Record<string, unknown>[] = [];
  const mock = api('parent', (path, options) => {
    if (path.includes('/classes?')) return response({ items: [{
      id: 'c1', classTypeVersion: writes.length + 1, version: 1, classTypeName: 'Updated lesson',
      level: amy.level, startsAt, endsAt, venueName: 'Harbour Pool', coachName: 'Alex',
    }] });
    if (path.endsWith('/credit-history')) return response({ items: [{ id: 'pack', available: 1, expiresOn: null, reserved: 0, consumed: 0, expired: 0, credits: 1, movements: [], sourcePackageId: null }], recoveries: [] });
    if (path.endsWith('/bookings') && options?.method === 'POST') {
      writes.push(JSON.parse(String(options.body)));
      return response({ error: { code: 'version_conflict' } }, 409);
    }
  });
  render(<App />);
  const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Book a lesson' }));
  async function chooseAndConfirm() {
    await user.selectOptions(await screen.findByLabelText('Class to book'), 'c1');
    await user.selectOptions(screen.getByLabelText('Credit package'), 'pack');
    await user.click(screen.getByRole('button', { name: 'Review booking' }));
    await user.click(screen.getByRole('button', { name: 'Confirm booking' }));
  }
  await chooseAndConfirm();
  expect(await screen.findByText('The class changed. Choose from the refreshed options.')).toHaveAttribute('role', 'alert');
  await screen.findByLabelText('Class to book');
  expect(screen.getByLabelText('Class to book')).toHaveValue('');
  expect(screen.getByRole('button', { name: 'Review booking' })).toBeDisabled();
  expect(writes).toHaveLength(1);
  await chooseAndConfirm();
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes.map(body => body.classTypeVersion)).toEqual([1, 2]);
  expect(writes[0].idempotencyKey).not.toBe(writes[1].idempotencyKey);
  expect(mock.mock.calls.filter(([path]) => String(path).includes('/classes?')).length).toBeGreaterThan(1);
});
