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

export default function StudentBookings({ student, expired, t, csrfToken, role, onBooked }: ProtectedProps & { student: Student; role: 'admin' | 'parent' | 'student'; onBooked: () => void }) {
  const id = useId();
  const studentId = student.id;
  const [from, setFrom] = useState(hkDate);
  const [to, setTo] = useState(() => rangeEnd(hkDate()));
  const [range, setRange] = useState(() => ({ from, to, revision: 0 }));
  const [invalid, setInvalid] = useState(false);

  function refreshBookings(startsAt: string[]) {
    const days = startsAt.map(time => hkDate(new Date(time))).sort();
    const first = days[0];
    const last = days[days.length - 1];
    if (first < range.from || last > range.to) {
      const end = last > rangeEnd(first) ? last : rangeEnd(first);
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
    <form className="schedule-filters" onSubmit={event => {
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
    </form>
    {invalid && <p role="alert">{t('Choose a date range of at most 93 days.', '請選擇最多 93 日的日期範圍。')}</p>}
    <BookingResults key={`${studentId}:${range.revision}`} studentId={studentId} role={role} csrfToken={csrfToken} expired={expired} t={t} from={range.from} to={range.to}/>
  </section>;
}

function BookingResults({ studentId, role, csrfToken, expired, t, from, to }: BookingProps & { from: string; to: string }) {
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
      : result === 'error' ? <p role="alert">{t('Could not load booked lessons. Use the button above to retry.', '未能載入已預約課堂。請使用上方按鈕重試。')}</p>
      : result.length === 0 ? <p role="status">{t('No booked lessons on these dates.', '這段日期內沒有已預約課堂。')}</p>
      : <ul className="link-list">{result.map(({ id, class: lesson, attendance }) => <li key={id}>
        <div><strong>{lesson.classTypeName}</strong>
          <p><time dateTime={lesson.startsAt}>{hkDate(new Date(lesson.startsAt))} {hkTime(lesson.startsAt)}</time>–<time dateTime={lesson.endsAt}>{hkDate(new Date(lesson.startsAt)) !== hkDate(new Date(lesson.endsAt)) && `${hkDate(new Date(lesson.endsAt))} `}{hkTime(lesson.endsAt)}</time></p>
          <p className="muted">{lesson.venueName} · {lesson.coachName}</p>
          <p>{t('Attendance', '出席記錄')} <span className="tag">{attendance ? t('Present', '已出席') : t('Unmarked', '尚未記錄')}</span></p>
          {role === 'parent' && !attendance && Date.parse(lesson.startsAt) > Date.now() && <RequestLessonChange studentId={studentId} bookingId={id} csrfToken={csrfToken} expired={expired} t={t}/>}
        </div><span className="tag">{t('Reserved', '已預約')}</span>
      </li>)}</ul>}
  </>;
}
