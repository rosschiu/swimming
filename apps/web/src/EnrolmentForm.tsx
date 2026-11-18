import { useEffect, useId, useRef, useState } from 'react';
import { APIError, type Student } from './api';
import type { components } from './generated/api';
import { classLabel, hkDate, type ClassSession } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Package = components['schemas']['CreditHistoryPackage'];
type Input = components['schemas']['EnrolmentInput'];
type Enrolment = components['schemas']['Enrolment'];
type Choices = { classes: ClassSession[]; packages: Package[] };

function endDate(from: string) {
  return hkDate(new Date(Date.parse(`${from}T12:00:00+08:00`) + 92 * 86400000));
}

export default function EnrolmentForm({ student, admin, csrfToken, expired, t, onBooked }: ProtectedProps & {
  student: Student; admin: boolean; onBooked: (startsAt: string[]) => void;
}) {
  const id = useId();
  const api = useProtectedRequest(expired);
  const path = `/students/${encodeURIComponent(student.id)}`;
  const [search, setSearch] = useState<{ from: string } | null>(null);
  const [date, setDate] = useState(hkDate);
  const [choices, setChoices] = useState<Choices | 'error' | null>(null);
  const [seriesId, setSeriesId] = useState('');
  const [sessionIds, setSessionIds] = useState<string[]>([]);
  const [packageId, setPackageId] = useState('');
  const [reason, setReason] = useState('');
  const [review, setReview] = useState(false);
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState('');
  const [uncertain, setUncertain] = useState(false);
  const [reserved, setReserved] = useState(0);
  const sending = useRef(false);
  const lastIntent = useRef<{ fingerprint: string; input: Input } | null>(null);

  useEffect(() => {
    if (!search) return;
    let current = true;
    const query = new URLSearchParams({ from: search.from, to: endDate(search.from) });
    void Promise.all([api<{ items: ClassSession[] }>(`/classes?${query}`), api<{ items: Package[] }>(`${path}/credit-history`)])
      .then(([classes, history]) => {
        if (current) setChoices({
          classes: classes.items.filter(lesson => lesson.seriesId && Date.parse(lesson.startsAt) > Date.now() && (admin || lesson.level === student.level)),
          packages: history.items.filter(item => item.available > 0),
        });
      })
      .catch(error => { if (current && !isAborted(error)) setChoices('error'); });
    return () => { current = false; };
  }, [api, admin, path, search, student.level]);

  function load(from: string) {
    setSeriesId('');
    setSessionIds([]);
    setPackageId('');
    setReason('');
    setChoices(null);
    setSearch({ from });
  }

  const options = choices === 'error' ? null : choices;
  const series = new Map<string, ClassSession>();
  for (const session of options?.classes ?? []) {
    if (session.seriesId && !series.has(session.seriesId)) series.set(session.seriesId, session);
  }
  const sessions = options?.classes.filter(lesson => lesson.seriesId === seriesId) ?? [];
  const selected = sessions.filter(lesson => sessionIds.includes(lesson.id));
  const credit = options?.packages.find(item => item.id === packageId);
  const override = admin && selected.some(lesson => lesson.level !== student.level);
  const packageLabel = (item: Package) => `${item.id} · ${t('Available', '可用堂數')} ${item.available} · ${item.expiresOn ? `${t('Expires', '到期日')} ${item.expiresOn}` : t('No expiry', '無到期日')}`;

  async function submit() {
    if (!lastIntent.current || sending.current) return;
    sending.current = true;
    setPending(true);
    setFailure('');
    try {
      const result = await api<Enrolment>(`${path}/enrolments`, {
        method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(lastIntent.current.input),
      });
      const times = result.bookings.map(booking => booking.class.startsAt);
      if (times.length !== selected.length || times.some(time => !Number.isFinite(Date.parse(time)))) throw new Error('Incomplete enrolment response');
      setReserved(times.length);
      setSearch(null);
      setReview(false);
      setUncertain(false);
      lastIntent.current = null;
      onBooked(times);
    } catch (error) {
      if (isAborted(error)) return;
      if (!(error instanceof APIError) || error.status >= 500 || error.status === 408 || error.status === 429) {
        setUncertain(true);
      } else {
        setUncertain(false);
        setReview(false);
        setFailure(error.code ?? 'unknown');
        if (search) load(search.from);
      }
    } finally {
      sending.current = false;
      setPending(false);
    }
  }

  const messages: Record<string, string> = {
    version_conflict: t('A selected lesson changed. Choose your weeks again.', '所選課堂已變更，請重新選擇週次。'),
    class_unavailable: t('A selected lesson is no longer available.', '其中一堂所選課堂已無法預約。'),
    class_full: t('A selected lesson is full. Choose your weeks again.', '其中一堂所選課堂已滿，請重新選擇週次。'),
    student_overlap: t('A selected lesson overlaps with another booking.', '其中一堂所選課堂與另一項預約重疊。'),
    level_mismatch: t('A selected lesson differs from the student’s swimming level.', '其中一堂所選課堂與學員的游泳程度不符。'),
    credit_unavailable: t('The package could not cover all selected lessons.', '套票無法支付所有所選課堂。'),
  };
  const canReview = selected.length > 0 && selected.length <= 14 && !!credit && credit.available >= selected.length && (!override || !!reason.trim());

  return <section className="section-gap" aria-label={t('Weekly enrolment', '每週課堂報名')}>
    {!search && <button className="outline" onClick={() => { setReserved(0); setFailure(''); const today = hkDate(); setDate(today); load(today); }}>{t('Enrol in weekly lessons', '報名每週課堂')}</button>}
    {reserved > 0 && <p className="success" role="status">{t(`Weekly enrolment confirmed. ${reserved} lessons and credits reserved.`, `已確認每週課堂報名，並預留 ${reserved} 堂課堂及其套票堂數。`)}</p>}
    {search && <div className="panel">
      <h3>{t('Weekly enrolment', '每週課堂報名')}</h3>
      {failure && <p role="alert" className="error">{Object.hasOwn(messages, failure) ? messages[failure] : t('The enrolment could not be completed. Review the refreshed options and try again.', '未能完成報名，請檢查最新選項後再試。')}</p>}
      {review && credit ? <>
        <h4>{t('Review weekly enrolment', '檢查每週課堂報名')}</h4>
        <p>{student.name} · {packageLabel(credit)}</p>
        <ul>{selected.map(lesson => <li key={lesson.id}>{classLabel(lesson)}</li>)}</ul>
        <p>{t(`Confirm to reserve ${selected.length} credits. All selected lessons are booked together.`, `確認後預留 ${selected.length} 堂套票堂數，並一次過預約所有所選課堂。`)}</p>
        {override && <p>{t('Level override reason', '程度例外原因')} · {reason.trim()}</p>}
        {uncertain && <p role="alert" className="error">{t('The result could not be confirmed. Retry the same enrolment to check its outcome.', '未能確認報名結果。請重試同一次報名以確認結果。')}</p>}
        <div className="form-actions">
          <button disabled={pending} onClick={() => void submit()}>{pending ? t('Confirming…', '正在確認…') : uncertain ? t('Retry same enrolment', '重試同一次報名') : t('Confirm weekly enrolment', '確認每週課堂報名')}</button>
          {!uncertain && <button className="outline" disabled={pending} onClick={() => setReview(false)}>{t('Edit enrolment', '編輯報名')}</button>}
        </div>
      </> : <>
        <form className="field-grid" onSubmit={event => { event.preventDefault(); setFailure(''); load(date); }}>
          <div><label htmlFor={`${id}-date`}>{t('Find weekly lessons from (Hong Kong)', '搜尋此日期起的每週課堂（香港）')}</label>
            <input id={`${id}-date`} type="date" required min={hkDate()} value={date} onChange={event => setDate(event.target.value)}/></div>
          <div className="form-actions"><button className="outline">{t('Load weekly lessons and credits', '載入每週課堂及堂數')}</button></div>
        </form>
        <p className="muted">{search.from}–{endDate(search.from)} · {t('Hong Kong time', '香港時間')}</p>
        {choices === 'error' ? <p role="alert">{t('Could not load enrolment options. Use the button above to retry.', '未能載入報名選項。請使用上方按鈕重試。')}</p>
          : !options ? <p role="status">{t('Loading enrolment options…', '正在載入報名選項…')}</p>
          : <form onSubmit={event => {
            event.preventDefault();
            if (!canReview || !credit || [...reason.trim()].length > 500) return;
            const intent = {
              seriesId, sessions: selected.map(lesson => ({ classId: lesson.id, classVersion: lesson.version, classTypeVersion: lesson.classTypeVersion })),
              creditPackageId: credit.id, ...(override ? { overrideReason: reason.trim() } : {}),
            };
            const fingerprint = JSON.stringify(intent);
            if (lastIntent.current?.fingerprint !== fingerprint) {
              lastIntent.current = { fingerprint, input: { ...intent, idempotencyKey: crypto.randomUUID() } };
            }
            setReview(true);
          }}>
            <label htmlFor={`${id}-series`}>{t('Weekly series', '每週系列')}</label>
            <select id={`${id}-series`} required value={seriesId} onChange={event => { setSeriesId(event.target.value); setSessionIds([]); setReason(''); }}>
              <option value="">{t('Choose a weekly series', '選擇每週系列')}</option>
              {[...series].map(([key, lesson]) => <option value={key} key={key}>{classLabel(lesson)} · {lesson.level}</option>)}
            </select>
            {!series.size && <p role="status">{t('No eligible weekly lessons on these dates.', '這段日期內沒有符合報名條件的每週課堂。')}</p>}
            {!!sessions.length && <fieldset>
              <legend>{t('Select lesson weeks', '選擇課堂週次')}</legend>
              {sessions.map(lesson => {
                const checked = sessionIds.includes(lesson.id);
                return <label className="checkbox-label" key={lesson.id}>
                  <input type="checkbox" checked={checked} disabled={!checked && sessionIds.length >= 14}
                    onChange={() => setSessionIds(previous => checked ? previous.filter(key => key !== lesson.id) : [...previous, lesson.id])}/>
                  {classLabel(lesson)} · {lesson.level}
                </label>;
              })}
            </fieldset>}
            <label htmlFor={`${id}-package`}>{t('Enrolment credit package', '報名所用堂數套票')}</label>
            <select id={`${id}-package`} required value={packageId} onChange={event => setPackageId(event.target.value)}>
              <option value="">{t('Choose a package', '選擇套票')}</option>
              {options.packages.map(item => <option value={item.id} key={item.id}>{packageLabel(item)}</option>)}
            </select>
            {!options.packages.length && <p role="status">{t('This student has no packages with available credits.', '這位學員沒有可用堂數的套票。')}</p>}
            {credit && credit.available < selected.length && <p role="status">{t('Choose a package with enough credits for every selected week.', '請選擇有足夠堂數支付所有所選週次的套票。')}</p>}
            {override && <><p>{t('The selected lessons include a level mismatch.', '所選課堂中有班別程度與學員不符。')}</p>
              <label htmlFor={`${id}-reason`}>{t('Level override reason', '程度例外原因')}</label>
              <textarea id={`${id}-reason`} required maxLength={500} value={reason} onChange={event => setReason(event.target.value)}/>
            </>}
            <button disabled={!canReview}>{t('Review enrolment', '檢查報名')}</button>
          </form>}
        <button className="outline" onClick={() => setSearch(null)}>{t('Cancel enrolment form', '取消報名表格')}</button>
      </>}
    </div>}
  </section>;
}
