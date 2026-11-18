import AdminActionQueue from './AdminActionQueue';
import { useEffect, useRef, useState, type FormEvent, type MouseEvent } from 'react';
import AccountAccess, { readAccessLink } from './AccountAccess';
import Availability from './Availability';
import Schedule from './Schedule';
import SchoolSetup from './SchoolSetup';
import UserManagement from './UserManagement';
import Relationships from './Relationships';
import AuditViewer from './AuditViewer';
import ProgressNotes from './ProgressNotes';
import Notifications from './Notifications';
import NotificationDeliveries from './NotificationDeliveries';
import Packages from './Packages';
import CreditHistory from './CreditHistory';
import StudentMilestones from './StudentMilestones';
import LessonChanges from './LessonChanges';
import StudentBookings from './StudentBookings';
import { APIError, request, type Session, type Student } from './api';
import { canView, isLocalNavigation, parseRoute, routePath, type Route, type View } from './navigation';

const demoRoles = [
  ['admin', 'Administrator', '管理員'],
  ['coach', 'Coach', '教練'],
  ['parent', 'Guardian', '監護人'],
  ['student', 'Student', '學員'],
] as const;

export default function App() {
  const [language, setLanguage] = useState<'en' | 'zh'>(() => window.localStorage.getItem('language') === 'zh' ? 'zh' : 'en');
  const t = (en: string, zh: string) => language === 'en' ? en : zh;
  const hostname = window.location.hostname;
  const showDemoAccounts = hostname === 'swimmer.bsoltest.com' ||
    (hostname === 'localhost' && import.meta.env.DEV && import.meta.env.VITE_TEST_DEMO_LOGIN === 'true');
  const generation = useRef(0);
  const profileGeneration = useRef(0);
  const routeGeneration = useRef(0);
  const directoryGeneration = useRef(0);
  const main = useRef<HTMLElement>(null);
  const focusDestination = useRef(false);
  const [session, setSession] = useState<Session | null>(null);
  const [checking, setChecking] = useState(true);
  const [startupError, setStartupError] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [students, setStudents] = useState<Student[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [selected, setSelected] = useState<Student | null>(null);
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState('');
  const [level, setLevel] = useState('');
  const [query, setQuery] = useState('');
  const [route, setRoute] = useState<Route>(() => parseRoute(window.location.pathname, window.location.search));
  const view = route.view;
  const studentId = route.view === 'students' ? route.studentId : undefined;
  const [profileState, setProfileState] = useState<'loading' | 'unavailable' | 'error' | null>(studentId ? 'loading' : null);
  const [unreadCount, setUnreadCount] = useState(0);
  const [bookingRevision, setBookingRevision] = useState(0);
  function changeRoute(next: Route) {
    routeGeneration.current++; profileGeneration.current++; directoryGeneration.current++;
    setRoute(next); setSelected(null); setEditing(false); setName(''); setLevel('');
    setError(''); setNotice(''); setBusy(false); setLoading(false); setLoadError(false);
    setProfileState(next.view === 'students' && next.studentId ? 'loading' : null);
    focusDestination.current = true;
  }
  function navigate(next: Route) {
    window.history.pushState(null, '', routePath(next));
    changeRoute(next);
  }
  function follow(event: MouseEvent<HTMLAnchorElement>, next: Route) {
    if (!isLocalNavigation(event)) return;
    event.preventDefault(); navigate(next);
  }
  useEffect(() => {
    const restore = () => { setAccessLink(readAccessLink()); setForgot(false); changeRoute(parseRoute(window.location.pathname, window.location.search)); };
    window.addEventListener('popstate', restore); return () => window.removeEventListener('popstate', restore);
  }, []);
  const [accessLink, setAccessLink] = useState(readAccessLink);
  const [forgot, setForgot] = useState(false);
  useEffect(() => { if (accessLink) window.history.replaceState(null, '', window.location.pathname); }, [accessLink]);
  function backToLogin() {
    if (accessLink) { window.history.replaceState(null, '', '/'); changeRoute(parseRoute('/')); }
    setAccessLink(null); setForgot(false); clearSession();
  }


  function clearSession() {
    generation.current++; profileGeneration.current++; directoryGeneration.current++;
    setSession(null); setStudents([]); setSelected(null); setEditing(false);
    setName(''); setLevel(''); setQuery(''); setProfileState(studentId ? 'loading' : null); setUnreadCount(0); setNotice(''); setError('');
    setBusy(false); setLoading(false); setLoadError(false); setChecking(false); setStartupError(false);
  }
  function expired(e: unknown) {
    if (e instanceof APIError && e.status === 401) { clearSession(); return true; }
    return false;
  }
  useEffect(() => { document.documentElement.lang = language === 'zh' ? 'zh-Hant-HK' : 'en'; window.localStorage.setItem('language', language); }, [language]);
  async function checkSession() {
    const current = generation.current;
    setChecking(true); setStartupError(false);
    try { const restored = await request<Session>('/auth/me'); if (current === generation.current) setSession(restored); }
    catch (e) { if (current === generation.current && !(e instanceof APIError && e.status === 401)) setStartupError(true); }
    finally { if (current === generation.current) setChecking(false); }
  }
  useEffect(() => { void checkSession(); }, []);
  async function loadStudents() {
    const current = generation.current;
    const directoryRequest = ++directoryGeneration.current;
    const relevant = () => current === generation.current && directoryRequest === directoryGeneration.current;
    setLoading(true); setLoadError(false);
    try { const result = await request<{ items: Student[] }>('/students'); if (relevant()) setStudents(result.items); }
    catch (e) { if (relevant() && !expired(e)) setLoadError(true); }
    finally { if (relevant()) setLoading(false); }
  }
  useEffect(() => { if (session && !accessLink && !forgot && canView(route, session.user.role) && (view === 'accounts' || view === 'students' && !studentId)) void loadStudents(); }, [session, route, accessLink, forgot]);
  async function login(e: FormEvent<HTMLFormElement>) {
    e.preventDefault(); clearSession(); setBusy(true);
    const current = generation.current;
    const data = new FormData(e.currentTarget);
    try { const restored = await request<Session>('/auth/login', { method: 'POST', body: JSON.stringify({ email: data.get('email'), password: data.get('password') }) }); if (current === generation.current) { setSession(restored); setNotice(''); focusDestination.current = true; } }
    catch { if (current === generation.current) setError(t('Unable to sign in. Check your email and password, then try again.', '無法登入。請檢查電郵及密碼後再試。')); }
    finally { if (current === generation.current) setBusy(false); }
  }
  async function logout() {
    const current = generation.current;
    setBusy(true); setError('');
    try { await request('/auth/logout', { method: 'POST', headers: { 'X-CSRF-Token': session!.csrfToken } }); if (current === generation.current) clearSession(); }
    catch (e) { if (current !== generation.current || expired(e)) return; setError(t('Sign out failed. Please try again.', '登出失敗，請再試。')); }
    finally { if (current === generation.current) setBusy(false); }
  }
  function openStudent(student: Pick<Student, 'id'>) { navigate({ view: 'students', studentId: student.id }); }
  async function loadProfile(id: string) {
    const current = generation.current;
    const routeRequest = routeGeneration.current;
    const profileRequest = ++profileGeneration.current;
    const relevant = () => current === generation.current && routeRequest === routeGeneration.current && profileRequest === profileGeneration.current;
    setSelected(null); setProfileState('loading'); setError('');
    try {
      const profile = await request<Student>(`/students/${encodeURIComponent(id)}`);
      if (relevant()) { setSelected(profile); setProfileState(null); }
    } catch (e) {
      if (!relevant() || expired(e)) return;
      setProfileState(e instanceof APIError && (e.status === 403 || e.status === 404) ? 'unavailable' : 'error');
    }
  }
  useEffect(() => {
    if (session && !accessLink && !forgot && studentId && canView(route, session.user.role)) void loadProfile(studentId);
  }, [session, route, accessLink, forgot]);
  useEffect(() => {
    if (focusDestination.current && !checking) { main.current?.focus(); focusDestination.current = false; }
  });
  function edit(student: Student | null) { profileGeneration.current++; setSelected(student); setName(student?.name || ''); setLevel(student?.level || ''); setEditing(true); setError(''); setNotice(''); }
  async function save(e: FormEvent) {
    const current = generation.current;
    const routeRequest = routeGeneration.current;
    const profileRequest = ++profileGeneration.current;
    const relevant = () => current === generation.current && routeRequest === routeGeneration.current && profileRequest === profileGeneration.current;
    e.preventDefault(); setError('');
    if (!name.trim() || !level.trim()) { setError(t('Enter a name and swimming level.', '請填寫姓名及游泳程度。')); return; }
    setBusy(true);
    try {
      const saved = await request<Student>(selected ? `/students/${encodeURIComponent(selected.id)}` : '/students', { method: selected ? 'PATCH' : 'POST', headers: { 'X-CSRF-Token': session!.csrfToken }, body: JSON.stringify({ name: name.trim(), level: level.trim(), ...(selected ? { version: selected.version } : {}) }) });
      if (!relevant()) return;
      if (!selected) navigate({ view: 'students', studentId: saved.id });
      else { setSelected(saved); setEditing(false); setBusy(false); }
      setNotice(t('Student record saved.', '已儲存學員資料。'));
    } catch (e) { if (!relevant() || expired(e)) return; setError(e instanceof APIError && e.status === 409 ? t('Someone updated this record. Cancel and reopen the profile to load the latest version before saving.', '此資料已被更新。請取消並重新開啟資料，載入最新版本後再儲存。') : t('Could not save. Your entries are preserved; please try again.', '未能儲存。已保留輸入內容，請再試。')); }
    finally { if (relevant()) setBusy(false); }
  }
  useEffect(() => {
    if (view === 'notifications' || !session || accessLink || forgot || !canView(route, session.user.role) || (session.user.role !== 'parent' && session.user.role !== 'student')) return;
    const controller = new AbortController();
    const current = generation.current;
    request<{ unreadCount: number }>('/notifications?limit=1', { signal: controller.signal }).then(result => { if (!controller.signal.aborted && current === generation.current) setUnreadCount(result.unreadCount); }).catch(e => { if (!controller.signal.aborted && current === generation.current) expired(e); });
    return () => controller.abort();
  }, [session, route, accessLink, forgot, bookingRevision]);
  const role = session?.user.role;
  const roles = { admin: t('Administrator', '管理員'), parent: t('Parent', '家長'), coach: t('Coach', '教練'), student: t('Student', '學員') };
  const allowed = !!session && canView(route, session.user.role);
  function navLink(destination: View, label: string) {
    const next: Route = { view: destination };
    return <a href={routePath(next)} className={view === destination ? 'nav-active' : 'nav-item'} aria-current={view === destination ? 'page' : undefined} onClick={event => follow(event, next)}>{label}</a>;
  }
  const visible = students.filter(s => `${s.name} ${s.level}`.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
  return <div className="app"><a className="skip" href="#main">{t('Skip to content', '跳至內容')}</a><header className="topbar"><a className="brand" href="/" aria-label="SwimCoach Pro"><span className="brand-mark" aria-hidden="true">≈</span><span>SwimCoach <strong>Pro</strong></span></a><div className="header-actions"><button className="text-button" onClick={() => setLanguage(language === 'en' ? 'zh' : 'en')}>{t('繁體中文', 'English')}</button>{session && <button className="outline" onClick={logout} disabled={busy}>{t('Sign out', '登出')}</button>}</div></header>
    <main id="main" ref={main} tabIndex={-1}>
      {accessLink || forgot ? <AccountAccess link={accessLink} t={t} onBack={backToLogin}/> : checking ? <p role="status" className="loading">{t('Checking your session…', '正在檢查登入狀態…')}</p> : startupError ? <section className="login-card"><h1>{t('Unable to connect', '未能連線')}</h1><p>{t('We could not check your session. Please try again.', '未能檢查登入狀態，請再試。')}</p><button onClick={checkSession}>{t('Retry', '重試')}</button></section> : !session ? <div className="login-layout"><section className="welcome"><p className="eyebrow">SWIMCOACH PRO</p><h1>{t('A clear view of every swimmer.', '掌握每位學員的成長。')}</h1><p>{t('Your swimming school, connected. Sign in to access the student records assigned to you.', '連繫你的游泳學校。登入以查看獲授權的學員資料。')}</p><div className="water-lines" aria-hidden="true">≈<br/>≈<br/>≈</div></section><section className="login-card"><p className="eyebrow">{t('WELCOME BACK', '歡迎回來')}</p><h2>{t('Sign in', '登入')}</h2><p className="muted">{t('Use the account provided by your school.', '請使用學校提供的帳戶。')}</p><form onSubmit={login}>
        {showDemoAccounts && <fieldset className="demo-accounts">
          <legend>{t('Demo accounts', '示範帳戶')}</legend>
          {demoRoles.map(([role, en, zh]) => <button key={role} type="button" className="outline" disabled={busy} onClick={event => {
            const form = event.currentTarget.form!;
            const email = form.elements.namedItem('email') as HTMLInputElement;
            const password = form.elements.namedItem('password') as HTMLInputElement;
            email.value = `${role}@swim.test`;
            password.value = 'Swim-test-2026!';
            setError('');
            email.focus();
          }}>{t(en, zh)}</button>)}
        </fieldset>}
        <label htmlFor="email">{t('Email', '電郵')}</label><input id="email" name="email" type="email" autoComplete="username" required/><label htmlFor="password">{t('Password', '密碼')}</label><input id="password" name="password" type="password" autoComplete="current-password" required/>{error && <p role="alert" className="error">{error}</p>}<button className="full" disabled={busy}>{busy ? t('Signing in…', '登入中…') : t('Sign in', '登入')}</button></form><button className="text-button full" onClick={() => { setForgot(true); setError(''); }}>{t('Forgot password?', '忘記密碼？')}</button></section></div> : <div className="workspace"><aside className="sidebar"><p className="eyebrow">{roles[role!]}</p><p className="user-name">{session.user.name}</p><nav aria-label={t('Main navigation', '主選單')}>{(role === 'parent' || role === 'student') && navLink('notifications', `${t('Notifications', '通知')} (${unreadCount})`)}{role === 'admin' && navLink('deliveries', t('Notification deliveries', '通知寄送'))}{navLink('students', t(role === 'parent' ? 'My children' : role === 'student' ? 'My profile' : 'Students', role === 'parent' ? '我的子女' : role === 'student' ? '我的資料' : '學員'))}{(role === 'admin' || role === 'coach' || role === 'parent') && navLink('lesson-changes', t('Lesson changes', '調課申請'))}{navLink('schedule', t('Schedule', '時間表'))}{(role === 'admin' || role === 'parent') && navLink('packages', t('Packages and payments', '套票及付款'))}{(role === 'admin' || role === 'coach') && navLink('availability', t('Availability', '可授課時間'))}{role === 'admin' && <>{navLink('setup', t('School setup', '學校設定'))}{navLink('accounts', t('Accounts', '帳戶'))}{navLink('audit', t('Audit history', '操作紀錄'))}</>}</nav><p className="sidebar-note">{t('Only records you are authorised to access appear here.', '這裡只顯示你獲授權查看的資料。')}</p></aside><section className="workspace-main">{!allowed ? <section className="panel"><h1>{t('Page unavailable', '無法開啟此頁面')}</h1><p>{t('This page does not exist or is unavailable for your account.', '此頁面不存在，或你的帳戶無法存取。')}</p><a className="text-button" href="/students" onClick={event => follow(event, { view: 'students' })}>{t('Go to students', '前往學員名單')}</a></section> : view === 'lesson-changes' && (role === 'admin' || role === 'coach' || role === 'parent') ? <LessonChanges key={`${session.user.id}:${route.lessonChangeStatus ?? ''}`} initialPending={route.lessonChangeStatus === 'pending'} follow={follow} role={role} csrfToken={session.csrfToken} expired={expired} t={t}/> : view === 'packages' && (role === 'admin' || role === 'parent') ? <Packages key={session.user.id} csrfToken={session.csrfToken} expired={expired} t={t} role={session.user.role}/> : view === 'notifications' && (role === 'parent' || role === 'student') ? <><Notifications key={session.user.id} csrfToken={session.csrfToken} expired={expired} t={t} onUnreadCount={setUnreadCount} onOpenStudent={id => void openStudent({ id })}/>{error && <p role="alert" className="error">{error}</p>}</> : view === 'deliveries' && role === 'admin' ? <NotificationDeliveries key={`${session.user.id}:${route.deliveryStatus ?? ''}`} status={route.deliveryStatus} onStatusChange={status => navigate({ view: 'deliveries', deliveryStatus: status || undefined })} csrfToken={session.csrfToken} expired={expired} t={t}/> : view === 'availability' && (role === 'admin' || role === 'coach') ? <Availability key={session.user.id} csrfToken={session.csrfToken} expired={expired} t={t} role={session.user.role}/> : view === 'schedule' ? <Schedule key={session.user.id} csrfToken={session.csrfToken} expired={expired} t={t} role={session.user.role}/> : view === 'setup' && role === 'admin' ? <SchoolSetup key={session.user.id} csrfToken={session.csrfToken} expired={expired} t={t}/> : view === 'accounts' && role === 'admin' ? <UserManagement key={session.user.id} csrfToken={session.csrfToken} expired={expired} t={t} students={students}/> : view === 'audit' && role === 'admin' ? <AuditViewer key={session.user.id} csrfToken={session.csrfToken} expired={expired} t={t}/> : <><div className="page-heading"><div><p className="eyebrow">{t('STUDENT DIRECTORY', '學員名單')}</p><h1>{t(role === 'parent' ? 'Your swimmers' : role === 'student' ? 'Your profile' : 'Your students', role === 'parent' ? '你的子女' : role === 'student' ? '你的資料' : '你的學員')}</h1><p className="muted">{t('Keep each swimmer’s information up to date.', '掌握每位學員的最新資料。')}</p></div>{role === 'admin' && !studentId && <button onClick={() => edit(null)} disabled={busy}>{t('Add student', '新增學員')}</button>}</div>{error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
      {!editing && studentId && profileState ? <section className="panel">{profileState === 'loading' ? <p role="status">{t('Loading student profile…', '正在載入學員資料…')}</p> : <><h2>{profileState === 'unavailable' ? t('Student profile unavailable', '無法開啟學員資料') : t('Could not load student profile', '未能載入學員資料')}</h2><p>{profileState === 'unavailable' ? t('This student does not exist or is unavailable for your account.', '此學員不存在，或你的帳戶無法存取。') : t('Please try again.', '請再試。')}</p><button className="outline" onClick={() => void loadProfile(studentId)}>{t('Retry', '重試')}</button><a className="text-button" href="/students" onClick={event => follow(event, { view: 'students' })}>{t('Back to directory', '返回名單')}</a></>}</section> : editing ? <section className="panel editor"><h2>{selected ? t('Edit student', '編輯學員') : t('New student', '新增學員')}</h2><form onSubmit={save}><label htmlFor="student-name">{t('Student name', '學員姓名')}</label><input id="student-name" value={name} onChange={e => setName(e.target.value)} required maxLength={100}/><label htmlFor="student-level">{t('Swimming level', '游泳程度')}</label><input id="student-level" value={level} onChange={e => setLevel(e.target.value)} required maxLength={100}/><div className="form-actions"><button disabled={busy}>{busy ? t('Saving…', '儲存中…') : t('Save student', '儲存學員')}</button><button type="button" className="outline" disabled={busy} onClick={() => { profileGeneration.current++; setEditing(false); setError(''); setBusy(false); if (studentId) void loadProfile(studentId); }}>{t('Cancel', '取消')}</button></div></form></section> : selected ? <section className="panel profile"><a className="text-button" href="/students" onClick={event => follow(event, { view: 'students' })}>← {t('Back to directory', '返回名單')}</a><div className="avatar" aria-hidden="true">{selected.name.slice(0, 1)}</div><h2>{selected.name}</h2><dl><dt>{t('Swimming level', '游泳程度')}</dt><dd>{selected.level}</dd></dl>{role === 'admin' && <><button onClick={() => edit(selected)}>{t('Edit student', '編輯學員')}</button><Relationships key={selected.id} studentId={selected.id} csrfToken={session.csrfToken} expired={expired} t={t}/></>}{(role === 'admin' || role === 'parent' || role === 'student') && <StudentBookings key={`bookings:${session.user.id}:${selected.id}`} student={selected} role={role} csrfToken={session.csrfToken} expired={expired} t={t} onBooked={() => setBookingRevision(previous => previous + 1)}/>}{role !== 'student' && <CreditHistory key={`credits:${session.user.id}:${selected.id}`} revision={bookingRevision} studentId={selected.id} role={session.user.role} csrfToken={session.csrfToken} expired={expired} t={t}/>}<StudentMilestones key={`milestones:${session.user.id}:${selected.id}`} studentId={selected.id} role={session.user.role} csrfToken={session.csrfToken} expired={expired} t={t}/><ProgressNotes key={`notes:${session.user.id}:${selected.id}`} studentId={selected.id} role={session.user.role} csrfToken={session.csrfToken} expired={expired} t={t}/></section> : <>{role === 'admin' && !studentId && <AdminActionQueue key={session.user.id} expired={expired} t={t} follow={follow}/>}<div className="directory-tools"><div><label htmlFor="search">{t('Search students', '搜尋學員')}</label><input id="search" type="search" value={query} onChange={e => setQuery(e.target.value)} placeholder={t('Name or swimming level', '姓名或游泳程度')}/></div><button className="outline" onClick={loadStudents} disabled={loading}>{t('Refresh', '重新載入')}</button></div>{loading ? <p role="status">{t('Loading students…', '載入學員中…')}</p> : loadError ? <div className="panel"><p role="alert">{t('Could not load the directory.', '未能載入學員名單。')}</p><button onClick={loadStudents}>{t('Retry', '重試')}</button></div> : <><p className="result-count" role="status">{visible.length} {t('students', '位學員')}</p><div className="student-grid">{visible.map(student => <a className="student-card" key={student.id} href={routePath({ view: 'students', studentId: student.id })} onClick={event => follow(event, { view: 'students', studentId: student.id })}><span className="avatar" aria-hidden="true">{student.name.slice(0, 1)}</span><span><strong>{student.name}</strong><span className="muted">{student.level}</span></span><span className="arrow" aria-hidden="true">↗</span></a>)}</div>{!visible.length && <div className="panel empty"><h2>{t('No students to display', '沒有可顯示的學員')}</h2><p>{query ? t('Try another name or level.', '請嘗試其他姓名或程度。') : t('Your school will link student records to your account.', '學校會將相關學員資料連結至你的帳戶。')}</p></div>}</>}</>}
    </>}</section></div>}
    </main><footer>SwimCoach Pro <span>•</span> {t('Connected school, confident swimmers.', '連繫學校，培育自信泳手。')}</footer></div>;
}
