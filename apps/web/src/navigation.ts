import type { MouseEvent } from 'react';
import type { User } from './api';

export type View = 'students' | 'accounts' | 'audit' | 'schedule' | 'setup' | 'availability' | 'notifications' | 'deliveries' | 'packages' | 'lesson-changes' | 'missing-attendance';
export const deliveryStatuses = ['pending', 'leased', 'sent', 'retry', 'failed', 'suppressed'] as const;
export type DeliveryStatus = typeof deliveryStatuses[number];
export type Route = { view: View; studentId?: string; classId?: string; deliveryStatus?: DeliveryStatus; lessonChangeStatus?: 'pending'; lessonChangeId?: string } | { view: 'unavailable' };
const paths: Record<View, string> = { students: '/students', accounts: '/accounts', audit: '/audit', schedule: '/schedule', setup: '/setup', availability: '/availability', notifications: '/notifications', deliveries: '/notification-deliveries', packages: '/packages', 'lesson-changes': '/lesson-changes', 'missing-attendance': '/missing-attendance' };

export function parseRoute(path: string, search = ''): Route {
  if (path === paths.deliveries) {
    const status = new URLSearchParams(search).get('status');
    const deliveryStatus = deliveryStatuses.find(value => value === status);
    return { view: 'deliveries', deliveryStatus };
  }
  if (path === paths['lesson-changes'] && new URLSearchParams(search).get('status') === 'pending') {
    return { view: 'lesson-changes', lessonChangeStatus: 'pending' };
  }
  if (path === '/') return { view: 'students' };
  for (const view of Object.keys(paths) as View[]) if (paths[view] === path) return { view };
  const student = /^\/students\/([^/]+)(?:\/lesson-changes\/([a-f0-9]{64}))?$/.exec(path);
  const roster = /^\/schedule\/([^/]+)\/roster$/.exec(path);
  const match = student ?? roster;
  if (match) {
    try {
      const id = decodeURIComponent(match[1]);
      if (id && id !== '.' && id !== '..' && !/[\/\\\u0000-\u001f\u007f]/.test(id)) return student ? { view: 'students', studentId: id, ...(student[2] && { lessonChangeId: student[2] }) } : { view: 'schedule', classId: id };
    } catch { /* Malformed URL segments have no destination. */ }
  }
  return { view: 'unavailable' };
}
export function routePath(route: Route): string {
  if (route.view === 'unavailable') return '/';
  if (route.view === 'deliveries' && route.deliveryStatus) return `${paths.deliveries}?status=${route.deliveryStatus}`;
  if (route.view === 'lesson-changes' && route.lessonChangeStatus) return `${paths['lesson-changes']}?status=pending`;
  if (route.view === 'schedule' && route.classId) return `/schedule/${encodeURIComponent(route.classId)}/roster`;
  if (route.view === 'students' && route.studentId) {
    const profile = `/students/${encodeURIComponent(route.studentId)}`;
    return route.lessonChangeId ? `${profile}/lesson-changes/${encodeURIComponent(route.lessonChangeId)}` : profile;
  }
  return paths[route.view];
}
export function canView(route: Route, role: User['role']): boolean {
  switch (route.view) {
    case 'students': return !route.lessonChangeId || role !== 'student';
    case 'schedule': return !route.classId || role === 'admin' || role === 'coach';
    case 'lesson-changes': return role === 'admin' || role === 'coach' || role === 'parent';
    case 'packages': return role === 'admin' || role === 'parent';
    case 'notifications': return role === 'parent' || role === 'student';
    case 'availability': return role === 'admin' || role === 'coach';
    case 'missing-attendance': case 'accounts': case 'audit': case 'setup': case 'deliveries': return role === 'admin';
    default: return false;
  }
}
export function isLocalNavigation(event: MouseEvent<HTMLAnchorElement>): boolean {
  return !event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && !event.currentTarget.hasAttribute('download') && (!event.currentTarget.target || event.currentTarget.target === '_self');
}
