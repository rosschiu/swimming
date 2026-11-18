import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import App from './App';

type Role = 'admin' | 'coach' | 'parent' | 'student';
const response = (body: unknown, status = 200) => new Response(status === 204 ? null : JSON.stringify(body), { status });
const lesson = { id: 'c1', venueId: 'v', classTypeId: 't', coachId: 'coach', startsAt: '2030-01-07T09:00:00Z', endsAt: '2030-01-07T10:00:00Z', capacity: 6,
  venueName: 'Harbour Pool', region: 'hk-island', classTypeName: 'Water Confidence', level: 'Beginner', coachName: 'Coach Alex', version: 1, classTypeVersion: 1 };
const amy = { id: 'amy', name: 'Amy', level: 'Beginner', upcomingCount: 3, pendingChanges: 1,
  nextLesson: { id: 'b1', studentId: 'amy', status: 'reserved', createdAt: '2030-01-01T00:00:00Z', class: lesson },
  credits: { available: 0, reserved: 3, nextExpiry: null } };
const ben = { id: 'ben', name: 'Ben', level: 'Advanced', upcomingCount: 0, pendingChanges: 0, nextLesson: null, credits: { available: 5, reserved: 0, nextExpiry: '2030-02-28' } };

function mockAPI(role: Role, home: () => Response) {
  const mock = vi.fn(async (input: string | URL | Request) => {
    const path = String(input);
    if (path.endsWith('/auth/me')) return response({ user: { id: role, name: 'Sam', role }, csrfToken: 'csrf' });
    if (path.endsWith('/auth/logout')) return response(null, 204);
    if (path.endsWith('/home')) return home();
    if (path.endsWith('/admin/action-queue')) return response({ purchasesAwaitingExpiry: 0, failedEmailDeliveries: 0, pendingLessonChanges: 0, classesMissingAttendance: 0 });
    return response({ items: [], nextCursor: null, unreadCount: 2 });
  });
  vi.stubGlobal('fetch', mock); return mock;
}
function phone(matches: boolean) {
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
}
beforeEach(() => { vi.unstubAllGlobals(); window.localStorage.clear(); window.history.replaceState(null, '', '/'); });

it('lands a guardian on each swimmer’s next lesson, credits and pending change', async () => {
  mockAPI('parent', () => response({ role: 'parent', today: '2030-01-07', students: [amy, ben], classes: [], pendingLessonChanges: 1, unreadNotifications: 2 }));
  render(<App />);
  const amyCard = await screen.findByRole('region', { name: 'Amy' });
  expect(screen.getByRole('heading', { name: 'Hi, Sam' })).toBeVisible();
  const next = within(amyCard).getByRole('link', { name: /Mon, 7 Jan 2030.*17:00–18:00.*Water Confidence.*Harbour Pool · Coach Alex/ });
  expect(next).toHaveAttribute('href', '/students/amy/bookings/b1');
  expect(amyCard).toHaveTextContent('2 more booked after this');
  expect(amyCard).toHaveTextContent('0 lessons left to book');
  expect(amyCard).toHaveTextContent('1 lesson change awaiting the coach');
  expect(within(amyCard).getByRole('link', { name: 'Buy lessons' })).toHaveAttribute('href', '/packages');
  const benCard = screen.getByRole('region', { name: 'Ben' });
  expect(benCard).toHaveTextContent('No upcoming lessons booked.');
  expect(benCard).toHaveTextContent('5 lessons left to book · use by 28 Feb 2030');
  expect(within(benCard).queryByRole('link', { name: 'Buy lessons' })).not.toBeInTheDocument();
  expect(screen.getByRole('link', { name: /2 unread updates/ })).toHaveAttribute('href', '/notifications');
});

it('shows a student their lessons without credit or purchase prompts', async () => {
  mockAPI('student', () => response({ role: 'student', today: '2030-01-07', students: [{ ...amy, credits: undefined, pendingChanges: 0 }], classes: [], pendingLessonChanges: 0, unreadNotifications: 0 }));
  render(<App />);
  const card = await screen.findByRole('region', { name: 'Amy' });
  expect(within(card).getByRole('link', { name: 'My lessons and progress' })).toHaveAttribute('href', '/students/amy');
  expect(card).not.toHaveTextContent('left to book');
  expect(within(card).queryByRole('link', { name: 'Buy lessons' })).not.toBeInTheDocument();
});

it('gives a coach their lessons by day with roster links and the request queue', async () => {
  const later = { ...lesson, id: 'c2', startsAt: '2030-01-08T09:00:00Z', endsAt: '2030-01-08T10:00:00Z', classTypeName: 'Swim Squad' };
  mockAPI('coach', () => response({ role: 'coach', today: '2030-01-07', students: [], pendingLessonChanges: 2, unreadNotifications: 0,
    classes: [{ class: lesson, booked: 4, unmarked: 4 }, { class: later, booked: 1, unmarked: 1 }] }));
  render(<App />);
  const lessons = await screen.findByRole('region', { name: 'Your lessons' });
  expect(within(lessons).getByText('Mon, 7 Jan 2030')).toBeVisible();
  expect(within(lessons).getByText('Tue, 8 Jan 2030')).toBeVisible();
  expect(within(lessons).getByRole('link', { name: /17:00–18:00.*Water Confidence.*4 booked/ })).toHaveAttribute('href', '/schedule/c1/roster');
  expect(screen.getByRole('link', { name: /2 lesson change requests need your decision/ })).toHaveAttribute('href', '/lesson-changes?status=pending');
});

it('retries a failed home summary in Traditional Chinese', async () => {
  let attempts = 0;
  mockAPI('parent', () => ++attempts === 1 ? response({}, 500) : response({ role: 'parent', today: '2030-01-07', students: [], classes: [], pendingLessonChanges: 0, unreadNotifications: 0 }));
  window.localStorage.setItem('language', 'zh');
  render(<App />);
  expect(await screen.findByRole('alert')).toHaveTextContent('未能載入主頁。');
  await userEvent.setup().click(screen.getByRole('button', { name: '重試' }));
  expect(await screen.findByRole('heading', { name: '尚未連結學員' })).toBeVisible();
});

it('uses bottom tabs and a dismissible More sheet on phones', async () => {
  phone(true);
  mockAPI('parent', () => response({ role: 'parent', today: '2030-01-07', students: [], classes: [], pendingLessonChanges: 0, unreadNotifications: 0 }));
  render(<App />);
  const tabs = await screen.findByRole('navigation', { name: 'Main navigation' });
  await within(tabs).findByLabelText('2 unread');
  expect(within(tabs).getAllByRole('link').map(link => link.textContent)).toEqual(['Home', 'Inbox2', 'Children', 'Packages']);
  expect(within(tabs).getByRole('link', { name: 'Home' })).toHaveAttribute('aria-current', 'page');
  expect(screen.queryByRole('complementary')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument();
  const user = userEvent.setup();
  await user.click(within(tabs).getByRole('button', { name: 'More' }));
  const sheet = screen.getByRole('dialog', { name: 'More' });
  expect(within(sheet).getAllByRole('link').map(link => link.textContent)).toEqual(['Lesson changes', 'Schedule']);
  expect(within(sheet).getByRole('link', { name: 'Lesson changes' })).toHaveFocus();
  await user.keyboard('{Escape}');
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(within(tabs).getByRole('button', { name: 'More' })).toHaveFocus();
  await user.click(within(tabs).getByRole('button', { name: 'More' }));
  await user.click(within(screen.getByRole('dialog')).getByRole('link', { name: 'Schedule' }));
  expect(window.location.pathname).toBe('/schedule');
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(within(tabs).getByRole('button', { name: 'More' })).toHaveClass('tab-active');
  await user.click(within(tabs).getByRole('button', { name: 'More' }));
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Sign out' }));
  expect(await screen.findByRole('button', { name: 'Sign in' })).toBeVisible();
});

it('keeps the full sidebar with Home first on wide screens', async () => {
  phone(false);
  mockAPI('coach', () => response({ role: 'coach', today: '2030-01-07', students: [], classes: [], pendingLessonChanges: 0, unreadNotifications: 0 }));
  await act(async () => { render(<App />); });
  const nav = await screen.findByRole('navigation', { name: 'Main navigation' });
  expect(within(nav).getAllByRole('link').map(link => link.textContent)).toEqual(['Home', 'Notifications (2)', 'Students', 'Lesson changes', 'Schedule', 'Availability']);
  expect(screen.getByRole('button', { name: 'Sign out' })).toBeVisible();
});
