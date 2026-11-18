import { useCallback, useEffect, useState, type MouseEvent } from 'react';
import { APIError } from './api';
import type { components } from './generated/api';
import { routePath, type Route } from './navigation';
import { classLabel } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Booking = components['schemas']['StudentBookingDetail'];

export default function BookingDetail({ studentId, bookingId, expired, t, follow }: Pick<ProtectedProps, 'expired' | 't'> & {
  studentId: string;
  bookingId: string;
  follow: (event: MouseEvent<HTMLAnchorElement>, next: Route) => void;
}) {
  const api = useProtectedRequest(expired);
  const [result, setResult] = useState<Booking | 'unavailable' | 'error' | null>(null);

  const load = useCallback(async () => {
    setResult(null);
    try {
      setResult(await api<Booking>(`/students/${encodeURIComponent(studentId)}/bookings/${encodeURIComponent(bookingId)}`));
    } catch (error) {
      if (!isAborted(error)) {
        setResult(error instanceof APIError && (error.status === 403 || error.status === 404) ? 'unavailable' : 'error');
      }
    }
  }, [api, studentId, bookingId]);
  useEffect(() => { void load(); }, [load]);

  const profile: Route = { view: 'students', studentId };
  return <section className="panel">
    <h1>{t('Booking details', '預約詳情')}</h1>
    {result === null ? <p role="status">{t('Loading booking…', '正在載入預約…')}</p>
      : result === 'unavailable' ? <p role="alert">{t('This booking is unavailable for your current access.', '目前權限無法存取此預約。')}</p>
      : result === 'error' ? <><p role="alert">{t('Could not load this booking.', '未能載入此預約。')}</p><button className="outline" onClick={() => void load()}>{t('Retry', '重試')}</button></>
      : <>
        <h2>{result.studentName}</h2>
        <p>{classLabel(result.class)}</p>
        <p className="muted">{t('Times shown in Hong Kong time.', '時間以香港時間顯示。')}</p>
        <p>{t('Booking status', '預約狀態')} <span className="tag">{t('Reserved', '已預約')}</span></p>
        <p>{t('Attendance', '出席記錄')} <span className="tag">{result.attendance ? t('Present', '已出席') : t('Unmarked', '尚未記錄')}</span></p>
        <a className="text-button" href={routePath(profile)} onClick={event => follow(event, profile)}>{t('Open student profile', '開啟學員資料')}</a>
      </>}
  </section>;
}
