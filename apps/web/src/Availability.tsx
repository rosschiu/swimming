import AvailabilityClasses from './AvailabilityClasses';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError, type User } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';
import { hkDate, hkTime, rangeEnd, type Venue, type AdminUser } from './scheduling';

type Slot = components['schemas']['AvailabilitySlot'];
type Settings = components['schemas']['AvailabilitySettings'];

export default function Availability({ role, csrfToken, expired, t }: ProtectedProps & { role: User['role'] }) {
  const api = useProtectedRequest(expired);
  const sequence = useRef(0);
  const [lessonWindow, setLessonWindow] = useState<Slot | null>(null);
  const [slots, setSlots] = useState<Slot[]>([]);
  const [venues, setVenues] = useState<Venue[]>([]);
  const [coaches, setCoaches] = useState<AdminUser[]>([]);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [from, setFrom] = useState(hkDate);
  const [to, setTo] = useState(() => rangeEnd(hkDate()));
  const [coachFilter, setCoachFilter] = useState('');
  const [venueFilter, setVenueFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [loadedRange, setLoadedRange] = useState('');
  const [editing, setEditing] = useState<Slot | null>(null);
  const [form, setForm] = useState(false);
  const [venueId, setVenueId] = useState('');
  const [startDate, setStartDate] = useState(hkDate);
  const [endDate, setEndDate] = useState(hkDate);
  const [startTime, setStartTime] = useState('09:00');
  const [endTime, setEndTime] = useState('10:00');
  const [capacity, setCapacity] = useState(6);
  const [cancelling, setCancelling] = useState<Slot | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const admin = role === 'admin';
  const headers = { 'X-CSRF-Token': csrfToken };
  async function load() {
    const current = ++sequence.current;
    if (!from || !to || to < from || (Date.parse(to) - Date.parse(from)) / 86400000 > 92) { setError(t('Choose a date range of at most 93 days.', '請選擇最多 93 日的日期範圍。')); setLoading(false); return; }
    setLoading(true); setError('');
    const query = new URLSearchParams({ from, to });
    if (coachFilter) query.set('coachId', coachFilter);
    if (venueFilter) query.set('venueId', venueFilter);
    if (statusFilter) query.set('status', statusFilter);
    try {
      const [s, v, policy, c] = await Promise.all([api<{items: Slot[]}>(`/availability?${query}`), api<{items: Venue[]}>('/venues'), api<Settings>('/settings/availability'), admin ? api<{items: AdminUser[]}>('/users?role=coach') : Promise.resolve({ items: [] })]);
      if (current !== sequence.current) return;
      setSlots(s.items); setVenues(v.items); setSettings(policy); setCoaches(c.items); setLoadedRange(`${from}–${to}`);
    } catch (e) { if (current === sequence.current && !isAborted(e)) { setSlots([]); setLoadedRange(''); setError(t('Could not load availability. Please retry.', '未能載入可授課時間，請重試。')); } }
    finally { if (current === sequence.current) setLoading(false); }
  }
  useEffect(() => { void load(); }, [api]);
  function edit(slot: Slot | null) {
    setEditing(slot); setForm(true); setCancelling(null); setVenueId(slot?.venueId || '');
    const day = slot ? hkDate(new Date(slot.startsAt)) : hkDate(new Date(Date.now() + (settings?.lockHours || 0) * 3600000 + 86400000));
    setStartDate(day); setEndDate(slot ? hkDate(new Date(slot.endsAt)) : day); setStartTime(slot ? hkTime(slot.startsAt) : '09:00'); setEndTime(slot ? hkTime(slot.endsAt) : '10:00'); setCapacity(slot?.capacity || 6); setError(''); setNotice('');
  }
  async function save(event: FormEvent) {
    event.preventDefault(); setError(''); setNotice('');
    if (`${endDate}T${endTime}` <= `${startDate}T${startTime}`) { setError(t('End must be after start.', '結束時間須在開始時間之後。')); return; }
    setBusy(true);
    try {
      await api<Slot>(`/availability${editing ? `/${editing.id}` : ''}`, { method: editing ? 'PATCH' : 'POST', headers, body: JSON.stringify({ venueId, startsAt: `${startDate}T${startTime}:00+08:00`, endsAt: `${endDate}T${endTime}:00+08:00`, capacity, ...(editing ? { version: editing.version } : {}) }) });
      setForm(false); setEditing(null); setNotice(t('Availability saved. This does not create a class or booking; adjust the date filter if needed.', '已儲存可授課時間，此操作不會建立課堂或預約。如有需要，請調整日期篩選。')); await load();
    } catch (e) { if (!isAborted(e)) setError(e instanceof APIError && e.status === 409 ? t('This slot changed, overlaps another commitment, or is inside the editing cutoff. Your entries are preserved; refresh and review before retrying.', '此時段已變更、與其他安排重疊或已超過編輯期限。已保留輸入內容，請重新載入並檢查後再試。') + ` (${e.message})` : t('Could not save availability. Your entries are preserved; check the fields and try again.', '未能儲存可授課時間，已保留輸入內容。請檢查後再試。')); }
    finally { setBusy(false); }
  }
  async function cancel() {
    if (!cancelling) return; setBusy(true); setError(''); setNotice('');
    try { await api<Slot>(`/availability/${cancelling.id}/cancel`, { method: 'POST', headers, body: JSON.stringify({ version: cancelling.version }) }); setCancelling(null); setNotice(t('Availability cancelled.', '已取消可授課時間。')); await load(); }
    catch (e) { if (!isAborted(e)) setError(t('Could not cancel. The slot may have changed or reached its editing cutoff. Refresh and review before retrying.', '未能取消。時段可能已變更或超過編輯期限，請重新載入並檢查後再試。')); }
    finally { setBusy(false); }
  }
  return <section><div className="page-heading"><div><p className="eyebrow">{t('COACH PLANNING', '教練安排')}</p><h1>{t('Available time slots', '可授課時段')}</h1><p className="muted">{t('Planning information only. Submitted availability does not create or guarantee a scheduled class or booking.', '僅供安排參考。提交可授課時間不會建立或保證課堂及預約。')}</p>{settings && <p className="muted">{t(`Coaches cannot add, change or cancel slots within ${settings.lockHours} hours of their start. All times are Hong Kong time.`, `教練不可在開始前 ${settings.lockHours} 小時內新增、更改或取消時段。所有時間均為香港時間。`)}</p>}</div>{!admin && <button disabled={busy || loading || !settings} onClick={() => edit(null)}>{t('Add availability', '新增可授課時間')}</button>}</div>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    {lessonWindow && <AvailabilityClasses key={lessonWindow.id} slot={lessonWindow} admin={admin} close={() => setLessonWindow(null)} csrfToken={csrfToken} expired={expired} t={t}/>}
    {form && <section className="panel"><h2>{editing ? t('Edit availability', '編輯可授課時間') : t('New availability', '新增可授課時間')}</h2>{editing && <p className="muted">{t('Editing this availability does not change scheduled classes. Ask an administrator to update classes separately.', '更改此可授課時間不會更改已排定課堂。如需調整課堂，請聯絡管理員另行處理。')}</p>}<form className="field-grid" onSubmit={save}><div><label htmlFor="availability-venue">{t('Availability venue', '可授課場地')}</label><select id="availability-venue" required value={venueId} onChange={e => setVenueId(e.target.value)}><option value="">{t('Choose a venue', '選擇場地')}</option>{editing && !venues.some(v => v.id === editing.venueId) && <option value={editing.venueId}>{editing.venueName} ({t('archived', '已封存')})</option>}{venues.filter(v => v.active || v.id === editing?.venueId).map(v => <option key={v.id} value={v.id}>{v.name}</option>)}</select></div><div><label htmlFor="availability-capacity">{t('Capacity', '名額')}</label><input id="availability-capacity" type="number" min={1} max={2147483647} step={1} required value={capacity} onChange={e => setCapacity(Number(e.target.value))}/></div><div><label htmlFor="availability-start-date">{t('Start date', '開始日期')}</label><input id="availability-start-date" type="date" required value={startDate} onChange={e => { setStartDate(e.target.value); if (endDate === startDate) setEndDate(e.target.value); }}/></div><div><label htmlFor="availability-end-date">{t('End date', '結束日期')}</label><input id="availability-end-date" type="date" required min={startDate} value={endDate} onChange={e => setEndDate(e.target.value)}/></div><div><label htmlFor="availability-start-time">{t('Start time (Hong Kong)', '開始時間（香港）')}</label><input id="availability-start-time" type="time" required value={startTime} onChange={e => setStartTime(e.target.value)}/></div><div><label htmlFor="availability-end-time">{t('End time (Hong Kong)', '結束時間（香港）')}</label><input id="availability-end-time" type="time" required value={endTime} onChange={e => setEndTime(e.target.value)}/></div><div className="form-actions full-row"><button disabled={busy}>{t('Save availability', '儲存可授課時間')}</button><button type="button" className="outline" disabled={busy} onClick={() => { setForm(false); setEditing(null); setError(''); }}>{t('Cancel', '取消')}</button></div></form></section>}
    {cancelling && <section className="panel section-gap" aria-labelledby="cancel-slot-title"><h2 id="cancel-slot-title">{t('Cancel this availability?', '取消此可授課時間？')}</h2><p>{cancelling.venueName} · {hkDate(new Date(cancelling.startsAt))} {hkTime(cancelling.startsAt)}</p><p>{t('This removes the submitted slot from planning. It does not cancel a scheduled class.', '此操作會取消已提交的安排時段，不會取消任何已排定課堂。')}</p><div className="form-actions"><button disabled={busy} onClick={cancel}>{t('Confirm cancellation', '確認取消')}</button><button className="outline" disabled={busy} onClick={() => setCancelling(null)}>{t('Keep availability', '保留時段')}</button></div></section>}
    <form className="panel schedule-filters section-gap" onSubmit={e => { e.preventDefault(); void load(); }}><div><label htmlFor="availability-from">{t('From date', '開始日期')}</label><input id="availability-from" type="date" required value={from} onChange={e => setFrom(e.target.value)}/></div><div><label htmlFor="availability-to">{t('To date', '結束日期')}</label><input id="availability-to" type="date" required min={from} value={to} onChange={e => setTo(e.target.value)}/></div><div><label htmlFor="availability-status">{t('Filter status', '篩選狀態')}</label><select id="availability-status" value={statusFilter} onChange={e => setStatusFilter(e.target.value)}><option value="">{t('All statuses', '所有狀態')}</option><option value="submitted">{t('Submitted', '已提交')}</option><option value="cancelled">{t('Cancelled', '已取消')}</option></select></div><div><label htmlFor="availability-venue-filter">{t('Filter venue', '篩選場地')}</label><select id="availability-venue-filter" value={venueFilter} onChange={e => setVenueFilter(e.target.value)}><option value="">{t('All venues', '所有場地')}</option>{venues.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}</select></div>{admin && <div><label htmlFor="availability-coach-filter">{t('Filter coach', '篩選教練')}</label><select id="availability-coach-filter" value={coachFilter} onChange={e => setCoachFilter(e.target.value)}><option value="">{t('All coaches', '所有教練')}</option>{coaches.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select></div>}<button disabled={loading || busy}>{t('Apply filters', '套用篩選')}</button></form>
    {loading ? <p role="status" className="section-gap">{t('Loading availability…', '載入可授課時間中…')}</p> : <><p role="status" className="result-count">{slots.length} {t('slots', '個時段')}{loadedRange && ` · ${t('Loaded dates', '已載入日期')}: ${loadedRange}`}</p><div className="record-list">{slots.map(slot => <article className="panel" key={slot.id}><div className="class-card"><div className="class-date"><strong>{hkDate(new Date(slot.startsAt))}</strong><span>{hkTime(slot.startsAt)}–{hkDate(new Date(slot.startsAt)) !== hkDate(new Date(slot.endsAt)) ? `${hkDate(new Date(slot.endsAt))} ` : ''}{hkTime(slot.endsAt)}</span></div><div className="class-description"><h2>{slot.venueName}</h2><p>{slot.coachName} · {t('Proposed capacity', '建議名額')} {slot.capacity}</p><span className="tag">{slot.status === 'submitted' ? t('Submitted', '已提交') : t('Cancelled', '已取消')}</span></div><button className="outline" disabled={busy || !!lessonWindow} onClick={() => setLessonWindow(slot)}>{admin ? t('View / schedule lessons', '查看／安排課堂') : t('View scheduled lessons', '查看已排定課堂')}</button>{!admin && <div className="heading-actions"><button disabled={busy || !slot.editable} className="outline" aria-describedby={!slot.editable ? `lock-${slot.id}` : undefined} aria-label={`${t('Edit availability', '編輯可授課時間')} ${hkDate(new Date(slot.startsAt))} ${hkTime(slot.startsAt)}`} onClick={() => edit(slot)}>{t('Edit', '編輯')}</button><button disabled={busy || !slot.editable} className="outline" aria-describedby={!slot.editable ? `lock-${slot.id}` : undefined} aria-label={`${t('Cancel availability', '取消可授課時間')} ${hkDate(new Date(slot.startsAt))} ${hkTime(slot.startsAt)}`} onClick={() => { setCancelling(slot); setForm(false); setError(''); }}>{t('Cancel slot', '取消時段')}</button></div>}</div>{!admin && !slot.editable && <p id={`lock-${slot.id}`} className="muted section-gap">{slot.status === 'cancelled' ? t('Cancelled slots cannot be edited.', '已取消的時段不可編輯。') : t('This slot is locked by the editing cutoff.', '此時段已超過編輯期限。')}</p>}</article>)}</div>{!slots.length && <p className="panel empty">{t('No availability matches these filters.', '沒有符合篩選的可授課時間。')}</p>}</>}
  </section>;
}
