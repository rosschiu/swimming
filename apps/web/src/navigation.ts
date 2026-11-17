import type { MouseEvent } from 'react';
import type { User } from './api';

export type View = 'students' | 'accounts' | 'audit' | 'schedule' | 'setup' | 'availability' | 'notifications' | 'deliveries';
export type Route = { view: View; studentId?: string } | { view: 'unavailable' };
const paths: Record<View, string> = { students: '/students', accounts: '/accounts', audit: '/audit', schedule: '/schedule', setup: '/setup', availability: '/availability', notifications: '/notifications', deliveries: '/notification-deliveries' };

export function parseRoute(path: string): Route {
  if (path === '/') return { view: 'students' };
  for (const view of Object.keys(paths) as View[]) if (paths[view] === path) return { view };
  const match = /^\/students\/([^/]+)$/.exec(path);
  if (match) {
    try {
      const studentId = decodeURIComponent(match[1]);
      if (studentId && studentId !== '.' && studentId !== '..' && !/[\/\\\u0000-\u001f\u007f]/.test(studentId)) return { view: 'students', studentId };
    } catch { /* Malformed URL segments have no destination. */ }
  }
  return { view: 'unavailable' };
}
export function routePath(route: Route): string {
  if (route.view === 'unavailable') return '/';
  return route.view === 'students' && route.studentId ? `/students/${encodeURIComponent(route.studentId)}` : paths[route.view];
}
export function canView(route: Route, role: User['role']): boolean {
  switch (route.view) {
    case 'students': case 'schedule': return true;
    case 'notifications': return role === 'parent' || role === 'student';
    case 'availability': return role === 'admin' || role === 'coach';
    case 'accounts': case 'audit': case 'setup': case 'deliveries': return role === 'admin';
    default: return false;
  }
}
export function isLocalNavigation(event: MouseEvent<HTMLAnchorElement>): boolean {
  return !event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && !event.currentTarget.hasAttribute('download') && (!event.currentTarget.target || event.currentTarget.target === '_self');
}
