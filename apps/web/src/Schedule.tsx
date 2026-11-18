import { Fragment, useEffect, useRef, useState, type FormEvent, type MouseEvent } from 'react';
import type { Route } from './navigation';
import ClassRoster from './ClassRoster';
import CancellationImpact from './CancellationImpact';
import SubstituteCoach from './SubstituteCoach';
import { APIError, type User } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';
import { hkDate, hkTime, rangeEnd, regionNames, type Venue, type ClassType, type ClassSession, type ClassSeries, type AdminUser } from './scheduling';

export default function Schedule({ csrfToken, expired, t, role, follow }: ProtectedProps & { role: User['role']; follow?: (event: MouseEvent<HTMLAnchorElement>, next: Route) => void }) {
  const api = useProtectedRequest(expired);
  const listGeneration = useRef(0);
  const [from, setFrom] = useState(hkDate);
  const [to, setTo] = useState(() => rangeEnd(hkDate()));
  const [venueFilter, setVenueFilter] = useState('');
  const [coachFilter, setCoachFilter] = useState('');
  const [regionFilter, setRegionFilter] = useState('');
  const [impactClassId, setImpactClassId] = useState<string | null>(null);
  const [substituteClassId, setSubstituteClassId] = useState<string | null>(null);
  const [rosterClassId, setRosterClassId] = useState<string | null>(null);
  const [classes, setClasses] = useState<ClassSession[]>([]);
  const [venues, setVenues] = useState<Venue[]>([]);
  const [types, setTypes] = useState<ClassType[]>([]);
  const [coaches, setCoaches] = useState<AdminUser[]>([]);
  const [series, setSeries] = useState<ClassSeries[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [loadedRange, setLoadedRange] = useState('');
  const [form, setForm] = useState<'single' | 'series' | null>(null);
  const [editing, setEditing] = useState<ClassSession | null>(null);
  const [venueId, setVenueId] = useState('');
  const [classTypeId, setClassTypeId] = useState('');
  const [coachId, setCoachId] = useState('');
  const [date, setDate] = useState(hkDate);
  const [endDate, setEndDate] = useState(hkDate);
  const [weekday, setWeekday] = useState(1);
  const [startTime, setStartTime] = useState('09:00');
  const [endTime, setEndTime] = useState('10:00');
  const [capacity, setCapacity] = useState(6);
  const regions = regionNames(t);
  const weekdays = [t('Monday', '星期一'), t('Tuesday', '星期二'), t('Wednesday', '星期三'), t('Thursday', '星期四'), t('Friday', '星期五'), t('Saturday', '星期六'), t('Sunday', '星期日')];
  const admin = role === 'admin';

  async function load() {
    const current = ++listGeneration.current;
    setRosterClassId(null);
    if (!from || !to || to < from || (Date.parse(to) - Date.parse(from)) / 86400000 > 92) { setError(t('Choose a date range of at most 93 days, with the end on or after the start.', '請選擇最多 93 日的日期範圍，結束日期不可早於開始日期。')); setLoading(false); return; }
    setLoading(true); setError('');
    const query = new URLSearchParams({ from, to });
    if (venueFilter) query.set('venueId', venueFilter);
    if (coachFilter) query.set('coachId', coachFilter);
    if (regionFilter) query.set('region', regionFilter);
    try {
      const results = await Promise.all([
        api<{items: ClassSession[]}>(`/classes?${query}`), api<{items: Venue[]}>('/venues'), api<{items: ClassType[]}>('/class-types'),
        admin ? api<{items: AdminUser[]}>('/users?role=coach') : Promise.resolve({ items: [] }),
        admin ? api<{items: ClassSeries[]}>(`/class-series?${new URLSearchParams({ from, to })}`) : Promise.resolve({ items: [] }),
      ]);
      if (current !== listGeneration.current) return;
      setLoadedRange(`${from}–${to}`); setClasses(results[0].items); setVenues(results[1].items); setTypes(results[2].items); setCoaches(results[3].items); setSeries(results[4].items);
    } catch (e) { if (current === listGeneration.current && !isAborted(e)) { setClasses([]); setSeries([]); setLoadedRange(''); setError(t('Could not load the schedule. Please retry.', '未能載入時間表，請重試。')); } }
    finally { if (current === listGeneration.current) setLoading(false); }
  }
  useEffect(() => { void load(); }, [api]);
  function newClass(mode: 'single' | 'series') {
    setForm(mode); setEditing(null); setVenueId(''); setClassTypeId(''); setCoachId(''); setDate(from); setEndDate(mode === 'series' ? to : from); setStartTime('09:00'); setEndTime('10:00'); setCapacity(6); setWeekday(1); setError(''); setNotice('');
  }
  function edit(value: ClassSession) {
    setForm('single'); setEditing(value); setVenueId(value.venueId); setClassTypeId(value.classTypeId); setCoachId(value.coachId); setDate(hkDate(new Date(value.startsAt))); setEndDate(hkDate(new Date(value.endsAt))); setStartTime(hkTime(value.startsAt)); setEndTime(hkTime(value.endsAt)); setCapacity(value.capacity); setError(''); setNotice('');
  }
  async function save(event: FormEvent) {
    event.preventDefault(); setError(''); setNotice('');
    if ((form === 'series' && startTime >= endTime) || (form === 'single' && `${endDate}T${endTime}` <= `${date}T${startTime}`)) { setError(t('End must be after start; weekly classes must finish on the same day.', '結束時間須在開始之後；每週課堂須於同一天結束。')); return; }
    if (form === 'series' && (endDate < date || (Date.parse(endDate) - Date.parse(date)) / 86400000 > 92)) { setError(t('Weekly series must span at most 93 days.', '每週系列最多可跨越 93 日。')); return; }
    setBusy(true);
    const common = { venueId, classTypeId, coachId, capacity };
    const input = form === 'series' ? { ...common, startDate: date, endDate, weekday, startTime, endTime } : { ...common, startsAt: `${date}T${startTime}:00+08:00`, endsAt: `${endDate}T${endTime}:00+08:00`, ...(editing ? { version: editing.version } : {}) };
    try {
      const saved = await api<ClassSession | ClassSeries>(form === 'series' ? '/class-series' : `/classes${editing ? `/${editing.id}` : ''}`, { method: editing ? 'PATCH' : 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(input) });
      setNotice('sessions' in saved ? t(`Weekly series created with ${saved.sessions.length} classes.`, `已建立每週系列，共 ${saved.sessions.length} 堂。`) : t('Class saved. Check the date filter if it falls outside the current view.', '已儲存課堂。如不在目前範圍內，請調整日期篩選。')); setForm(null); setEditing(null); await load();
    } catch (e) { if (!isAborted(e)) setError(e instanceof APIError && e.code === 'substitution_required' ? t('This class has bookings. Use Substitute coach so families and both coaches are told.', '此課堂已有預約，請使用「代課教練」，以便通知家庭及兩位教練。') : e instanceof APIError && e.status === 409 ? t('This class changed or conflicts with the coach’s schedule or travel time. Your entries are preserved; review the schedule before retrying.', '此課堂已變更，或與教練時間表及交通時間衝突。已保留輸入內容，請檢查時間表後再試。') + (e.message ? ` (${e.message})` : '') : t('Could not save the class. Your entries are preserved; check the details and try again.', '未能儲存課堂。已保留輸入內容，請檢查後再試。')); }
    finally { setBusy(false); }
  }
  const selectableVenues = venues.filter(v => v.active || v.id === editing?.venueId);
  const selectableTypes = types.filter(c => c.active || c.id === editing?.classTypeId);
  const selectableCoaches = coaches.filter(c => c.active || c.id === editing?.coachId);
  return <section><div className="page-heading"><div><p className="eyebrow">{t('SCHOOL TIMETABLE', '學校時間表')}</p><h1>{role === 'coach' ? t('Your teaching schedule', '你的授課時間表') : admin ? t('Class schedule', '課堂時間表') : t('Class catalogue', '課堂目錄')}</h1><p className="muted">{role === 'coach' ? t('Classes assigned to you. All dates and times are Hong Kong time.', '獲分配給你的課堂。所有日期及時間均為香港時間。') : t('All dates and times are Hong Kong time.', '所有日期及時間均為香港時間。')}</p>{!admin && role !== 'coach' && <p className="catalogue-note">{t('This is the school’s class catalogue, not your bookings.', '這是學校課堂目錄，並非你的預約。')}</p>}</div>{admin && <div className="heading-actions"><button disabled={busy || loading} onClick={() => newClass('single')}>{t('Add class', '新增課堂')}</button><button className="outline" disabled={busy || loading} onClick={() => newClass('series')}>{t('Add weekly series', '新增每週系列')}</button></div>}</div>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    {form && <section className="panel"><h2>{form === 'series' ? t('New weekly series', '新增每週系列') : editing ? t('Edit class', '編輯課堂') : t('New class', '新增課堂')}</h2>{editing?.seriesId && <p className="muted">{t('This changes only this session, not the other classes in its weekly series.', '此操作只更改這一堂，不影響系列的其他課堂。')}</p>}<form onSubmit={save} className="field-grid"><div><label htmlFor="class-venue">{t('Class venue', '課堂場地')}</label><select id="class-venue" required value={venueId} onChange={e => setVenueId(e.target.value)}><option value="">{t('Choose a venue', '選擇場地')}</option>{selectableVenues.map(v => <option key={v.id} value={v.id}>{v.name}{!v.active ? ` (${t('archived', '已封存')})` : ''}</option>)}</select></div><div><label htmlFor="class-type">{t('Class type', '班別')}</label><select id="class-type" required value={classTypeId} onChange={e => setClassTypeId(e.target.value)}><option value="">{t('Choose a class type', '選擇班別')}</option>{selectableTypes.map(c => <option key={c.id} value={c.id}>{c.name} · {c.level}</option>)}</select></div><div><label htmlFor="class-coach">{t('Assigned coach', '授課教練')}</label><select id="class-coach" required value={coachId} onChange={e => setCoachId(e.target.value)}><option value="">{t('Choose a coach', '選擇教練')}</option>{selectableCoaches.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select></div><div><label htmlFor="class-capacity">{t('Capacity', '名額')}</label><input id="class-capacity" type="number" min={1} max={2147483647} step={1} required value={capacity} onChange={e => setCapacity(Number(e.target.value))}/></div><div><label htmlFor="class-date">{form === 'series' ? t('Series start date', '系列開始日期') : t('Class date', '課堂日期')}</label><input id="class-date" type="date" required value={date} onChange={e => { setDate(e.target.value); if (form === 'single' && (!editing || endDate === date)) setEndDate(e.target.value); }}/></div>{form === 'single' && <div><label htmlFor="class-end-date">{t('Class end date', '課堂結束日期')}</label><input id="class-end-date" type="date" required min={date} value={endDate} onChange={e => setEndDate(e.target.value)}/></div>}{form === 'series' && <><div><label htmlFor="series-end">{t('Series end date', '系列結束日期')}</label><input id="series-end" type="date" required min={date} value={endDate} onChange={e => setEndDate(e.target.value)}/></div><div><label htmlFor="series-weekday">{t('Repeat every', '每週重複')}</label><select id="series-weekday" value={weekday} onChange={e => setWeekday(Number(e.target.value))}>{weekdays.map((day, i) => <option value={i + 1} key={day}>{day}</option>)}</select></div></>}<div><label htmlFor="class-start">{t('Start time (Hong Kong)', '開始時間（香港）')}</label><input id="class-start" type="time" required value={startTime} onChange={e => setStartTime(e.target.value)}/></div><div><label htmlFor="class-end">{t('End time (Hong Kong)', '結束時間（香港）')}</label><input id="class-end" type="time" required value={endTime} onChange={e => setEndTime(e.target.value)}/></div><div className="form-actions full-row"><button disabled={busy}>{form === 'series' ? t('Create weekly series', '建立每週系列') : t('Save class', '儲存課堂')}</button><button type="button" className="outline" disabled={busy} onClick={() => { setForm(null); setEditing(null); setError(''); }}>{t('Cancel', '取消')}</button></div></form></section>}
    <form onSubmit={e => { e.preventDefault(); void load(); }} className="panel schedule-filters section-gap"><div><label htmlFor="schedule-from">{t('From date', '開始日期')}</label><input id="schedule-from" type="date" required value={from} onChange={e => setFrom(e.target.value)}/></div><div><label htmlFor="schedule-to">{t('To date', '結束日期')}</label><input id="schedule-to" type="date" required min={from} value={to} onChange={e => setTo(e.target.value)}/></div><div><label htmlFor="schedule-region">{t('Filter region', '篩選地區')}</label><select id="schedule-region" value={regionFilter} onChange={e => setRegionFilter(e.target.value)}><option value="">{t('All regions', '所有地區')}</option>{Object.entries(regions).map(([id, name]) => <option key={id} value={id}>{name}</option>)}</select></div><div><label htmlFor="schedule-venue">{t('Filter venue', '篩選場地')}</label><select id="schedule-venue" value={venueFilter} onChange={e => setVenueFilter(e.target.value)}><option value="">{t('All venues', '所有場地')}</option>{venues.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}</select></div>{admin && <div><label htmlFor="schedule-coach">{t('Filter coach', '篩選教練')}</label><select id="schedule-coach" value={coachFilter} onChange={e => setCoachFilter(e.target.value)}><option value="">{t('All coaches', '所有教練')}</option>{coaches.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select></div>}<button disabled={loading || busy}>{t('Apply filters', '套用篩選')}</button></form>
    {loading ? <p role="status" className="section-gap">{t('Loading classes…', '載入課堂中…')}</p> : <>
      <p role="status" className="result-count">{classes.length} {t('classes', '堂課')}{loadedRange && ` · ${t('Loaded dates', '已載入日期')}: ${loadedRange}`}</p>
      <div className="record-list">{classes.map(c => {
        const day = hkDate(new Date(c.startsAt));
        const endDay = hkDate(new Date(c.endsAt));
        const time = hkTime(c.startsAt);
        const label = `${c.classTypeName} ${day} ${time}`;
        const rosterOpen = rosterClassId === c.id;
        const impactOpen = admin && impactClassId === c.id;
        return <Fragment key={c.id}>
          <article className="panel class-card">
            <div className="class-date"><strong>{day}</strong><span>{time}–{day !== endDay && `${endDay} `}{hkTime(c.endsAt)}</span></div>
            <div className="class-description">
              <h2>{c.classTypeName}</h2><p>{c.venueName} · {regions[c.region]}</p>
              <p className="muted">{c.coachName} · {c.level} · {t('Capacity', '名額')} {c.capacity}</p>
              {c.seriesId && <span className="tag">{t('Weekly series', '每週系列')}</span>}
              {c.cancelledAt && <span className="tag">{t('Cancelled by the school', '學校已取消')}</span>}
            </div>
            {(admin || role === 'coach') && <div className="heading-actions">
              {admin && !c.cancelledAt && <button className="outline" disabled={busy} onClick={() => edit(c)} aria-label={`${t('Edit class', '編輯課堂')} ${label}`}>{t('Edit', '編輯')}</button>}
              {admin && !c.cancelledAt && Date.parse(c.startsAt) > Date.now() && <button className="outline" aria-expanded={substituteClassId === c.id} aria-label={`${t('Substitute coach', '代課教練')} ${label}`}
                onClick={() => setSubstituteClassId(substituteClassId === c.id ? null : c.id)}>{t('Substitute coach', '代課教練')}</button>}
              {admin && <button className="outline" aria-expanded={impactOpen} aria-label={`${t('Review cancellation impact', '檢視取消課堂影響')} ${label}`}
                onClick={() => setImpactClassId(impactOpen ? null : c.id)}>{t('Review cancellation impact', '檢視取消課堂影響')}</button>}
              <button className="outline" aria-expanded={rosterOpen} aria-label={`${t('View roster', '查看名單')} ${label}`}
                onClick={() => setRosterClassId(rosterOpen ? null : c.id)}>{t('View roster', '查看名單')}</button>
            </div>}
          </article>
          {admin && substituteClassId === c.id && <SubstituteCoach lesson={c} coaches={coaches} csrfToken={csrfToken} expired={expired} t={t} onClose={() => setSubstituteClassId(null)}
            onSubstituted={lesson => { setClasses(previous => previous.map(item => item.id === lesson.id ? lesson : item)); setSubstituteClassId(null); setNotice(t(`${lesson.coachName} now teaches this lesson.`, `${lesson.coachName} 現負責此課堂。`)); }}/>}
          {impactOpen && <CancellationImpact classId={c.id} csrfToken={csrfToken} expired={expired} t={t} onCancelled={lesson => setClasses(previous => previous.map(item => item.id === lesson.id ? lesson : item))}/>}
          {rosterOpen && <ClassRoster follow={follow} classId={c.id} csrfToken={csrfToken} expired={expired} t={t}/>}
        </Fragment>;
      })}</div>
      {!classes.length && <p className="panel empty">{t('No classes match these filters. Try another date range or location.', '沒有符合篩選的課堂，請嘗試其他日期或地點。')}</p>}
    </>}
    {admin && series.length > 0 && <section className="section-gap"><h2>{t('Weekly series in this date range', '此日期範圍的每週系列')}</h2><div className="record-list">{series.map(s => <details className="panel" key={s.id}><summary>{s.startDate}–{s.endDate} · {weekdays[s.weekday - 1]} · {s.startTime}–{s.endTime}</summary><p className="muted">{t('Individual sessions can be edited in the schedule above.', '可在上方時間表編輯個別課堂。')}</p><ul>{s.sessions.map(c => <li key={c.id}>{hkDate(new Date(c.startsAt))} {hkTime(c.startsAt)}–{hkDate(new Date(c.startsAt)) !== hkDate(new Date(c.endsAt)) ? `${hkDate(new Date(c.endsAt))} ` : ''}{hkTime(c.endsAt)} · {c.classTypeName} · {c.venueName} · {c.coachName}</li>)}</ul></details>)}</div></section>}
  </section>;
}
