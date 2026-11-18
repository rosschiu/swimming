import { useEffect, useId, useState, type ReactNode } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { hkDate, hkTime } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Impact = components['schemas']['CancellationImpact'];
type Section = 'bookings' | 'inbound' | 'outbound';

export default function CancellationImpact({ classId, expired, t }: Pick<ProtectedProps, 'expired' | 't'> & { classId: string }) {
  const api = useProtectedRequest(expired);
  const id = useId();
  const [view, setView] = useState<{ section: Section; pages: string[] }>({ section: 'bookings', pages: [] });
  const [result, setResult] = useState<Impact | 'unavailable' | 'error' | null>(null);

  useEffect(() => {
    let current = true;
    const query = new URLSearchParams({ section: view.section });
    const cursor = view.pages.at(-1);
    if (cursor) query.set('cursor', cursor);
    void api<Impact>(`/classes/${encodeURIComponent(classId)}/cancellation-impact?${query}`)
      .then(impact => { if (current) setResult(impact); })
      .catch(error => {
        if (current && !isAborted(error)) setResult(error instanceof APIError && [403, 404].includes(error.status) ? 'unavailable' : 'error');
      });
    return () => { current = false; };
  }, [api, classId, view]);

  function show(section: Section, pages: string[] = []) {
    setResult(null);
    setView({ section, pages });
  }

  return <section className="panel section-gap" aria-labelledby={id}>
    <div className="page-heading">
      <h3 id={id}>{t('Cancellation impact', '取消課堂影響')}</h3>
      <button type="button" className="outline" disabled={result === null} onClick={() => show(view.section)}>
        {t('Refresh impact', '重新載入影響')}
      </button>
    </div>
    <p>{t('Review the current lesson records. No lesson, booking or credit changes are made here.', '檢視目前的課堂紀錄。此處不會更改課堂、預約或堂數。')}</p>
    <label htmlFor={`${id}-section`}>{t('Impact details', '影響詳情')}</label>
    <select id={`${id}-section`} value={view.section} onChange={event => show(event.target.value as Section)}>
      <option value="bookings">{t('Booked swimmers', '已預約學員')}</option>
      <option value="inbound">{t('Pending inbound requests', '待處理調入申請')}</option>
      <option value="outbound">{t('Pending outbound requests', '待處理調出申請')}</option>
    </select>
    {result === null ? <p role="status">{t('Loading cancellation impact…', '正在載入取消課堂影響…')}</p>
      : result === 'unavailable' ? <p role="alert">{t('This lesson is unavailable for your current access.', '目前權限無法存取此課堂。')}</p>
      : result === 'error' ? <p role="alert">{t('Could not load cancellation impact. Refresh to retry.', '未能載入取消課堂影響，請重新載入。')}</p>
      : <ImpactDetails impact={result} view={view.section} t={t}>
        {view.pages.length > 0 && <button type="button" className="outline" onClick={() => show(view.section, view.pages.slice(0, -1))}>{t('Previous page', '上一頁')}</button>}
        {result.nextCursor && <button type="button" className="outline" onClick={() => show(view.section, [...view.pages, result.nextCursor!])}>{t('Next page', '下一頁')}</button>}
      </ImpactDetails>}
  </section>;
}

function ImpactDetails({ impact, view, t, children }: { impact: Impact; view: Section; t: ProtectedProps['t']; children: ReactNode }) {
  const { lesson } = impact;
  const total = view === 'bookings' ? impact.bookedSwimmers : view === 'inbound' ? impact.pendingInbound : impact.pendingOutbound;
  const shown = view === 'bookings' ? impact.bookings.length : impact.requests.length;
  const reviewed = Date.parse(impact.reviewedAt);
  const state = reviewed >= Date.parse(lesson.endsAt) ? t('Ended', '已結束') :
    reviewed >= Date.parse(lesson.startsAt) ? t('Started', '已開始') : t('Upcoming', '即將開始');
  return <>
    <p>{lesson.classTypeName} · {lesson.venueName} · {lesson.coachName}</p>
    <p>{hkDate(new Date(lesson.startsAt))} {hkTime(lesson.startsAt)} – {hkDate(new Date(lesson.endsAt))} {hkTime(lesson.endsAt)} · {state}</p>
    <p>{t('Reviewed at', '檢視時間')} <time dateTime={impact.reviewedAt}>{hkDate(new Date(impact.reviewedAt))} {hkTime(impact.reviewedAt)}</time> {t('Hong Kong time', '香港時間')}</p>
    <dl>
      <dt>{t('Booked swimmers', '已預約學員')}</dt><dd>{impact.bookedSwimmers}</dd>
      <dt>{t('Unmarked bookings', '未記錄出席的預約')}</dt><dd>{impact.unmarkedBookings}</dd>
      <dt>{t('Recorded attendance', '已記錄出席')}</dt><dd>{impact.recordedAttendance}</dd>
      <dt>{t('Pending inbound requests', '待處理調入申請')}</dt><dd>{impact.pendingInbound}</dd>
      <dt>{t('Pending outbound requests', '待處理調出申請')}</dt><dd>{impact.pendingOutbound}</dd>
    </dl>
    <p role="status">{t(`Showing ${shown} of ${total} on this page.`, `此頁顯示 ${total} 筆中的 ${shown} 筆。`)}</p>
    {view === 'bookings' && impact.bookings.length > 0 && <ul className="link-list">{impact.bookings.map(item =>
      <li key={item.bookingId}>
        <strong>{item.studentName}</strong> · {item.attendanceMarkedAt ? t('Attendance recorded', '已記錄出席') : t('Attendance unmarked', '尚未記錄出席')}
        <p>{item.enrolmentId ? t('Fixed enrolment', '固定報名') : t('Flexible booking', '彈性預約')} · {t('Booking', '預約')} {item.bookingId}</p>
        <p>{t('Source package', '來源套票')} {item.packageId} · {item.expiresOn ? `${t('Expiry (inclusive, Hong Kong)', '到期日（包括當日，香港）')} ${item.expiresOn}` : t('No expiry', '無到期日')}</p>
        {item.packageExpired && <p>{t('Package expired at review', '檢視時套票已到期')}</p>}
        {!item.packageCoversLesson && <p>{t('Package expiry does not cover the lesson under its recorded booking rule.', '按預約時的規則，套票有效期未能涵蓋此課堂。')}</p>}
      </li>)}</ul>}
    {view !== 'bookings' && impact.requests.length > 0 && <ul className="link-list">{impact.requests.map(item =>
      <li key={item.id}><strong>{item.studentName}</strong>
        <p>{t('Request', '申請')} {item.id} · {t('Booking', '預約')} {item.bookingId}</p>
        <p>{view === 'inbound' ? t('Original lesson', '原課堂') : t('Replacement lesson', '替代課堂')} {hkDate(new Date(item.otherStartsAt))} {hkTime(item.otherStartsAt)} – {hkDate(new Date(item.otherEndsAt))} {hkTime(item.otherEndsAt)} · {item.otherVenue}</p>
      </li>)}</ul>}
    <div className="form-actions">{children}</div>
  </>;
}
