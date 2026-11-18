import { useCallback, useEffect, useState, type MouseEvent } from 'react';
import ClassRoster from './ClassRoster';
import { APIError } from './api';
import type { components } from './generated/api';
import { routePath, type Route } from './navigation';
import { classLabel, type ClassSession } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Report = components['schemas']['MissingAttendanceList'];
type Follow = (event: MouseEvent<HTMLAnchorElement>, next: Route) => void;
const reportRoute: Route = { view: 'missing-attendance' };
const scheduleRoute: Route = { view: 'schedule' };

export default function MissingAttendance({ expired, t, follow }: Pick<ProtectedProps, 'expired' | 't'> & { follow: Follow }) {
  const api = useProtectedRequest(expired);
  const [result, setResult] = useState<Report>();
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  const load = useCallback(async (cursor?: string) => {
    setLoading(true);
    setFailed(false);
    if (!cursor) setResult(undefined);
    const query = cursor ? `?${new URLSearchParams({ cursor })}` : '';
    try {
      const report = await api<Report>(`/admin/missing-attendance${query}`);
      setResult(previous => cursor && previous ? { ...report, items: [...previous.items, ...report.items] } : report);
    } catch (error) {
      if (isAborted(error)) return;
      setFailed(true);
    }
    setLoading(false);
  }, [api]);

  useEffect(() => { void load(); }, [load]);
  const cursor = result?.nextCursor;
  return <section className="panel">
    <h1>{t('Classes with missing attendance', '缺少出席記錄的課堂')}</h1>
    <p className="muted">{t('Dates and times are shown in Hong Kong time.', '日期及時間以香港時間顯示。')}</p>
    <button type="button" className="outline" disabled={loading} onClick={() => void load()}>
      {failed && !result ? t('Retry report', '重試載入報告') : t('Refresh report', '重新載入報告')}
    </button>
    {loading && <p role="status">{t('Loading missing attendance…', '正在載入缺少的出席記錄…')}</p>}
    {failed && <p role="alert">{t('Could not load missing attendance.', '未能載入缺少的出席記錄。')}</p>}
    {result && (result.items.length === 0
      ? <p role="status">{t('No classes with missing attendance.', '沒有缺少出席記錄的課堂。')}</p>
      : <ul className="link-list">{result.items.map(lesson => {
        const destination: Route = { view: 'schedule', classId: lesson.id };
        return <li key={lesson.id}>
          <div><strong>{classLabel(lesson)}</strong>
            <p>{t('Unmarked bookings', '未記錄的預約')} · {lesson.unmarkedBookings}</p></div>
          <a href={routePath(destination)} onClick={event => follow(event, destination)}>{t('Open roster', '開啟課堂名單')}</a>
        </li>;
      })}</ul>)}
    {cursor && <button type="button" className="outline" disabled={loading} onClick={() => void load(cursor)}>
      {t('Load more classes', '載入更多課堂')}
    </button>}
  </section>;
}

export function RosterPage({ classId, admin, follow, ...props }: ProtectedProps & { classId: string; admin: boolean; follow: Follow }) {
  const { expired, t } = props;
  const api = useProtectedRequest(expired);
  const [lesson, setLesson] = useState<ClassSession | 'unavailable' | 'error'>();
  const back = admin ? reportRoute : scheduleRoute;
  const load = useCallback(async () => {
    setLesson(undefined);
    try {
      setLesson(await api<ClassSession>(`/classes/${encodeURIComponent(classId)}`));
    } catch (error) {
      if (!isAborted(error)) setLesson(error instanceof APIError && (error.status === 403 || error.status === 404) ? 'unavailable' : 'error');
    }
  }, [api, classId]);
  useEffect(() => { void load(); }, [load]);

  return <section className="panel">
    <a href={routePath(back)} onClick={event => follow(event, back)}>
      {admin ? t('Back to missing attendance', '返回缺少的出席記錄') : t('Back to schedule', '返回時間表')}
    </a>
    <h1>{t('Lesson roster', '課堂名單')}</h1>
    {!lesson ? <p role="status">{t('Loading lesson…', '正在載入課堂…')}</p>
      : lesson === 'unavailable' ? <p role="alert">{t('This lesson is unavailable.', '無法存取此課堂。')}</p>
      : lesson === 'error' ? <>
        <p role="alert">{t('Could not load the lesson.', '未能載入課堂。')}</p>
        <button type="button" className="outline" onClick={() => void load()}>{t('Retry lesson', '重試載入課堂')}</button>
      </> : <>
        <p>{classLabel(lesson)}</p>
        <ClassRoster {...props} classId={classId}/>
      </>}
  </section>;
}
