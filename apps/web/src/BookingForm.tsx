import { useEffect, useId, useRef, useState } from 'react';
import { APIError, type Student } from './api';
import type { components } from './generated/api';
import { bookingCreditCovers, classLabel, hkDate, hkTime, rangeEnd, type ClassSession } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type CreditPackage = components['schemas']['CreditHistoryPackage'];
type CreditHistory = components['schemas']['CreditHistory'];
type BookingInput = components['schemas']['BookingInput'];
type Choices = { classes: ClassSession[]; packages: CreditPackage[]; validity: CreditHistory['bookingValidity'] };
type Props = ProtectedProps & { student: Student; admin: boolean; onBooked: (startsAt: string) => void };

const shiftDate = (date: string, days: number) => { const day = new Date(`${date}T12:00:00+08:00`); day.setUTCDate(day.getUTCDate() + days); return hkDate(day); };
// Spend the credit that expires first; packages without expiry come last.
const byExpiry = (a: CreditPackage, b: CreditPackage) => (a.expiresOn ?? '9999-12-31').localeCompare(b.expiresOn ?? '9999-12-31');

export default function BookingForm({ csrfToken, expired, t, student, admin, onBooked }: Props) {
  const api = useProtectedRequest(expired);
  const id = useId();
  const [open, setOpen] = useState(false);
  const [date, setDate] = useState(hkDate);
  const [range, setRange] = useState(() => ({ from: hkDate() }));
  const [choices, setChoices] = useState<Choices | 'error' | null>(null);
  const [classId, setClassId] = useState('');
  const [packageId, setPackageId] = useState('');
  const [reason, setReason] = useState('');
  const [input, setInput] = useState<BookingInput | null>(null);
  const [pending, setPending] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [booked, setBooked] = useState(false);
  const submitting = useRef(false);
  const lastIntent = useRef<{ fingerprint: string; input: BookingInput } | null>(null);
  const path = `/students/${encodeURIComponent(student.id)}`;

  useEffect(() => {
    if (!open) return;
    let current = true;
    const query = new URLSearchParams({ from: range.from, to: rangeEnd(range.from) });
    void Promise.all([api<{ items: ClassSession[] }>(`/classes?${query}`), api<CreditHistory>(`${path}/credit-history`)])
      .then(([classes, history]) => {
        if (current) setChoices({
          classes: classes.items.filter(lesson => !lesson.cancelledAt && Date.parse(lesson.startsAt) > Date.now() && (admin || lesson.level === student.level)),
          packages: history.items.filter(item => item.available > 0),
          validity: history.bookingValidity,
        });
      })
      .catch(error => { if (current && !isAborted(error)) setChoices('error'); });
    return () => { current = false; };
  }, [api, admin, open, path, range, student.level]);

  function reload(from: string) {
    setChoices(null);
    setClassId('');
    setPackageId('');
    setReason('');
    setRange({ from });
  }
  const options = choices === 'error' ? null : choices;
  const lesson = options?.classes.find(item => item.id === classId);
  const lessonPackages = lesson && options ? options.packages.filter(item => {
    return bookingCreditCovers(item.expiresOn, lesson, options.validity.validityRule);
  }).sort(byExpiry) : [];
  const creditPackage = (input ? options?.packages : lessonPackages)?.find(item => item.id === packageId);
  const override = admin && !!lesson && lesson.level !== student.level;
  const packageLabel = (value: CreditPackage) => `${t(`${value.available} ${value.available === 1 ? 'lesson' : 'lessons'} left`, `尚餘 ${value.available} 堂`)} · ${value.expiresOn ? t(`use by ${value.expiresOn}`, `${value.expiresOn} 前使用`) : t('no expiry', '無到期日')}${value.sourcePackageId ? ` · ${t('restored credits', '已恢復堂數')}` : ''}`;
  function chooseClass(value: string) {
    setClassId(value); setReason('');
    const next = options?.classes.find(item => item.id === value);
    setPackageId(next && options ? options.packages.filter(item => bookingCreditCovers(item.expiresOn, next, options.validity.validityRule)).sort(byExpiry)[0]?.id ?? '' : '');
  }
  const days = options ? [...new Set(options.classes.map(item => hkDate(new Date(item.startsAt))))] : [];
  const today = hkDate();

  async function submit() {
    if (!input || submitting.current) return;
    submitting.current = true;
    setPending(true);
    setFailure(null);
    try {
      const booking = await api<components['schemas']['StudentBooking']>(`${path}/bookings`, {
        method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(input),
      });
      const startsAt = booking.class.startsAt;
      if (!Number.isFinite(Date.parse(startsAt))) throw new Error('Booking response missing a class start time');
      lastIntent.current = null;
      setInput(null);
      setUncertain(false);
      setOpen(false);
      setBooked(true);
      onBooked(startsAt);
    } catch (error) {
      if (isAborted(error)) return;
      if (!(error instanceof APIError) || error.status >= 500 || error.status === 408 || error.status === 429) {
        setUncertain(true);
        return;
      }
      setInput(null);
      setUncertain(false);
      setFailure(error.code ?? 'unknown');
      reload(range.from);
    } finally {
      submitting.current = false;
      setPending(false);
    }
  }

  const messages: Record<string, string> = {
    version_conflict: t('The class changed. Choose from the refreshed options.', '課堂已變更，請從最新選項重新選擇。'),
    booking_validity_changed: t('The school’s booking validity rule changed. Review the refreshed options and confirm again.', '全校預約有效期規則已變更。請查看最新選項並重新確認。'),
    class_unavailable: t('This class is no longer available.', '此課堂已無法預約。'),
    class_full: t('This class is full. Choose another class.', '此課堂已滿，請選擇其他課堂。'),
    student_overlap: t('This lesson overlaps with another booking for this student.', '此課堂與這位學員的另一項預約重疊。'),
    level_mismatch: t('The student’s level differs from the class level. Refresh the student profile if their level changed.', '學員程度與班別程度不符。如學員程度已變更，請重新載入學員資料。'),
    credit_unavailable: t('The selected package cannot cover this lesson. Check its expiry or choose another package.', '所選套票無法支付此課堂。請檢查到期日或選擇其他套票。'),
    conflict: t('The booking conflicts with a previous request. Review the booked lessons before trying again.', '預約與先前的要求衝突。請先查看已預約課堂再試。'),
  };

  return <section className="section-gap" aria-label={t('Book a lesson', '預約課堂')}>
    {!open && <button className="outline" onClick={() => { setBooked(false); setFailure(null); reload(hkDate()); setDate(hkDate()); setOpen(true); }}>{t('Book a lesson', '預約課堂')}</button>}
    {booked && <p role="status" className="success">{t('Lesson booked. One place and one credit are reserved.', '課堂已預約，並已預留一個名額及一堂套票堂數。')}</p>}
    {open && <div className="panel">
      <h3>{t('Book a lesson', '預約課堂')}</h3>
      {failure && <p role="alert" className="error">{Object.hasOwn(messages, failure) ? messages[failure] : t('Could not book the lesson. Review the refreshed options and try again.', '未能預約課堂。請檢查最新選項後再試。')}</p>}
      {input && options && lesson && creditPackage ? <div>
        <h4>{t('Review booking', '檢查預約')}</h4>
        <p>{student.name} · {classLabel(lesson)}</p>
        <p>{t('Uses one credit', '使用一堂')} · {packageLabel(creditPackage)}</p>
        <p>{options.validity.validityRule === 'lesson_start'
          ? t('The credit must be valid when the lesson starts.', '堂數須於課堂開始時有效。')
          : t('The credit must be valid through the end of the lesson.', '堂數須有效至課堂結束。')}</p>
        <p>{t('Confirm to reserve one place and one credit.', '確認後將預留一個名額及一堂套票堂數。')}</p>
        {input.overrideReason && <p>{t('Level override reason', '程度例外原因')} · {input.overrideReason}</p>}
        {uncertain && <p role="alert" className="error">{t('The result could not be confirmed. Retry this same booking to check its outcome.', '未能確認預約結果。請重試同一項預約以確認結果。')}</p>}
        <div className="form-actions">
          <button disabled={pending} onClick={() => void submit()}>{pending ? t('Confirming…', '正在確認…') : uncertain ? t('Retry same booking', '重試同一項預約') : t('Confirm booking', '確認預約')}</button>
          {!uncertain && <button className="outline" disabled={pending} onClick={() => setInput(null)}>{t('Edit booking', '編輯預約')}</button>}
        </div>
      </div> : <>
        <div className="week-picker">
          <button type="button" className="outline" disabled={range.from <= today} onClick={() => { const from = shiftDate(range.from, -7) < today ? today : shiftDate(range.from, -7); setDate(from); setFailure(null); reload(from); }}>{t('Previous week', '上一週')}</button>
          <div><label htmlFor={`${id}-date`}>{t('Show classes from', '顯示此日期起的課堂')}</label>
            <input id={`${id}-date`} type="date" required min={today} value={date} onChange={event => { setDate(event.target.value); if (event.target.value >= today) { setFailure(null); reload(event.target.value); } }}/></div>
          <button type="button" className="outline" onClick={() => { const from = shiftDate(range.from, 7); setDate(from); setFailure(null); reload(from); }}>{t('Next week', '下一週')}</button>
        </div>
        <p className="muted">{range.from}–{rangeEnd(range.from)} · {t('Hong Kong time', '香港時間')}</p>
        {choices === 'error' ? <div><p role="alert">{t('Could not load booking options.', '未能載入預約選項。')}</p><button type="button" className="outline" onClick={() => reload(range.from)}>{t('Retry', '重試')}</button></div> : !options ? <p role="status">{t('Loading booking options…', '正在載入預約選項…')}</p> : <form onSubmit={event => {
          event.preventDefault();
          if (!options || !lesson || !creditPackage || (override && (!reason.trim() || [...reason.trim()].length > 500))) return;
          const intent = {
            classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion,
            creditPackageId: creditPackage.id, bookingValidityVersion: options.validity.version,
            ...(override ? { overrideReason: reason.trim() } : {}),
          };
          const fingerprint = JSON.stringify(intent);
          if (lastIntent.current?.fingerprint !== fingerprint) lastIntent.current = { fingerprint, input: { ...intent, idempotencyKey: crypto.randomUUID() } };
          setInput(lastIntent.current.input);
        }}>
          <fieldset className="choice-group">
            <legend>{t('Class to book', '預約課堂')}</legend>
            {!options.classes.length && <p role="status">{t('No eligible classes on these dates. Try the next week.', '這段日期內沒有符合預約條件的課堂。請查看下一週。')}</p>}
            {days.map(day => <div key={day} role="group" aria-label={day}>
              <p className="eyebrow choice-day">{day}</p>
              {options.classes.filter(item => hkDate(new Date(item.startsAt)) === day).map(value => <label key={value.id} className={classId === value.id ? 'choice-card choice-selected' : 'choice-card'}>
                <input type="radio" name={`${id}-class`} value={value.id} checked={classId === value.id} onChange={() => chooseClass(value.id)}/>
                <span className="choice-time">{hkTime(value.startsAt)}–{hkTime(value.endsAt)}</span>
                <span className="choice-body"><strong>{value.classTypeName}</strong><span className="muted">{value.venueName} · {value.coachName}{admin ? ` · ${value.level}` : ''}</span></span>
              </label>)}
            </div>)}
          </fieldset>
          {lesson && lessonPackages.length > 0 && <fieldset className="choice-group">
            <legend>{t('Credit package', '堂數套票')}</legend>
            {lessonPackages.map(value => <label key={value.id} className={packageId === value.id ? 'choice-card choice-selected' : 'choice-card'}>
              <input type="radio" name={`${id}-package`} value={value.id} checked={packageId === value.id} onChange={() => setPackageId(value.id)}/>
              <span className="choice-body">{packageLabel(value)}</span>
            </label>)}
          </fieldset>}
          {!options.packages.length && <p role="status">{t('This student has no packages with available credits.', '這位學員沒有可用堂數的套票。')}</p>}
          {lesson && options.packages.length > 0 && !lessonPackages.length && <p role="status">{t('No credit package is valid for this lesson.', '沒有適用於此課堂的有效堂數套票。')}</p>}
          {override && <><p>{t('Student level', '學員程度')} · {student.level} / {t('Class level', '班別程度')} · {lesson.level}</p>
            <label htmlFor={`${id}-reason`}>{t('Level override reason', '程度例外原因')}</label>
            <textarea id={`${id}-reason`} required maxLength={500} value={reason} onChange={event => setReason(event.target.value)}/>
          </>}
          <button disabled={!lesson || !creditPackage || (override && !reason.trim())}>{t('Review booking', '檢查預約')}</button>
        </form>}
        <button className="outline" onClick={() => setOpen(false)}>{t('Cancel booking form', '取消預約表格')}</button>
      </>}
    </div>}
  </section>;
}
