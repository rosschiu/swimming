import { attendanceSummary, creditLabel } from './attendanceLabels';
import CancelLesson from './CancelLesson';
import { useEffect, useId, useState } from 'react';
import BookingForm from './BookingForm';
import EnrolmentForm from './EnrolmentForm';
import { RequestLessonChange } from './LessonChanges';
import { APIError, type Student } from './api';
import type { components } from './generated/api';
import { hkDate, hkTime, rangeEnd } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type StudentBooking = components['schemas']['StudentBooking'];
type BookingProps = ProtectedProps & { studentId: string; role: 'admin' | 'parent' | 'student' };

function upcomingEnd(from: string) {
  const day = new Date(`${from}T12:00:00+08:00`);
  day.setUTCDate(day.getUTCDate() + 92);
  return hkDate(day);
}

export default function StudentBookings({ student, expired, t, csrfToken, role, onBooked }: ProtectedProps & { student: Student; role: 'admin' | 'parent' | 'student'; onBooked: () => void }) {
  const id = useId();
  const studentId = student.id;
  const [from, setFrom] = useState(hkDate);
  // Default to the longest range the API allows so upcoming lessons show without a search.
  const [to, setTo] = useState(() => upcomingEnd(hkDate()));
  const [choosing, setChoosing] = useState(false);
  const [range, setRange] = useState(() => ({ from, to, revision: 0 }));
  const [invalid, setInvalid] = useState(false);

  function refreshBookings(startsAt: string[]) {
    const days = startsAt.map(time => hkDate(new Date(time))).sort();
    const first = days[0];
    const last = days[days.length - 1];
    if (first < range.from || last > range.to) {
      const end = last > upcomingEnd(first) ? last : upcomingEnd(first);
      setFrom(first);
      setTo(end);
      setRange(previous => ({ from: first, to: end, revision: previous.revision + 1 }));
    } else {
      setRange(previous => ({ ...previous, revision: previous.revision + 1 }));
    }
    setInvalid(false);
    onBooked();
  }

  return <section className="section-gap" aria-labelledby={`${id}-title`}>
    <h2 id={`${id}-title`}>{t('Booked lessons', '已預約課堂')}</h2>
    <p className="muted">{t('All dates and times are Hong Kong time.', '所有日期及時間均為香港時間。')}</p>
    {role !== 'student' && <>
      <BookingForm student={student} admin={role === 'admin'} csrfToken={csrfToken} expired={expired} t={t} onBooked={startsAt => refreshBookings([startsAt])}/>
      <EnrolmentForm student={student} admin={role === 'admin'} csrfToken={csrfToken} expired={expired} t={t} onBooked={refreshBookings}/>
    </>}
    {!choosing && <button className="text-button" aria-expanded="false" onClick={() => setChoosing(true)}>{t('Change dates', '更改日期')}</button>}
    {choosing && <form className="schedule-filters" onSubmit={event => {
      event.preventDefault();
      const valid = from && to && to >= from && Date.parse(to) - Date.parse(from) <= 92 * 86400000;
      setInvalid(!valid);
      if (valid) setRange(previous => ({ from, to, revision: previous.revision + 1 }));
    }}>
      <div><label htmlFor={`${id}-from`}>{t('Bookings from date', '預約開始日期')}</label>
        <input id={`${id}-from`} type="date" required value={from} onChange={event => setFrom(event.target.value)}/>
      </div>
      <div><label htmlFor={`${id}-to`}>{t('Bookings to date', '預約結束日期')}</label>
        <input id={`${id}-to`} type="date" required min={from} value={to} onChange={event => setTo(event.target.value)}/>
      </div>
      <button className="outline">{t('Load booked lessons', '載入已預約課堂')}</button>
    </form>}
    {invalid && <p role="alert">{t('Choose a date range of at most 93 days.', '請選擇最多 93 日的日期範圍。')}</p>}
    <BookingResults key={`${studentId}:${range.revision}`} studentId={studentId} role={role} csrfToken={csrfToken} expired={expired} t={t} from={range.from} to={range.to} onRetry={() => setRange(previous => ({ ...previous, revision: previous.revision + 1 }))} onChanged={onBooked}/>
  </section>;
}

function BookingResults({ studentId, role, csrfToken, expired, t, from, to, onRetry, onChanged }: BookingProps & { from: string; to: string; onRetry: () => void; onChanged: () => void }) {
  const api = useProtectedRequest(expired);
  const [result, setResult] = useState<StudentBooking[] | 'unavailable' | 'error' | null>(null);

  useEffect(() => {
    const query = new URLSearchParams({ from, to });
    void api<{ items: StudentBooking[] }>(`/students/${encodeURIComponent(studentId)}/bookings?${query}`)
      .then(({ items }) => setResult(items))
      .catch(error => {
        if (!isAborted(error)) setResult(error instanceof APIError && (error.status === 403 || error.status === 404) ? 'unavailable' : 'error');
      });
  }, [api, studentId, from, to]);

  return <>
    <p className="muted">{t('Selected dates', '選定日期')} {from}–{to}</p>
    {result === null ? <p role="status">{t('Loading booked lessons…', '正在載入已預約課堂…')}</p>
      : result === 'unavailable' ? <p role="alert">{t('Booked lessons are unavailable for your current access.', '目前權限無法存取已預約課堂。')}</p>
      : result === 'error' ? <div><p role="alert">{t('Could not load booked lessons.', '未能載入已預約課堂。')}</p><button className="outline" onClick={onRetry}>{t('Retry booked lessons', '重試載入已預約課堂')}</button></div>
      : result.length === 0 ? <p role="status">{t('No booked lessons on these dates.', '這段日期內沒有已預約課堂。')}</p>
      : <ul className="link-list">{result.map(({ id, class: current, attendance, status, cancellation }) => {
        // A cancelled lesson keeps the details the guardian reviewed, even if the class was edited later.
        const lesson = cancellation?.lesson ?? current;
        const active = status === 'reserved';
        return <li key={id}>
        <div><strong>{lesson.classTypeName}</strong>
          <p><time dateTime={lesson.startsAt}>{hkDate(new Date(lesson.startsAt))} {hkTime(lesson.startsAt)}</time>–<time dateTime={lesson.endsAt}>{hkDate(new Date(lesson.startsAt)) !== hkDate(new Date(lesson.endsAt)) && `${hkDate(new Date(lesson.endsAt))} `}{hkTime(lesson.endsAt)}</time></p>
          <p className="muted">{lesson.venueName} · {lesson.coachName}</p>
          {active && <p>{t('Attendance', '出席記錄')} <span className="tag">{attendanceSummary(attendance, t)}</span></p>}
          {cancellation && <p className="muted">{cancellation.timing === 'early' ? t('Cancelled on time', '準時取消') : t('Cancelled late', '逾時取消')} · {creditLabel(cancellation.creditOutcome, t)}</p>}
          {role === 'parent' && active && !attendance && Date.parse(lesson.startsAt) > Date.now() && <div className="form-actions">
            <RequestLessonChange studentId={studentId} bookingId={id} csrfToken={csrfToken} expired={expired} t={t}/>
            <CancelLesson studentId={studentId} bookingId={id} csrfToken={csrfToken} expired={expired} t={t} onCancelled={booking => {
              setResult(previous => Array.isArray(previous) ? previous.map(item => item.id === booking.id ? booking : item) : previous);
              onChanged();
            }}/>
          </div>}
        </div><span className="tag">{active ? t('Reserved', '已預約') : t('Cancelled', '已取消')}</span>
      </li>;
      })}</ul>}
  </>;
}
