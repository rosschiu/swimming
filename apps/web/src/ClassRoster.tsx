import { useEffect, useRef, useState, type MouseEvent } from 'react';
import { APIError } from './api';
import type { components, operations } from './generated/api';
import { routePath, type Route } from './navigation';
import { hkDate, hkTime } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Roster = operations['getClassRoster']['responses'][200]['content']['application/json'];
type RosterEntry = components['schemas']['RosterEntry'];

export default function ClassRoster({ classId, csrfToken, expired, t, follow }: ProtectedProps & {
  classId: string;
  follow?: (event: MouseEvent<HTMLAnchorElement>, next: Route) => void;
}) {
  const api = useProtectedRequest(expired);
  const path = `/classes/${encodeURIComponent(classId)}/roster`;
  const [result, setResult] = useState<Roster | 'unavailable' | 'error' | null>(null);
  const [revision, setRevision] = useState(0);
  const [reviewing, setReviewing] = useState<RosterEntry | null>(null);
  const reviewTrigger = useRef<HTMLButtonElement>(null);
  const confirmButton = useRef<HTMLButtonElement>(null);
  const focusRecordedBooking = useRef<string | null>(null);
  const focusRosterFeedback = useRef(false);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState('');
  const [notice, setNotice] = useState('');
  const [, setClock] = useState(0);
  const opensAt = result && typeof result === 'object' ? result.attendanceOpensAt : undefined;
  const remaining = opensAt ? Date.parse(opensAt) - Date.now() : 0;
  const isOpen = !!opensAt && remaining <= 0;

  useEffect(() => {
    void api<Roster>(path)
      .then(setResult)
      .catch(error => {
        if (!isAborted(error)) setResult(error instanceof APIError && (error.status === 403 || error.status === 404) ? 'unavailable' : 'error');
      });
  }, [api, path, revision]);

  useEffect(() => {
    if (remaining <= 0) return;
    const timer = window.setTimeout(() => setClock(tick => tick + 1), Math.min(remaining, 2147483647));
    return () => window.clearTimeout(timer);
  }, [remaining]);

  function reload() {
    setResult(null);
    setReviewing(null);
    setFailure('');
    setNotice('');
    setRevision(previous => previous + 1);
  }

  function focusFeedback(element: HTMLParagraphElement | null) {
    if (element && focusRosterFeedback.current) {
      focusRosterFeedback.current = false;
      element.focus();
    }
  }

  async function markPresent(entry: RosterEntry) {
    if (saving) return;
    setSaving(true);
    setFailure('');
    setNotice('');
    try {
      const attendance = await api<components['schemas']['Attendance']>(`${path}/${encodeURIComponent(entry.bookingId)}/attendance`, {
        method: 'PUT', headers: { 'X-CSRF-Token': csrfToken },
      });
      if (document.activeElement === confirmButton.current) {
        focusRecordedBooking.current = entry.bookingId;
      }
      setResult(previous => previous && typeof previous === 'object' ? {
        ...previous,
        items: previous.items.map(item => item.bookingId === entry.bookingId ? { ...item, attendance } : item),
      } : previous);
      setReviewing(null);
      setNotice(t(`Attendance recorded for ${entry.studentName}.`, `已記錄 ${entry.studentName} 的出席。`));
    } catch (error) {
      if (isAborted(error)) return;
      if (error instanceof APIError && (error.status === 403 || error.status === 404)) {
        focusRosterFeedback.current = document.activeElement === confirmButton.current;
        setReviewing(null);
        setResult('unavailable');
      } else if (error instanceof APIError && error.code === 'attendance_not_open') {
        focusRosterFeedback.current = document.activeElement === confirmButton.current;
        setReviewing(null);
        setFailure(t('Attendance has yet to open. Reload the roster for the current opening time.', '出席記錄尚未開放，請重新載入名單查看最新的開放時間。'));
      } else if (error instanceof APIError && error.code === 'credit_unavailable') {
        setFailure(t('Could not record attendance because the reserved lesson credit is unavailable.', '預留的課堂點數無法使用，未能記錄出席。'));
      } else {
        setFailure(t('Could not record attendance. Try again.', '未能記錄出席，請重試。'));
      }
    } finally {
      setSaving(false);
    }
  }

  return <section className="panel" aria-label={t('Class roster', '課堂名單')}>
    <div className="page-heading">
      <h3>{t('Class roster', '課堂名單')}</h3>
      <button className="outline" disabled={result === null || saving} onClick={reload}>
        {t('Reload roster', '重新載入名單')}
      </button>
    </div>
    {failure && <p className="error roster-feedback" ref={focusFeedback} tabIndex={-1} role="alert">{failure}</p>}
    {notice && <p className="success" role="status">{notice}</p>}
    {result === null ? <p role="status">{t('Loading class roster…', '正在載入課堂名單…')}</p>
      : result === 'unavailable' ? <p className="roster-feedback" ref={focusFeedback} tabIndex={-1} role="alert">{t('This roster is unavailable for your current access.', '目前權限無法存取此課堂名單。')}</p>
      : result === 'error' ? <p role="alert">{t('Could not load the roster. Reload to retry.', '未能載入名單，請重新載入。')}</p>
      : result.items.length === 0 ? <p role="status">{t('No reserved places for this class.', '此課堂尚無預約。')}</p>
      : <>
        {!isOpen && opensAt && <p className="muted">{t('Attendance opens', '出席記錄開放時間')} <time dateTime={opensAt}>{hkDate(new Date(opensAt))} {hkTime(opensAt)}</time> {t('Hong Kong time', '香港時間')}</p>}
        <ul className="link-list roster-list">{result.items.map(entry => {
          const evidence = entry.attendance?.evidence;
          const audit: Route = { view: 'audit', auditEntityId: entry.bookingId };
          const markedAt = entry.attendance?.markedAt;
          return <li key={entry.bookingId}>
          <div><strong>{entry.studentName}</strong><span className="muted">{entry.level}</span>
            {evidence && markedAt && <>
              <p>{t('Recorded by', '記錄者')} {evidence.recordedBy} · <time dateTime={markedAt}>{hkDate(new Date(markedAt))} {hkTime(markedAt)}</time> {t('Hong Kong time', '香港時間')}</p>
              {evidence.hasAudit && <a href={routePath(audit)} onClick={event => follow?.(event, audit)}>{t('View attendance audit', '查看出席操作紀錄')}</a>}
            </>}
          </div>
          <div className="roster-actions">
            {entry.attendance ? <span className="tag" tabIndex={-1} ref={status => {
              if (status && focusRecordedBooking.current === entry.bookingId) {
                focusRecordedBooking.current = null;
                status.focus();
              }
            }}>{t('Present', '已出席')}</span> : <span className="tag">{t('Reserved', '已預約')}</span>}
            {!entry.attendance && <button
              className="outline" disabled={!isOpen || saving} aria-expanded={reviewing?.bookingId === entry.bookingId} aria-label={t(`Mark present ${entry.studentName}`, `標記 ${entry.studentName} 出席`)}
              onClick={event => {
                reviewTrigger.current = event.currentTarget;
                setReviewing(reviewing?.bookingId === entry.bookingId ? null : entry);
                setFailure('');
                setNotice('');
              }}>
              {t('Mark present', '標記出席')}
            </button>}
            {reviewing?.bookingId === entry.bookingId && <div>
              <p>{t(`Mark ${entry.studentName} present and use one reserved lesson credit?`, `標記 ${entry.studentName} 出席並使用一個預留的課堂點數？`)}</p>
              <div className="form-actions">
                <button ref={confirmButton} aria-disabled={saving} onClick={() => void markPresent(entry)}>{t('Confirm present', '確認出席')}</button>
                <button className="outline" disabled={saving} onClick={() => {
                  setReviewing(null);
                  setFailure('');
                  reviewTrigger.current?.focus();
                }}>{t('Cancel', '取消')}</button>
              </div>
            </div>}
          </div>
        </li>;
        })}</ul>
      </>}
  </section>;
}
