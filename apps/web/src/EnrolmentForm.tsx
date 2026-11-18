import { useEffect, useId, useRef, useState } from 'react';
import { APIError, type Student } from './api';
import type { components } from './generated/api';
import { bookingCreditCovers, classLabel, hkDate, type ClassSession } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Package = components['schemas']['CreditHistoryPackage'];
type Input = components['schemas']['EnrolmentInput'];
type Enrolment = components['schemas']['Enrolment'];
type PartialConfirmation = components['schemas']['PartialEnrolmentConfirmation'];
type CreditHistory = components['schemas']['CreditHistory'];
type RenewalInput = components['schemas']['EnrolmentRenewalInput'];
type Renewal = components['schemas']['EnrolmentRenewal'];
type StudentEnrolments = components['schemas']['StudentEnrolments'];
type Choices = { classes: ClassSession[]; packages: Package[]; validity: CreditHistory['bookingValidity']; enrolments: StudentEnrolments };

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
  const [renewing, setRenewing] = useState(false);
  const [sourceId, setSourceId] = useState('');
  const [carried, setCarried] = useState<number | null>(null);
  const [date, setDate] = useState(hkDate);
  const [choices, setChoices] = useState<Choices | 'error' | null>(null);
  const [seriesId, setSeriesId] = useState('');
  const [sessionIds, setSessionIds] = useState<string[]>([]);
  const [packageId, setPackageId] = useState('');
  const [reason, setReason] = useState('');
  const [partial, setPartial] = useState<PartialConfirmation | null>(null);
  const [review, setReview] = useState(false);
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState('');
  const [uncertain, setUncertain] = useState(false);
  const [reserved, setReserved] = useState(0);
  const sending = useRef(false);
  const lastIntent = useRef<{ fingerprint: string; input: Input & Partial<RenewalInput> } | null>(null);

  useEffect(() => {
    if (!search) return;
    let current = true;
    const query = new URLSearchParams({ from: search.from, to: endDate(search.from) });
    void Promise.all([api<{ items: ClassSession[] }>(`/classes?${query}`), api<CreditHistory>(`${path}/credit-history`),
      api<StudentEnrolments>(`${path}/enrolments`)])
      .then(([classes, history, enrolments]) => {
        if (current) setChoices({
          classes: classes.items.filter(lesson => lesson.seriesId && Date.parse(lesson.startsAt) > Date.now() && (admin || lesson.level === student.level)),
          packages: history.items,
          validity: history.bookingValidity,
          enrolments,
        });
      })
      .catch(error => { if (current && !isAborted(error)) setChoices('error'); });
    return () => { current = false; };
  }, [api, admin, path, search, student.level]);

  function load(from: string) {
    lastIntent.current = null;
    setSourceId('');
    setPartial(null);
    setSeriesId('');
    setSessionIds([]);
    setPackageId('');
    setReason('');
    setChoices(null);
    setSearch({ from });
  }

  const options = choices === 'error' ? null : choices;
  const source = options?.enrolments?.items.find(item => item.id === sourceId);
  const sourcePackage = options?.packages.find(item => item.id === source?.creditPackageId);
  const classes = options?.classes.filter(lesson => !source || Date.parse(lesson.startsAt) > Date.parse(source.lastLessonStartsAt)) ?? [];
  const series = new Map<string, ClassSession>();
  for (const session of classes) {
    if (session.seriesId && !series.has(session.seriesId)) series.set(session.seriesId, session);
  }
  const sessions = classes.filter(lesson => lesson.seriesId === seriesId);
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
      const result = await api<Enrolment | Renewal>(source
        ? `${path}/enrolments/${encodeURIComponent(source.id)}/renewals` : `${path}/enrolments`, {
        method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(lastIntent.current.input),
      });
      const times = result.bookings.map(booking => booking.class.startsAt);
      const expectedCount = lastIntent.current.input.partialConfirmation?.sessionIds.length ?? selected.length;
      if (times.length !== expectedCount || times.some(time => !Number.isFinite(Date.parse(time)))) throw new Error('Incomplete enrolment response');
      if (source && !('carriedForwardCredits' in result)) throw new Error('Incomplete renewal response');
      setCarried('carriedForwardCredits' in result ? result.carriedForwardCredits : null);
      setReserved(times.length);
      setPartial(null);
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
        if (error.code === 'partial_enrolment_confirmation_required') {
          const details = error.details as { availableSessionIds?: unknown; settingsVersion?: unknown } | undefined;
          const ids = details?.availableSessionIds;
          const version = details?.settingsVersion;
          if (Array.isArray(ids) && ids.length && new Set(ids).size === ids.length &&
            ids.every(id => selected.some(lesson => lesson.id === id)) && typeof version === 'number' && Number.isInteger(version) && version > 0) {
            setPartial({ sessionIds: ids, settingsVersion: version });
            return;
          }
        }
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
    booking_validity_changed: t('The school’s booking validity rule changed. Review the refreshed options and confirm again.', '全校預約有效期規則已變更。請查看最新選項並重新確認。'),
    partial_enrolment_confirmation_required: t('None of the selected weeks are available. Choose your weeks again.', '所選週次均無法預約，請重新選擇。'),
    fixed_enrolment_settings_changed: t('The school’s fixed enrolment settings changed. Review the refreshed options and confirm again.', '全校每週報名設定已變更，請查看最新選項並重新確認。'),
    class_unavailable: t('A selected lesson is no longer available.', '其中一堂所選課堂已無法預約。'),
    class_full: t('A selected lesson is full. Choose your weeks again.', '其中一堂所選課堂已滿，請重新選擇週次。'),
    student_overlap: t('A selected lesson overlaps with another booking.', '其中一堂所選課堂與另一項預約重疊。'),
    level_mismatch: t('A selected lesson differs from the student’s swimming level.', '其中一堂所選課堂與學員的游泳程度不符。'),
    renewal_package_ineligible: t('The chosen package is no longer eligible for renewal. Review the refreshed packages.', '所選套票已不符合續報資格，請查看最新套票。'),
    credit_unavailable: t('The package could not cover all selected lessons.', '套票無法支付所有所選課堂。'),
  };
  const settings = options?.enrolments?.settings;
  const mayConfirmSubset = settings?.allocationPolicy === 'confirm_available_subset';
  const packages = options?.packages.filter(item => {
    if (renewing) {
      if (!source || item.sourcePackageId || item.id === source.creditPackageId || item.purchaseId === source.purchaseId ||
        (settings?.renewalPolicy === 'same_offer' && (!source.offerId || item.offerId !== source.offerId))) return false;
    } else if (item.available < 1) {
      return false;
    }
    if (item.expiresOn && item.expiresOn < hkDate()) return false;
    const covers = (lesson: ClassSession) => bookingCreditCovers(item.expiresOn, lesson, options.validity.validityRule);
    return mayConfirmSubset && selected.length ? selected.some(covers) : selected.every(covers);
  }) ?? [];
  const creditsNeeded = mayConfirmSubset ? 1 : selected.length;
  const carryAvailable = source?.carryForwardEnabled && sourcePackage && (!sourcePackage.expiresOn || sourcePackage.expiresOn >= hkDate())
    ? sourcePackage.available : 0;
  const canReview = selected.length > 0 && selected.length <= 14 && !!credit && packages.includes(credit) &&
    (!renewing || !!source) && credit.available + carryAvailable >= creditsNeeded && (!override || !!reason.trim());

  function open(renewal: boolean) {
    setRenewing(renewal);
    setReserved(0);
    setCarried(null);
    setFailure('');
    const today = hkDate();
    setDate(today);
    load(today);
  }

  const reviewLessons = partial ? selected.filter(lesson => partial.sessionIds.includes(lesson.id)) : selected;

  return <section className="section-gap" aria-label={t('Weekly enrolment', '每週課堂報名')}>
    {!search && <div className="form-actions">
      <button className="outline" onClick={() => open(false)}>{t('Enrol in weekly lessons', '報名每週課堂')}</button>
      <button className="outline" onClick={() => open(true)}>{t('Renew weekly enrolment', '續報每週課堂')}</button>
    </div>}
    {reserved > 0 && <p className="success" role="status">{carried === null
      ? t(`Weekly enrolment confirmed. ${reserved} lessons and credits reserved.`, `已確認每週課堂報名，並預留 ${reserved} 堂課堂及其套票堂數。`)
      : t(`Renewal confirmed. ${reserved} lessons reserved and ${carried} credits carried forward.`, `已確認續報，預留 ${reserved} 堂課堂並轉移 ${carried} 堂套票堂數。`)}</p>}
    {search && <div className="panel">
      <h3>{renewing ? t('Renew weekly enrolment', '續報每週課堂') : t('Weekly enrolment', '每週課堂報名')}</h3>
      {failure && <p role="alert" className="error">{Object.hasOwn(messages, failure) ? messages[failure] : t('The enrolment could not be completed. Review the refreshed options and try again.', '未能完成報名，請檢查最新選項後再試。')}</p>}
      {review && credit && options ? <>
        <h4>{source ? t('Review weekly renewal', '檢查每週課堂續報') : t('Review weekly enrolment', '檢查每週課堂報名')}</h4>
        <p>{student.name} · {packageLabel(credit)}</p>
        {source && <>
          <p>{t('Renewal packages', '續報套票')} · {source.creditPackageId} → {credit.id}</p>
          <p>{source.carryForwardEnabled
            ? t('Unused, unreserved credits still valid in the old package will move to the new package’s expiry. The amount is calculated when you confirm.', '舊套票內仍有效的未用及未預留堂數將轉至新套票的到期日，轉移堂數會在確認時計算。')
            : t('The old package’s policy keeps its unused credits in that package.', '舊套票的規則將未使用堂數保留於原套票。')}</p>
          <p>{t('Previously reserved lessons stay on the old package.', '已預留課堂仍使用舊套票。')}</p>
        </>}
        <p>{options.validity.validityRule === 'lesson_start'
          ? t('The credit must be valid when each lesson starts.', '堂數須於每堂課開始時有效。')
          : t('The credit must be valid through the end of each lesson.', '堂數須有效至每堂課結束。')}</p>
        {partial && <>
          <p role="status">{t('Availability changed. Review the weeks below and confirm again.', '可預約的週次已變更。請檢查以下週次並重新確認。')}</p>
          {reviewLessons.length < selected.length && <p>{t('Weeks no longer available', '無法預約的週次')} · {selected.filter(lesson => !partial.sessionIds.includes(lesson.id)).map(lesson => hkDate(new Date(lesson.startsAt))).join(', ')}</p>}
        </>}
        <ul>{reviewLessons.map(lesson => <li key={lesson.id}>{classLabel(lesson)}</li>)}</ul>
        <p>{t(`Confirm to reserve ${reviewLessons.length} credits for the weeks shown above.`, `確認後預留 ${reviewLessons.length} 堂套票堂數，以預約以上週次。`)}</p>
        {override && <p>{t('Level override reason', '程度例外原因')} · {reason.trim()}</p>}
        {uncertain && <p role="alert" className="error">{t('The result could not be confirmed. Retry the same enrolment to check its outcome.', '未能確認報名結果。請重試同一次報名以確認結果。')}</p>}
        <div className="form-actions">
          <button disabled={pending} onClick={() => {
            if (partial && lastIntent.current && !uncertain) lastIntent.current.input.partialConfirmation = partial;
            void submit();
          }}>{pending ? t('Confirming…', '正在確認…') : uncertain ? t('Retry same enrolment', '重試同一次報名') : partial ? t('Confirm available weeks', '確認可預約的週次') : source ? t('Confirm renewal', '確認續報') : t('Confirm weekly enrolment', '確認每週課堂報名')}</button>
          {!uncertain && <button className="outline" disabled={pending} onClick={() => { setPartial(null); lastIntent.current = null; setReview(false); }}>{t('Edit enrolment', '編輯報名')}</button>}
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
              creditPackageId: credit.id, bookingValidityVersion: options.validity.version, ...(override ? { overrideReason: reason.trim() } : {}),
            };
            const request = { ...intent, settingsVersion: settings?.version, ...(source ? { sourcePackageId: source.creditPackageId } : {}) };
            const fingerprint = JSON.stringify({ ...request, sourceId });

            if (lastIntent.current?.fingerprint !== fingerprint) {
              lastIntent.current = { fingerprint, input: { ...request, idempotencyKey: crypto.randomUUID() } };
            }
            setReview(true);
          }}>
            {renewing && <>
              <label htmlFor={`${id}-source`}>{t('Enrolment to renew', '要續報的每週課堂')}</label>
              <select id={`${id}-source`} required value={sourceId} onChange={event => {
                setSourceId(event.target.value); setSeriesId(''); setSessionIds([]); setPackageId(''); setReason('');
              }}>
                <option value="">{t('Choose a previous enrolment', '選擇先前報名')}</option>
                {options.enrolments?.items.map(item => <option key={item.id} value={item.id}>
                  {hkDate(new Date(item.lastLessonStartsAt))} · {item.creditPackageId}
                </option>)}
              </select>
            </>}
            <label htmlFor={`${id}-series`}>{t('Weekly series', '每週系列')}</label>
            <select id={`${id}-series`} disabled={renewing && !source} required value={seriesId} onChange={event => { setSeriesId(event.target.value); setSessionIds([]); setReason(''); }}>
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
            <select id={`${id}-package`} required value={packages.some(item => item.id === packageId) ? packageId : ''} onChange={event => setPackageId(event.target.value)}>
              <option value="">{t('Choose a package', '選擇套票')}</option>
              {packages.map(item => <option value={item.id} key={item.id}>{packageLabel(item)}</option>)}
            </select>
            {!packages.length && <p role="status">{t('No package with available credits covers the selected lessons.', '沒有可用堂數的套票涵蓋所有所選課堂。')}</p>}
            {credit && !mayConfirmSubset && credit.available + (renewing ? carryAvailable : 0) < selected.length && <p role="status">{t('Choose a package with enough credits for every selected week.', '請選擇有足夠堂數支付所有所選週次的套票。')}</p>}
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
