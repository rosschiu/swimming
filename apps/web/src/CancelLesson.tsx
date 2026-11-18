import { useRef, useState } from 'react';
import { APIError } from './api';
import type { components } from './generated/api';
import { hkDate, hkTime } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps, type Translate } from './useProtectedRequest';

type Preview = components['schemas']['CancellationPreview'];
type Booking = components['schemas']['StudentBooking'];

export function cancellationConsequence(preview: Pick<Preview, 'timing' | 'creditOutcome'>, t: Translate) {
  const when = preview.timing === 'early' ? t('This is an on-time cancellation.', '此為準時取消。') : t('This is a late cancellation.', '此為逾時取消。');
  const credit = {
    retained: t('The lesson credit goes back to your package.', '課堂堂數會退回你的套票。'),
    expired: t('The credit goes back, but its package has expired, so it cannot be used again.', '堂數會退回，但套票已過期，不能再使用。'),
    consumed: t('The lesson credit is used under the school’s cancellation policy.', '根據學校的取消政策，此課堂堂數會被扣除。'),
  }[preview.creditOutcome];
  return `${when} ${credit}`;
}

export default function CancelLesson({ studentId, bookingId, csrfToken, expired, t, onCancelled }: ProtectedProps & {
  studentId: string; bookingId: string; onCancelled: (booking: Booking) => void;
}) {
  const api = useProtectedRequest(expired);
  const path = `/students/${encodeURIComponent(studentId)}/bookings/${encodeURIComponent(bookingId)}/cancellation`;
  const [preview, setPreview] = useState<Preview | null>(null);
  const [state, setState] = useState<'idle' | 'loading' | 'saving'>('idle');
  const [message, setMessage] = useState('');
  const intent = useRef<{ fingerprint: string; key: string } | null>(null);

  function explain(error: unknown) {
    const code = error instanceof APIError ? error.code : undefined;
    return code === 'cancellation_closed' ? t('This lesson can no longer be cancelled online. Please contact the school.', '此課堂已不能在網上取消，請聯絡學校。')
      : error instanceof APIError && (error.status === 403 || error.status === 404) ? t('This booking is unavailable for your current access.', '目前權限無法存取此預約。')
      : t('Could not complete the cancellation. Try again.', '未能完成取消，請再試。');
  }
  async function review() {
    setState('loading'); setMessage('');
    try { setPreview(await api<Preview>(path)); }
    catch (error) { if (!isAborted(error)) setMessage(explain(error)); }
    finally { setState('idle'); }
  }
  async function confirm() {
    if (!preview || state !== 'idle') return;
    const body = { classId: preview.lesson.id, timing: preview.timing, creditOutcome: preview.creditOutcome };
    const fingerprint = JSON.stringify(body);
    if (intent.current?.fingerprint !== fingerprint) intent.current = { fingerprint, key: crypto.randomUUID() };
    setState('saving'); setMessage('');
    try {
      onCancelled(await api<Booking>(path, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ ...body, idempotencyKey: intent.current.key }) }));
    } catch (error) {
      if (isAborted(error)) return;
      if (error instanceof APIError && error.code === 'cancellation_changed' && error.details) {
        setPreview(error.details as Preview);
        setMessage(t('The details changed since you looked. Check them again before confirming.', '資料在你查看後已變更，請再次檢查後才確認。'));
      } else {
        setMessage(explain(error));
      }
    } finally { setState(previous => previous === 'saving' ? 'idle' : previous); }
  }

  if (!preview) return <>
    <button type="button" className="outline" disabled={state !== 'idle'} onClick={() => void review()}>{state === 'loading' ? t('Checking…', '檢查中…') : t('Cancel lesson', '取消課堂')}</button>
    {message && <p role="alert" className="error">{message}</p>}
  </>;
  const lesson = preview.lesson;
  return <div className="cancel-review" role="group" aria-label={t('Cancel this lesson', '取消此課堂')}>
    <p><strong>{lesson.classTypeName}</strong> · {hkDate(new Date(lesson.startsAt))} {hkTime(lesson.startsAt)}–{hkTime(lesson.endsAt)} · {lesson.venueName}</p>
    <p>{cancellationConsequence(preview, t)}</p>
    {preview.timing === 'early' && <p className="muted">{t(`Cancellations after ${hkDate(new Date(preview.lateFrom))} ${hkTime(preview.lateFrom)} are late.`, `於 ${hkDate(new Date(preview.lateFrom))} ${hkTime(preview.lateFrom)} 之後取消屬逾時取消。`)}</p>}
    {message && <p role="alert" className="error">{message}</p>}
    <div className="form-actions">
      <button disabled={state !== 'idle'} onClick={() => void confirm()}>{state === 'saving' ? t('Cancelling…', '取消中…') : t('Confirm cancellation', '確認取消')}</button>
      <button className="outline" disabled={state === 'saving'} onClick={() => { setPreview(null); setMessage(''); intent.current = null; }}>{t('Keep lesson', '保留課堂')}</button>
    </div>
  </div>;
}
