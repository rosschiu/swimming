import { useEffect, useState, type MouseEvent, type ReactNode } from 'react';
import type { components } from './generated/api';
import AdminActionQueue from './AdminActionQueue';
import { routePath, type Route } from './navigation';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Summary = components['schemas']['Home'];
type HomeStudent = components['schemas']['HomeStudent'];
type HomeClass = components['schemas']['HomeClass'];
type Lesson = components['schemas']['ClassSession'];
type Follow = (event: MouseEvent<HTMLAnchorElement>, next: Route) => void;
type Props = Pick<ProtectedProps, 'expired' | 't'> & { name: string; follow: Follow };

function RouteLink({ to, follow, className, children }: { to: Route; follow: Follow; className?: string; children: ReactNode }) {
  return <a className={className} href={routePath(to)} onClick={event => follow(event, to)}>{children}</a>;
}

const hkYear = (value: Date) => new Intl.DateTimeFormat('en', { timeZone: 'Asia/Hong_Kong', year: 'numeric' }).format(value);
function lessonDay(value: string, t: ProtectedProps['t']) {
  const date = new Date(value);
  const year = hkYear(date) === hkYear(new Date()) ? {} : { year: 'numeric' } as const;
  return new Intl.DateTimeFormat(t('en-GB', 'zh-HK'), { timeZone: 'Asia/Hong_Kong', weekday: 'short', day: 'numeric', month: 'short', ...year }).format(date);
}
function lessonTime(lesson: Pick<Lesson, 'startsAt' | 'endsAt'>) {
  const format = new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Hong_Kong', hour: '2-digit', minute: '2-digit', hour12: false });
  return `${format.format(new Date(lesson.startsAt))}–${format.format(new Date(lesson.endsAt))}`;
}
function expiryDate(value: string, t: ProtectedProps['t']) {
  return new Intl.DateTimeFormat(t('en-GB', 'zh-HK'), { timeZone: 'UTC', day: 'numeric', month: 'short', year: 'numeric' }).format(new Date(`${value}T00:00:00Z`));
}

function LessonSummary({ lesson, t }: { lesson: Lesson; t: ProtectedProps['t'] }) {
  return <div className="lesson-summary">
    <p className="lesson-when"><strong>{lessonDay(lesson.startsAt, t)}</strong> <span>{lessonTime(lesson)}</span></p>
    <p className="lesson-what">{lesson.classTypeName}</p>
    <p className="muted">{lesson.venueName} · {lesson.coachName}</p>
  </div>;
}

function SwimmerCard({ student, guardian, follow, t }: { student: HomeStudent; guardian: boolean; follow: Follow; t: ProtectedProps['t'] }) {
  const profile: Route = { view: 'students', studentId: student.id };
  const credits = student.credits;
  return <section className="home-card" aria-labelledby={`swimmer-${student.id}`}>
    <div className="home-card-head">
      <span className="avatar" aria-hidden="true">{student.name.slice(0, 1)}</span>
      <div><h2 id={`swimmer-${student.id}`}><RouteLink className="name-link" to={profile} follow={follow}>{student.name}</RouteLink></h2><p className="muted">{student.level}</p></div>
    </div>
    <p className="eyebrow">{t('NEXT LESSON', '下一堂')}</p>
    {student.nextLesson
      ? <RouteLink className="lesson-link" to={{ view: 'students', studentId: student.id, bookingId: student.nextLesson.id }} follow={follow}><LessonSummary lesson={student.nextLesson.class} t={t}/></RouteLink>
      : <p className="muted">{t('No upcoming lessons booked.', '暫未預約任何課堂。')}</p>}
    {student.upcomingCount > 1 && <p className="muted">{t(`${student.upcomingCount - 1} more booked after this`, `之後還有 ${student.upcomingCount - 1} 堂已預約`)}</p>}
    {guardian && credits && <p className="credit-line">
      <strong>{t(`${credits.available} ${credits.available === 1 ? 'lesson' : 'lessons'} left to book`, `尚餘 ${credits.available} 堂可預約`)}</strong>
      {credits.nextExpiry && <span className="muted"> · {t(`use by ${expiryDate(credits.nextExpiry, t)}`, `${expiryDate(credits.nextExpiry, t)} 前使用`)}</span>}
    </p>}
    {student.pendingChanges > 0 && <p className="chip">{t(`${student.pendingChanges} lesson change awaiting the coach`, `${student.pendingChanges} 項調課申請待教練處理`)}</p>}
    <div className="home-actions">
      <RouteLink className="button-link" to={profile} follow={follow}>{guardian ? t('Lessons and booking', '課堂及預約') : t('My lessons and progress', '我的課堂及進度')}</RouteLink>
      {guardian && credits && credits.available === 0 && <RouteLink className="button-link outline-link" to={{ view: 'packages' }} follow={follow}>{t('Buy lessons', '購買課堂')}</RouteLink>}
    </div>
  </section>;
}

function ClassList({ classes, admin, follow, t }: { classes: HomeClass[]; admin: boolean; follow: Follow; t: ProtectedProps['t'] }) {
  if (!classes.length) return <p className="muted">{admin ? t('No lessons today.', '今天沒有課堂。') : t('No upcoming lessons assigned to you.', '你暫時沒有獲分配的課堂。')}</p>;
  let day = '';
  return <ul className="class-list">{classes.map(item => {
    const label = lessonDay(item.class.startsAt, t);
    const heading = label !== day ? (day = label) : null;
    return <li key={item.class.id}>
      {heading && !admin && <p className="eyebrow class-day">{heading}</p>}
      <RouteLink className="class-row" to={{ view: 'schedule', classId: item.class.id }} follow={follow}>
        <span className="class-time">{lessonTime(item.class)}</span>
        <span className="class-body"><strong>{item.class.classTypeName}</strong><span className="muted">{item.class.venueName}{admin ? ` · ${item.class.coachName}` : ''}</span></span>
        <span className="class-count">{t(`${item.booked} booked`, `${item.booked} 人`)}{item.unmarked > 0 && new Date(item.class.startsAt) <= new Date() && <span className="chip">{t(`${item.unmarked} to mark`, `${item.unmarked} 待點名`)}</span>}</span>
      </RouteLink>
    </li>;
  })}</ul>;
}

export default function Home({ expired, t, name, follow }: Props) {
  const api = useProtectedRequest(expired);
  const [summary, setSummary] = useState<Summary | 'error' | null>(null);
  async function load() {
    setSummary(null);
    try { setSummary(await api<Summary>('/home')); }
    catch (error) { if (!isAborted(error)) setSummary('error'); }
  }
  useEffect(() => { void load(); }, [api]);

  const greeting = <h1>{t(`Hi, ${name}`, `${name}，你好`)}</h1>;
  if (summary === null) return <>{greeting}<p role="status">{t('Loading…', '載入中…')}</p></>;
  if (summary === 'error') return <>{greeting}<div className="panel"><p role="alert">{t('Could not load your home page.', '未能載入主頁。')}</p><button onClick={() => void load()}>{t('Retry', '重試')}</button></div></>;
  const requests: Route = { view: 'lesson-changes', lessonChangeStatus: 'pending' };
  return <div className="home">
    {greeting}
    {summary.unreadNotifications > 0 && <RouteLink className="notice-banner" to={{ view: 'notifications' }} follow={follow}>
      {t(`${summary.unreadNotifications} unread ${summary.unreadNotifications === 1 ? 'update' : 'updates'}`, `${summary.unreadNotifications} 則未讀通知`)}<span aria-hidden="true">›</span>
    </RouteLink>}
    {(summary.role === 'parent' || summary.role === 'student') && (summary.students.length
      ? summary.students.map(student => <SwimmerCard key={student.id} student={student} guardian={summary.role === 'parent'} follow={follow} t={t}/>)
      : <div className="panel empty"><h2>{t('No swimmers linked yet', '尚未連結學員')}</h2><p>{t('Your school will link your swimmer to this account.', '學校會將學員連結至此帳戶。')}</p></div>)}
    {summary.role === 'coach' && <>
      {summary.pendingLessonChanges > 0 && <RouteLink className="notice-banner" to={requests} follow={follow}>
        {t(`${summary.pendingLessonChanges} lesson change ${summary.pendingLessonChanges === 1 ? 'request needs' : 'requests need'} your decision`, `${summary.pendingLessonChanges} 項調課申請待你決定`)}<span aria-hidden="true">›</span>
      </RouteLink>}
      <section className="home-card" aria-labelledby="coach-lessons"><h2 id="coach-lessons">{t('Your lessons', '你的課堂')}</h2><ClassList classes={summary.classes} admin={false} follow={follow} t={t}/></section>
    </>}
    {summary.role === 'admin' && <>
      <AdminActionQueue expired={expired} t={t} follow={follow}/>
      <section className="home-card section-gap" aria-labelledby="today-lessons"><h2 id="today-lessons">{t('Today’s lessons', '今日課堂')}</h2><ClassList classes={summary.classes} admin follow={follow} t={t}/></section>
    </>}
  </div>;
}
