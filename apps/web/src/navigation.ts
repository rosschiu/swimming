import type { MouseEvent } from 'react';
import type { User } from './api';

export type View = 'home' | 'students' | 'accounts' | 'audit' | 'schedule' | 'setup' | 'availability' | 'notifications' | 'deliveries' | 'packages' | 'lesson-changes' | 'missing-attendance';
export const deliveryStatuses = ['pending', 'leased', 'sent', 'retry', 'failed', 'suppressed'] as const;
export type DeliveryStatus = typeof deliveryStatuses[number];
export type Route = { view: View; studentId?: string; bookingId?: string; auditEntityId?: string; classId?: string; deliveryStatus?: DeliveryStatus; lessonChangeStatus?: 'pending'; lessonChangeId?: string } | { view: 'unavailable' };
const paths: Record<View, string> = { home: '/home', students: '/students', accounts: '/accounts', audit: '/audit', schedule: '/schedule', setup: '/setup', availability: '/availability', notifications: '/notifications', deliveries: '/notification-deliveries', packages: '/packages', 'lesson-changes': '/lesson-changes', 'missing-attendance': '/missing-attendance' };

export function parseRoute(path: string, search = ''): Route {
  if (path === paths.audit) {
    return { view: 'audit', auditEntityId: new URLSearchParams(search).get('entityId') || undefined };
  }
  if (path === paths.deliveries) {
    const status = new URLSearchParams(search).get('status');
    const deliveryStatus = deliveryStatuses.find(value => value === status);
    return { view: 'deliveries', deliveryStatus };
  }
  if (path === paths['lesson-changes'] && new URLSearchParams(search).get('status') === 'pending') {
    return { view: 'lesson-changes', lessonChangeStatus: 'pending' };
  }
  if (path === '/') return { view: 'home' };
  for (const view of Object.keys(paths) as View[]) if (paths[view] === path) return { view };
  const change = /^\/lesson-changes\/([^/]+)\/([a-f0-9]{64})$/.exec(path);
  const booking = /^\/students\/([^/]+)\/bookings\/([A-Za-z0-9_-]+)$/.exec(path);
  const student = /^\/students\/([^/]+)(?:\/lesson-changes\/([a-f0-9]{64}))?$/.exec(path);
  const roster = /^\/schedule\/([^/]+)\/roster$/.exec(path);
  const match = change ?? booking ?? student ?? roster;
  if (match) {
    try {
      const id = decodeURIComponent(match[1]);
      if (id && id !== '.' && id !== '..' && !/[\/\\\u0000-\u001f\u007f]/.test(id)) {
        if (change) return { view: 'lesson-changes', studentId: id, lessonChangeId: change[2] };
        if (booking) return { view: 'students', studentId: id, bookingId: booking[2] };
        if (student) return { view: 'students', studentId: id, ...(student[2] && { lessonChangeId: student[2] }) };
        return { view: 'schedule', classId: id };
      }
    } catch { /* Malformed URL segments have no destination. */ }
  }
  return { view: 'unavailable' };
}
export function routePath(route: Route): string {
  if (route.view === 'unavailable') return '/';
  if (route.view === 'audit' && route.auditEntityId) return `${paths.audit}?${new URLSearchParams({ entityId: route.auditEntityId })}`;
  if (route.view === 'deliveries' && route.deliveryStatus) return `${paths.deliveries}?status=${route.deliveryStatus}`;
  if (route.view === 'lesson-changes' && route.studentId && route.lessonChangeId) return `/lesson-changes/${encodeURIComponent(route.studentId)}/${encodeURIComponent(route.lessonChangeId)}`;
  if (route.view === 'lesson-changes' && route.lessonChangeStatus) return `${paths['lesson-changes']}?status=pending`;
  if (route.view === 'schedule' && route.classId) return `/schedule/${encodeURIComponent(route.classId)}/roster`;
  if (route.view === 'students' && route.studentId) {
    const profile = `/students/${encodeURIComponent(route.studentId)}`;
    if (route.bookingId) return `${profile}/bookings/${encodeURIComponent(route.bookingId)}`;
    return route.lessonChangeId ? `${profile}/lesson-changes/${encodeURIComponent(route.lessonChangeId)}` : profile;
  }
  return paths[route.view];
}
export function canView(route: Route, role: User['role']): boolean {
  switch (route.view) {
    case 'home': return true;
    case 'students': return (!route.lessonChangeId || role !== 'student') && (!route.bookingId || role !== 'coach');
    case 'schedule': return !route.classId || role === 'admin' || role === 'coach';
    case 'lesson-changes': return role === 'admin' || role === 'coach' || role === 'parent';
    case 'packages': return role === 'admin' || role === 'parent';
    case 'notifications': return role === 'parent' || role === 'student' || role === 'coach';
    case 'availability': return role === 'admin' || role === 'coach';
    case 'missing-attendance': case 'accounts': case 'audit': case 'setup': case 'deliveries': return role === 'admin';
    default: return false;
  }
}
export function isLocalNavigation(event: MouseEvent<HTMLAnchorElement>): boolean {
  return !event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && !event.currentTarget.hasAttribute('download') && (!event.currentTarget.target || event.currentTarget.target === '_self');
}
