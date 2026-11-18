import { useId, useRef, useState } from 'react';
import { APIError } from './api';
import type { components } from './generated/api';
import { hkDate, hkTime } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Impact = components['schemas']['CancellationImpact'];
type Result = components['schemas']['SchoolCancellationResult'];

// Administrators cancel one upcoming lesson after reviewing its impact; credits always return to families.
export default function CancelClass({ impact, csrfToken, expired, t, onCancelled }: ProtectedProps & { impact: Impact; onCancelled: (result: Result) => void }) {
  const api = useProtectedRequest(expired);
  const id = useId();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState('');
  const [expiries, setExpiries] = useState<Record<string, string>>({});
  const [needed, setNeeded] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState<Result | null>(null);
  const intent = useRef<{ fingerprint: string; key: string } | null>(null);
  const lesson = impact.lesson;
  const names = new Map(impact.bookings.map(item => [item.bookingId, item.studentName]));
  const today = hkDate();

  if (result) return <div className="success" role="status">
    <p>{t('Lesson cancelled.', '課堂已取消。')} {t(`${result.cancelledBookings} bookings cancelled and their credits returned; ${result.restoredCredits} restored with a new expiry; ${result.closedRequests} lesson change requests closed.`, `已取消 ${result.cancelledBookings} 項預約並退回堂數；其中 ${result.restoredCredits} 堂以新到期日恢復；已結束 ${result.closedRequests} 項調課申請。`)}</p>
  </div>;
  if (lesson.cancelledAt) return <p className="muted">{t('This lesson was cancelled by the school on', '此課堂已由學校取消，時間：')} {hkDate(new Date(lesson.cancelledAt))} {hkTime(lesson.cancelledAt)}</p>;
  if (Date.parse(impact.reviewedAt) >= Date.parse(lesson.startsAt)) return <p className="muted">{t('Started lessons are handled through attendance corrections.', '已開始的課堂須透過出席更正處理。')}</p>;
  if (!open) return <button type="button" className="outline" onClick={() => setOpen(true)}>{t('Cancel this lesson for everyone', '為所有人取消此課堂')}</button>;

  async function submit() {
    if (!reason.trim() || saving || needed.some(bookingId => !expiries[bookingId])) return;
    const body = { reason: reason.trim(), version: lesson.version, ...(needed.length ? { expiries: Object.fromEntries(needed.map(bookingId => [bookingId, expiries[bookingId]])) } : {}) };
    const fingerprint = JSON.stringify(body);
    if (intent.current?.fingerprint !== fingerprint) intent.current = { fingerprint, key: crypto.randomUUID() };
    setSaving(true); setError('');
    try {
      const cancelled = await api<Result>(`/classes/${encodeURIComponent(lesson.id)}/cancellation`, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ ...body, idempotencyKey: intent.current.key }) });
      setResult(cancelled);
      onCancelled(cancelled);
    } catch (cause) {
      if (isAborted(cause)) return;
      const code = cause instanceof APIError ? cause.code : undefined;
      if (code === 'expiry_required') {
        const ids = (cause as APIError).details as { bookingIds?: string[] } | undefined;
        setNeeded(ids?.bookingIds ?? []);
        setError(t('Some returned credits come from packages that have already expired. Enter a new expiry for each before confirming.', '部分退回的堂數來自已過期的套票。請先為每項輸入新到期日再確認。'));
      } else {
        setError(code === 'version_conflict' ? t('The lesson changed. Refresh the impact and review it again.', '課堂已變更，請重新載入影響並再次檢視。')
          : code === 'class_started' ? t('This lesson has started. Use attendance corrections instead.', '課堂已開始，請改用出席更正。')
          : code === 'class_cancelled' ? t('This lesson is already cancelled.', '此課堂已取消。')
          : t('Could not cancel the lesson. Your entries are preserved; try again.', '未能取消課堂，已保留輸入內容，請再試。'));
      }
    } finally { setSaving(false); }
  }

  return <div className="cancel-review" role="group" aria-label={t('Cancel this lesson', '取消此課堂')}>
    <p>{t(`Cancelling returns credits to ${impact.bookedSwimmers} booked swimmers and closes ${impact.pendingInbound + impact.pendingOutbound} pending lesson change requests. Families and the coach are notified.`, `取消後會退回 ${impact.bookedSwimmers} 位已預約學員的堂數，並結束 ${impact.pendingInbound + impact.pendingOutbound} 項待處理調課申請，並通知家庭及教練。`)}</p>
    <label htmlFor={`${id}-reason`}>{t('Reason shown in the audit history', '記錄於操作紀錄的原因')}</label>
    <input id={`${id}-reason`} maxLength={500} value={reason} disabled={saving} onChange={event => setReason(event.target.value)}/>
    {needed.map(bookingId => <div key={bookingId}>
      <label htmlFor={`${id}-${bookingId}`}>{t(`New expiry for ${names.get(bookingId) ?? bookingId}`, `${names.get(bookingId) ?? bookingId} 的新到期日`)}</label>
      <input id={`${id}-${bookingId}`} type="date" min={today} value={expiries[bookingId] ?? ''} disabled={saving} onChange={event => setExpiries(previous => ({ ...previous, [bookingId]: event.target.value }))}/>
    </div>)}
    {error && <p role="alert" className="error">{error}</p>}
    <div className="form-actions">
      <button disabled={!reason.trim() || saving || needed.some(bookingId => !expiries[bookingId])} onClick={() => void submit()}>{saving ? t('Cancelling…', '取消中…') : t('Confirm school cancellation', '確認學校取消課堂')}</button>
      <button className="outline" disabled={saving} onClick={() => { setOpen(false); setError(''); }}>{t('Keep lesson', '保留課堂')}</button>
    </div>
  </div>;
}
