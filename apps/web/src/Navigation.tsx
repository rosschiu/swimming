import { useEffect, useRef, useState, type MouseEvent } from 'react';
import type { User } from './api';
import { routePath, type Route, type View } from './navigation';
import type { Translate } from './useProtectedRequest';

type Role = User['role'];
type Item = { view: View; label: string; tab?: string; badge?: number };

// Phone layouts use bottom tabs; jsdom and older browsers without matchMedia get the desktop sidebar.
export function useCompactLayout() {
  const query = '(max-width: 900px)';
  const [compact, setCompact] = useState(() => typeof window.matchMedia === 'function' && window.matchMedia(query).matches);
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return;
    const media = window.matchMedia(query);
    const update = () => setCompact(media.matches);
    update(); media.addEventListener('change', update);
    return () => media.removeEventListener('change', update);
  }, []);
  return compact;
}

function items(role: Role, unread: number, t: Translate): Item[] {
  const list: Item[] = [{ view: 'home', label: t('Home', '主頁'), tab: role === 'coach' ? t('Today', '今日') : t('Home', '主頁') }];
  if (role !== 'admin') list.push({ view: 'notifications', label: `${t('Notifications', '通知')} (${unread})`, tab: t('Inbox', '通知'), badge: unread });
  if (role === 'admin') list.push({ view: 'deliveries', label: t('Notification deliveries', '通知寄送') });
  list.push({ view: 'students', label: t(role === 'parent' ? 'My children' : role === 'student' ? 'My profile' : 'Students', role === 'parent' ? '我的子女' : role === 'student' ? '我的資料' : '學員'),
    tab: role === 'parent' ? t('Children', '子女') : role === 'student' ? t('Profile', '我的資料') : role === 'admin' ? t('Students', '學員') : undefined });
  if (role !== 'student') list.push({ view: 'lesson-changes', label: t('Lesson changes', '調課申請'), tab: role === 'coach' || role === 'admin' ? t('Requests', '申請') : undefined });
  list.push({ view: 'schedule', label: t('Schedule', '時間表'), tab: role !== 'parent' ? t(role === 'student' ? 'Classes' : 'Schedule', role === 'student' ? '課堂' : '時間表') : undefined });
  if (role === 'admin' || role === 'parent') list.push({ view: 'packages', label: t('Packages and payments', '套票及付款'), tab: role === 'parent' ? t('Packages', '套票') : undefined });
  if (role === 'admin' || role === 'coach') list.push({ view: 'availability', label: t('Availability', '可授課時間') });
  if (role === 'admin') list.push({ view: 'setup', label: t('School setup', '學校設定') }, { view: 'accounts', label: t('Accounts', '帳戶') }, { view: 'audit', label: t('Audit history', '操作紀錄') });
  return list;
}

type Props = {
  role: Role; name: string; roleLabel: string; unread: number; view: View | 'unavailable'; compact: boolean; busy: boolean; t: Translate;
  follow: (event: MouseEvent<HTMLAnchorElement>, next: Route) => void;
  onLanguage: () => void; onSignOut: () => void;
};

export default function Navigation({ role, name, roleLabel, unread, view, compact, busy, t, follow, onLanguage, onSignOut }: Props) {
  const [more, setMore] = useState(false);
  const sheet = useRef<HTMLDivElement>(null);
  const moreButton = useRef<HTMLButtonElement>(null);
  function closeSheet() { setMore(false); moreButton.current?.focus(); }
  const all = items(role, unread, t);
  useEffect(() => { setMore(false); }, [view]);
  useEffect(() => {
    if (!more) return;
    sheet.current?.querySelector<HTMLElement>('a,button')?.focus();
    const close = (event: KeyboardEvent) => { if (event.key === 'Escape') closeSheet(); };
    window.addEventListener('keydown', close); return () => window.removeEventListener('keydown', close);
  }, [more]);
  const link = (item: Item, className: string, label = item.label) => {
    const next: Route = { view: item.view };
    const active = view === item.view;
    return <a key={item.view} href={routePath(next)} className={active ? `${className} nav-active` : className} aria-current={active ? 'page' : undefined} onClick={event => follow(event, next)}>{label}</a>;
  };
  if (!compact) return <aside className="sidebar"><p className="eyebrow">{roleLabel}</p><p className="user-name">{name}</p>
    <nav aria-label={t('Main navigation', '主選單')}>{all.map(item => link(item, 'nav-item'))}</nav>
    <p className="sidebar-note">{t('Only records you are authorised to access appear here.', '這裡只顯示你獲授權查看的資料。')}</p></aside>;

  const tabs = all.filter(item => item.tab);
  const rest = all.filter(item => !item.tab);
  const moreActive = rest.some(item => item.view === view);
  return <>
    {more && <div className="sheet-backdrop" onClick={closeSheet}>
      <div className="sheet" ref={sheet} role="dialog" aria-modal="true" aria-label={t('More', '更多')} onClick={event => event.stopPropagation()}>
        <p className="eyebrow">{roleLabel}</p><p className="user-name">{name}</p>
        {rest.length > 0 && <nav aria-label={t('More pages', '更多頁面')} className="sheet-links">{rest.map(item => link(item, 'sheet-link'))}</nav>}
        <button className="outline full-width" onClick={onLanguage}>{t('繁體中文', 'English')}</button>
        <button className="outline full-width" onClick={onSignOut} disabled={busy}>{t('Sign out', '登出')}</button>
        <button className="text-button full-width" onClick={closeSheet}>{t('Close', '關閉')}</button>
      </div>
    </div>}
    <nav className="tabbar" aria-label={t('Main navigation', '主選單')}>
      {tabs.map(item => {
        const next: Route = { view: item.view };
        const active = view === item.view;
        return <a key={item.view} href={routePath(next)} className={active ? 'tab tab-active' : 'tab'} aria-current={active ? 'page' : undefined} onClick={event => follow(event, next)}>
          <span>{item.tab}</span>{item.badge ? <span className="tab-badge" aria-label={t(`${item.badge} unread`, `${item.badge} 則未讀`)}>{item.badge}</span> : null}
        </a>;
      })}
      <button ref={moreButton} className={moreActive || more ? 'tab tab-active' : 'tab'} aria-expanded={more} aria-haspopup="dialog" onClick={() => setMore(true)}>{t('More', '更多')}</button>
    </nav>
  </>;
}
