import { useCallback, useEffect, useId, useRef, useState, type MouseEvent } from 'react';
import { APIError } from './api';
import type { components } from './generated/api';
import { routePath, type Route } from './navigation';
import { classLabel, hkDate, hkTime, rangeEnd } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps, type Translate } from './useProtectedRequest';

type LessonChange = components['schemas']['LessonChange'];
type ChangeList = components['schemas']['LessonChangeList'];
type Options = components['schemas']['LessonChangeOptions'];
type ChangeRequest = components['schemas']['LessonChangeRequest'];
type Decision = components['schemas']['LessonChangeDecision'];

function errorMessage(error: unknown, t: Translate, fallback: string, deciding = false) {
  if (error instanceof APIError && error.status === 409) {
    switch (error.code) {
      case 'class_full': return t('The replacement lesson is full. Try again if a place opens.', '替換課堂已滿。若有空位，請再試。');
      case 'student_overlap': return t('Another booking overlaps the replacement lesson.', '另一項預約與替換課堂的時間重疊。');
      case 'credit_unavailable': return t('The reserved credit cannot cover the replacement lesson.', '預留的點數無法用於替換課堂。');
      case 'target_changed': return deciding
        ? t('The replacement lesson changed. Reject this request so the guardian can review the new details and submit again.', '替換課堂已更改。請拒絕此申請，讓家長查看新資料並重新申請。')
        : t('The replacement lesson changed. Search again to review the latest details.', '替換課堂已更改。請重新搜尋以查看最新資料。');
      case 'change_cutoff': return t('The deadline to request a change has passed.', '已過調課申請期限。');
      case 'change_pending': return t('This booking already has a pending change request.', '此預約已有待處理的調課申請。');
    }
    return t('This lesson or request has changed. Refresh to check its latest status.', '課堂或申請已更改。請重新載入以查看最新狀態。');
  }
  if (error instanceof APIError && (error.status === 403 || error.status === 404)) {
    return t('This lesson or request is unavailable for your current access.', '目前權限無法存取此課堂或申請。');
  }
  return fallback;
}

function statusLabel(status: LessonChange['status'], t: Translate) {
  return { pending: t('Pending', '待處理'), approved: t('Approved', '已批准'), rejected: t('Rejected', '已拒絕') }[status];
}

function Timestamp({ value }: { value: string }) {
  return <time dateTime={value}>{hkDate(new Date(value))} {hkTime(value)}</time>;
}

const changeHistory: Route = { view: 'lesson-changes' };

export default function LessonChanges(props: ProtectedProps & {
  role: 'parent' | 'coach' | 'admin';
  initialPending?: boolean;
  follow: (event: MouseEvent<HTMLAnchorElement>, next: Route) => void;
}) {
  const { expired, t, role } = props;
  const api = useProtectedRequest(expired);
  const [result, setResult] = useState<ChangeList>();
  const [failed, setFailed] = useState(false);
  const [loading, setLoading] = useState(true);
  const [pendingOnly, setPendingOnly] = useState(props.initialPending || role === 'coach');
  const [decision, setDecision] = useState<Decision['decision']>();

  const load = useCallback(async (cursor?: string) => {
    setLoading(true);
    setFailed(false);
    if (!cursor) {
      setDecision(undefined);
      setResult(undefined);
    }
    const query = new URLSearchParams();
    if (pendingOnly) query.set('status', 'pending');
    if (cursor) query.set('cursor', cursor);
    try {
      const page = await api<ChangeList>(`/lesson-changes${query.size ? `?${query}` : ''}`);
      setResult(previous => cursor && previous ? { ...page, items: [...previous.items, ...page.items] } : page);
    } catch (error) {
      if (isAborted(error)) return;
      setFailed(true);
    }
    setLoading(false);
  }, [api, pendingOnly]);

  useEffect(() => { void load(); }, [load]);

  function updateDecision(updated: LessonChange) {
    if (updated.status === 'pending') return;
    setDecision(updated.status);
    setResult(previous => previous && ({
      ...previous,
      items: pendingOnly ? previous.items.filter(item => item.id !== updated.id)
        : previous.items.map(item => item.id === updated.id ? updated : item),
    }));
  }

  const cursor = result?.nextCursor;
  return <section className="panel">
    <h1>{role === 'admin' ? t('Lesson change history', '調課記錄') : t('Lesson changes', '調課申請')}</h1>
    <p className="muted">{t('Dates and times are shown in Hong Kong time.', '日期及時間以香港時間顯示。')}</p>
    {role !== 'coach' && pendingOnly && <a className="text-button" href={routePath(changeHistory)} onClick={event => props.follow(event, changeHistory)}>{t('View all lesson changes', '查看所有調課申請')}</a>}
    {role === 'coach' && <label className="checkbox-label"><input type="checkbox" checked={pendingOnly} disabled={loading} onChange={event => setPendingOnly(event.target.checked)}/> {t('Show pending requests only', '只顯示待處理申請')}</label>}
    <button type="button" className="outline" disabled={loading} onClick={() => void load()}>{t('Refresh requests', '重新載入申請')}</button>
    {decision && <p role="status">{decision === 'approved' ? t('Lesson change approved.', '已批准調課申請。') : t('Lesson change rejected.', '已拒絕調課申請。')}</p>}
    {failed && <p role="alert">{t('Could not load lesson changes.', '未能載入調課申請。')}</p>}
    {loading && <p role="status">{t('Loading lesson changes…', '正在載入調課申請…')}</p>}
    {result && (result.items.length === 0 ? <p role="status">{pendingOnly ? t('No pending lesson change requests.', '沒有待處理的調課申請。') : t('No lesson change requests for this view.', '此檢視沒有調課申請。')}</p>
      : <ul className="link-list">{result.items.map(item => <li key={item.id}>
        <div>
          <strong>{item.studentName}</strong> <span className="tag">{statusLabel(item.status, t)}</span>
          <p>{t('Original lesson', '原定課堂')} · {classLabel(item.originalClass)}</p>
          <p>{t('Requested lesson', '申請的課堂')} · {classLabel(item.targetClass)}</p>
          <p>{t('Requested at', '申請時間')} <Timestamp value={item.requestedAt}/>{role === 'admin' && <> · {t('Requester', '申請人')} {item.requestedBy}</>}</p>
          {item.decidedAt && <p>{t('Decided at', '處理時間')} <Timestamp value={item.decidedAt}/>{role === 'admin' && item.decidedBy && <> · {t('Decided by', '處理人')} {item.decidedBy}</>}</p>}
          {item.decisionReason && <p>{t('Decision reason', '處理原因')} · {item.decisionReason}</p>}
          {role === 'coach' && item.status === 'pending' && <CoachDecision {...props} changeId={item.id} onDecision={updateDecision}/>}
        </div>
      </li>)}</ul>)}
    {cursor && <button type="button" className="outline" disabled={loading} onClick={() => void load(cursor)}>{t('Load more requests', '載入更多申請')}</button>}
  </section>;
}

function CoachDecision({ changeId, onDecision, csrfToken, expired, t }: ProtectedProps & { changeId: string; onDecision: (item: LessonChange) => void }) {
  const id = useId();
  const api = useProtectedRequest(expired);
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [error, setError] = useState('');

  async function decide(decision: Decision['decision']) {
    setBusy(true);
    setError('');
    const input: Decision = { decision, reason: reason.trim() || undefined };
    try {
      onDecision(await api<LessonChange>(`/lesson-changes/${encodeURIComponent(changeId)}/decision`, {
        method: 'PUT', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(input),
      }));
    } catch (cause) {
      if (!isAborted(cause)) {
        setUnavailable(cause instanceof APIError && (cause.status === 403 || cause.status === 404));
        setError(errorMessage(cause, t, t('Could not save the decision. Try again.', '未能儲存處理結果。請再試。'), true));
      }
    } finally { setBusy(false); }
  }

  return <div>
    <label htmlFor={id}>{t('Decision reason (optional)', '處理原因（選填）')}</label>
    <input id={id} value={reason} onChange={event => setReason(event.target.value)} maxLength={500} disabled={busy || unavailable}/>
    <div className="form-actions">
      <button type="button" disabled={busy || unavailable} onClick={() => void decide('approved')}>{t('Approve change', '批准調課')}</button>
      <button type="button" className="outline" disabled={busy || unavailable} onClick={() => void decide('rejected')}>{t('Reject change', '拒絕調課')}</button>
    </div>
    {busy && <p role="status">{t('Saving decision…', '正在儲存處理結果…')}</p>}
    {error && <p role="alert">{error}</p>}
  </div>;
}

export function RequestLessonChange(props: ProtectedProps & { studentId: string; bookingId: string }) {
  const [open, setOpen] = useState(false);
  return open ? <ChangeOptions {...props}/> : <button type="button" className="outline" onClick={() => setOpen(true)}>{props.t('Request lesson change', '申請調課')}</button>;
}

function ChangeOptions({ studentId, bookingId, csrfToken, expired, t }: ProtectedProps & { studentId: string; bookingId: string }) {
  const id = useId();
  const api = useProtectedRequest(expired);
  const [from, setFrom] = useState(hkDate);
  const [to, setTo] = useState(() => rangeEnd(hkDate()));
  const [options, setOptions] = useState<Options>();
  const [target, setTarget] = useState('');
  const [busy, setBusy] = useState<'search' | 'submit'>();
  const [error, setError] = useState('');
  const [submitted, setSubmitted] = useState<LessonChange>();
  const intent = useRef<ChangeRequest>(undefined);
  const path = `/students/${encodeURIComponent(studentId)}/bookings/${encodeURIComponent(bookingId)}`;

  function clearSelection() {
    setOptions(undefined);
    setTarget('');
    setError('');
  }

  async function findOptions() {
    clearSelection();
    if (!from || !to || to < from || Date.parse(to) - Date.parse(from) > 92 * 86_400_000) {
      setError(t('Choose a date range of at most 93 days.', '請選擇最多 93 日的日期範圍。'));
      return;
    }
    setBusy('search');
    try {
      setOptions(await api<Options>(`${path}/change-options?${new URLSearchParams({ from, to })}`));
    } catch (cause) {
      if (!isAborted(cause)) setError(errorMessage(cause, t, t('Could not load available lessons. Try again.', '未能載入可選課堂。請再試。')));
    } finally { setBusy(undefined); }
  }

  async function submit() {
    const selection = options?.items.find(item => item.id === target);
    if (!selection || busy) return;
    setBusy('submit');
    setError('');
    if (intent.current?.targetClassId !== target || intent.current.targetClassVersion !== selection.version) {
      intent.current = { targetClassId: target, targetClassVersion: selection.version, idempotencyKey: crypto.randomUUID() };
    }
    try {
      setSubmitted(await api<LessonChange>(`${path}/change-requests`, {
        method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(intent.current),
      }));
    } catch (cause) {
      if (!isAborted(cause)) {
        if (cause instanceof APIError && cause.status === 409) clearSelection();
        setError(errorMessage(cause, t, t('Could not send the request. Try again.', '未能送出申請。請再試。')));
      }
    } finally { setBusy(undefined); }
  }

  if (submitted) return <p role="status">{t('Lesson change request received.', '已收到調課申請。')} {statusLabel(submitted.status, t)}</p>;

  return <div>
    <form className="schedule-filters" onSubmit={event => { event.preventDefault(); void findOptions(); }}>
      <div><label htmlFor={`${id}-from`}>{t('New lesson from date', '新課堂開始日期')}</label>
        <input id={`${id}-from`} type="date" required value={from} disabled={!!busy} onChange={event => { setFrom(event.target.value); clearSelection(); }}/></div>
      <div><label htmlFor={`${id}-to`}>{t('New lesson to date', '新課堂結束日期')}</label>
        <input id={`${id}-to`} type="date" required value={to} min={from} disabled={!!busy} onChange={event => { setTo(event.target.value); clearSelection(); }}/></div>
      <button className="outline" disabled={!!busy}>{t('Find replacement lessons', '搜尋可替換課堂')}</button>
    </form>
    {busy === 'search' && <p role="status">{t('Loading available lessons…', '正在載入可選課堂…')}</p>}
    {options && <p className="muted">{t(`Request changes at least ${options.cutoffHours} hours before the original lesson.`, `請在原定課堂開始前至少 ${options.cutoffHours} 小時申請調課。`)}</p>}
    {options && (options.items.length === 0
      ? <p role="status">{t('No replacement lessons are available for these dates or this booking.', '這段日期或此預約沒有可替換的課堂。')}</p>
      : <form onSubmit={event => { event.preventDefault(); void submit(); }}>
        <label htmlFor={`${id}-target`}>{t('Replacement lesson', '替換課堂')}</label>
        <select id={`${id}-target`} required value={target} disabled={!!busy} onChange={event => { setTarget(event.target.value); setError(''); }}>
          <option value="">{t('Choose a lesson', '選擇課堂')}</option>
          {options.items.map(item => <option key={item.id} value={item.id}>{classLabel(item)}</option>)}
        </select>
        <p>{t('Your original booking stays in place until a coach approves the request.', '教練批准申請前，原有預約將會保留。')}</p>
        <button disabled={!target || !!busy}>{busy === 'submit' ? t('Sending request…', '正在送出申請…') : t('Send change request', '送出調課申請')}</button>
      </form>)}
    {error && <p role="alert">{error}</p>}
  </div>;
}
