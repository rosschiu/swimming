import { useRef, useState, type FormEvent } from 'react';
import { APIError } from './api';
import type { components } from './generated/api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type ProgressNoteInput = components['schemas']['ProgressNoteInput'];

// Feedback for one attended lesson, shared with the swimmer's family as a lesson-linked progress note.
export default function LessonFeedback({ studentId, studentName, bookingId, csrfToken, expired, t, onShared }: ProtectedProps & {
  studentId: string; studentName: string; bookingId: string; onShared: () => void;
}) {
  const api = useProtectedRequest(expired);
  const [open, setOpen] = useState(false);
  const [body, setBody] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(false);
  const pending = useRef<ProgressNoteInput | null>(null);
  const id = `feedback-${bookingId}`;
  async function share(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    if (!pending.current && (!body.trim() || [...body.trim()].length > 4000)) { setError(t('Enter feedback of 1–4000 characters.', '請輸入 1 至 4000 字的回饋。')); return; }
    pending.current ??= { requestId: crypto.randomUUID(), body: body.trim(), bookingId };
    setBusy(true); setError('');
    try {
      await api(`/students/${encodeURIComponent(studentId)}/progress-notes`, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(pending.current) });
      pending.current = null; setRetry(false); setBody(''); setOpen(false); onShared();
    } catch (e) {
      if (isAborted(e)) return;
      const uncertain = !(e instanceof APIError) || e.status >= 500 || e.code === 'request_conflict';
      if (!uncertain) pending.current = null;
      setRetry(uncertain);
      setError(uncertain ? t('Sharing could not be confirmed. Your text is preserved. Retry the same submission to avoid duplicates.', '未能確認是否已分享。已保留內容，請重試同一次提交以避免重複。')
        : e instanceof APIError && e.code === 'lesson_not_eligible' ? t('Feedback can be shared only for a swimmer marked present.', '只可為已標記出席的學員分享回饋。')
        : t('Could not share this feedback. Your text is preserved; check your access and try again.', '未能分享回饋。已保留內容，請檢查權限後再試。'));
    } finally { setBusy(false); }
  }
  if (!open) return <button className="outline" aria-label={t(`Share feedback for ${studentName}`, `為 ${studentName} 分享回饋`)} onClick={() => setOpen(true)}>{t('Share feedback', '分享回饋')}</button>;
  return <form onSubmit={share}>
    <label htmlFor={id}>{t(`Feedback for ${studentName}`, `給 ${studentName} 的回饋`)}</label>
    <textarea id={id} value={body} onChange={event => setBody(event.target.value)} disabled={busy || retry} required/>
    <p className="muted">{t('Shared with the family straight away.', '會即時分享予家長。')}</p>
    {error && <p role="alert" className="error">{error}</p>}
    <div className="form-actions">
      <button disabled={busy}>{busy ? t('Sharing…', '分享中…') : retry ? t('Retry same submission', '重試同一次提交') : t('Share feedback', '分享回饋')}</button>
      <button type="button" className="outline" disabled={busy} onClick={() => { setOpen(false); setError(''); if (!retry) setBody(''); }}>{t('Cancel', '取消')}</button>
    </div>
  </form>;
}
