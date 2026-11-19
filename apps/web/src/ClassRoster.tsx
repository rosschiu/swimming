import { useEffect, useRef, useState, type MouseEvent } from 'react';
import { APIError } from './api';
import type { components, operations } from './generated/api';
import { routePath, type Route } from './navigation';
import { creditLabel, outcomeLabel } from './attendanceLabels';
import LessonFeedback from './LessonFeedback';
import { hkDate, hkTime } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Roster = operations['getClassRoster']['responses'][200]['content']['application/json'];
type RosterEntry = components['schemas']['RosterEntry'];
type Attendance = components['schemas']['Attendance'];
type Outcome = Attendance['status'];

function Correction({ entry, path, csrfToken, expired, t, onSaved }: ProtectedProps & { entry: RosterEntry & { attendance: Attendance }; path: string; onSaved: (attendance: Attendance) => void }) {
  const api = useProtectedRequest(expired);
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<Outcome | ''>('');
  const [reason, setReason] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const intent = useRef<{ fingerprint: string; key: string } | null>(null);
  const choices = (['present', 'absent', 'ill'] as Outcome[]).filter(value => value !== entry.attendance.status);
  async function save() {
    if (!status || !reason.trim() || saving) return;
    const body = { status, reason: reason.trim(), revision: entry.attendance.revision };
    const fingerprint = JSON.stringify(body);
    if (intent.current?.fingerprint !== fingerprint) intent.current = { fingerprint, key: crypto.randomUUID() };
    setSaving(true);
    setError('');
    try {
      onSaved(await api<Attendance>(`${path}/${encodeURIComponent(entry.bookingId)}/attendance/corrections`, {
        method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ ...body, idempotencyKey: intent.current.key }),
      }));
      setOpen(false); setStatus(''); setReason(''); intent.current = null;
    } catch (cause) {
      if (isAborted(cause)) return;
      const code = cause instanceof APIError ? cause.code : undefined;
      setError(code === 'attendance_changed' ? t('Someone changed this attendance. Reload the roster before correcting it.', '出席記錄已被更改，請重新載入名單後再更正。')
        : code === 'correction_closed' ? t('The correction window for this lesson has closed.', '此課堂的更正期限已過。')
        : code === 'credit_not_reversible' ? t('The returned credit has already been used or restored, so it cannot be taken back.', '退回的堂數已被使用或恢復，無法收回。')
        : code === 'attendance_not_open' ? t('Absence and illness can be recorded once the lesson has ended.', '課堂結束後才可記錄缺席或病假。')
        : t('Could not save the correction. Your entries are preserved; try again.', '未能儲存更正，已保留輸入內容，請再試。'));
    } finally { setSaving(false); }
  }
  if (!open) return <button className="text-button" onClick={() => setOpen(true)} aria-label={t(`Correct attendance ${entry.studentName}`, `更正 ${entry.studentName} 的出席`)}>{t('Correct', '更正')}</button>;
  return <div className="correction">
    <fieldset className="choice-group">
      <legend>{t(`Change ${entry.studentName} to`, `將 ${entry.studentName} 更改為`)}</legend>
      {choices.map(value => <label key={value} className={status === value ? 'choice-card choice-selected' : 'choice-card'}>
        <input type="radio" name={`correct-${entry.bookingId}`} value={value} checked={status === value} onChange={() => setStatus(value)}/>
        <span className="choice-body">{outcomeLabel(value, t)}</span>
      </label>)}
    </fieldset>
    <label htmlFor={`correct-reason-${entry.bookingId}`}>{t('Reason for correction', '更正原因')}</label>
    <input id={`correct-reason-${entry.bookingId}`} maxLength={500} value={reason} onChange={event => setReason(event.target.value)}/>
    <p className="muted">{t('Staff only. Do not include medical details.', '只供職員查看，請勿填寫醫療資料。')}</p>
    <div className="form-actions">
      <button disabled={!status || !reason.trim() || saving} onClick={() => void save()}>{saving ? t('Saving…', '儲存中…') : t('Save correction', '儲存更正')}</button>
      <button className="outline" disabled={saving} onClick={() => { setOpen(false); setError(''); }}>{t('Cancel', '取消')}</button>
    </div>
    {error && <p role="alert" className="error">{error}</p>}
  </div>;
}

export default function ClassRoster({ classId, csrfToken, expired, t, follow }: ProtectedProps & {
  classId: string;
  follow?: (event: MouseEvent<HTMLAnchorElement>, next: Route) => void;
}) {
  const api = useProtectedRequest(expired);
  const path = `/classes/${encodeURIComponent(classId)}/roster`;
  const [result, setResult] = useState<Roster | 'unavailable' | 'error' | null>(null);
  const [revision, setRevision] = useState(0);
  const [reviewing, setReviewing] = useState<{ entry: RosterEntry; status: Outcome } | null>(null);
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
  const roster = result && typeof result === 'object' ? result : undefined;
  const absenceOpen = !!roster && Date.parse(roster.absenceOpensAt) <= Date.now();
  const correctable = !!roster && Date.now() <= Date.parse(roster.correctionClosesAt);
  function updateEntry(bookingId: string, attendance: Attendance) {
    setResult(previous => previous && typeof previous === 'object' ? {
      ...previous,
      items: previous.items.map(item => item.bookingId === bookingId ? { ...item, attendance } : item),
    } : previous);
  }

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

  async function mark(entry: RosterEntry, status: Outcome) {
    if (saving) return;
    setSaving(true);
    setFailure('');
    setNotice('');
    try {
      const attendance = await api<components['schemas']['Attendance']>(`${path}/${encodeURIComponent(entry.bookingId)}/attendance`, {
        method: 'PUT', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ status }),
      });
      if (document.activeElement === confirmButton.current) {
        focusRecordedBooking.current = entry.bookingId;
      }
      updateEntry(entry.bookingId, attendance);
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
            }}>{outcomeLabel(entry.attendance.status, t)}</span> : <span className="tag">{t('Reserved', '已預約')}</span>}
            {entry.attendance && <span className="muted">{creditLabel(entry.attendance.creditOutcome, t)}{entry.attendance.revision > 1 ? ` · ${t('corrected', '已更正')}` : ''}</span>}
            {entry.attendance && correctable && <Correction entry={{ ...entry, attendance: entry.attendance }} path={path} csrfToken={csrfToken} expired={expired} t={t} onSaved={attendance => {
              updateEntry(entry.bookingId, attendance);
              setNotice(t(`Attendance corrected for ${entry.studentName}.`, `已更正 ${entry.studentName} 的出席。`));
            }}/>}
            {entry.attendance?.status === 'present' && <LessonFeedback studentId={entry.studentId} studentName={entry.studentName} bookingId={entry.bookingId} csrfToken={csrfToken} expired={expired} t={t}
              onShared={() => setNotice(t(`Feedback shared with ${entry.studentName}'s family.`, `已向 ${entry.studentName} 的家長分享回饋。`))}/>}
            {!entry.attendance && (['present', ...(absenceOpen ? ['absent', 'ill'] : [])] as Outcome[]).map(status => <button key={status}
              className="outline" disabled={(status === 'present' ? !isOpen : !absenceOpen) || saving}
              aria-expanded={reviewing?.entry.bookingId === entry.bookingId && reviewing.status === status}
              aria-label={{ present: t(`Mark present ${entry.studentName}`, `標記 ${entry.studentName} 出席`), absent: t(`Mark absent ${entry.studentName}`, `標記 ${entry.studentName} 缺席`), ill: t(`Mark ill ${entry.studentName}`, `標記 ${entry.studentName} 病假`) }[status]}
              onClick={event => {
                reviewTrigger.current = event.currentTarget;
                setReviewing(reviewing?.entry.bookingId === entry.bookingId && reviewing.status === status ? null : { entry, status });
                setFailure('');
                setNotice('');
              }}>
              {{ present: t('Mark present', '標記出席'), absent: t('Mark absent', '標記缺席'), ill: t('Mark ill', '標記病假') }[status]}
            </button>)}
            {reviewing?.entry.bookingId === entry.bookingId && <div>
              <p>{reviewing.status === 'present'
                ? t(`Mark ${entry.studentName} present and use one reserved lesson credit?`, `標記 ${entry.studentName} 出席並使用一個預留的課堂點數？`)
                : t(`Mark ${entry.studentName} ${reviewing.status === 'ill' ? 'ill' : 'absent'}? The reserved credit is used or returned under the package's ${reviewing.status === 'ill' ? 'illness' : 'absence'} policy.`, `標記 ${entry.studentName} ${reviewing.status === 'ill' ? '病假' : '缺席'}？預留堂數將按套票的${reviewing.status === 'ill' ? '病假' : '缺席'}政策使用或退回。`)}</p>
              <div className="form-actions">
                <button ref={confirmButton} aria-disabled={saving} onClick={() => void mark(entry, reviewing.status)}>{{ present: t('Confirm present', '確認出席'), absent: t('Confirm absent', '確認缺席'), ill: t('Confirm ill', '確認病假') }[reviewing.status]}</button>
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
