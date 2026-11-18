import { useEffect, useState, type FormEvent } from 'react';
import AvailabilitySettings from './AvailabilitySettings';
import BookingValiditySettings from './BookingValiditySettings';
import LessonSettings from './LessonSettings';
import SkillCatalogue from './SkillCatalogue';
import { APIError } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';
import { regionNames, type Venue, type ClassType, type SchedulingSettings } from './scheduling';

export default function SchoolSetup({ csrfToken, expired, t }: ProtectedProps) {
  const api = useProtectedRequest(expired);
  const [venues, setVenues] = useState<Venue[]>([]);
  const [types, setTypes] = useState<ClassType[]>([]);
  const [settings, setSettings] = useState<SchedulingSettings | null>(null);
  const [gap, setGap] = useState(30);
  const [venue, setVenue] = useState<Venue | null>(null);
  const [classType, setClassType] = useState<ClassType | null>(null);
  const [form, setForm] = useState<'venue' | 'type' | null>(null);
  const [name, setName] = useState('');
  const [address, setAddress] = useState('');
  const [region, setRegion] = useState<Venue['region']>('hk-island');
  const [level, setLevel] = useState('');
  const [active, setActive] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const regions = regionNames(t);
  const headers = { 'X-CSRF-Token': csrfToken };
  async function load() {
    setLoading(true); setError('');
    try { const [v, c, s] = await Promise.all([api<{items: Venue[]}>('/venues'), api<{items: ClassType[]}>('/class-types'), api<SchedulingSettings>('/settings/scheduling')]); setVenues(v.items); setTypes(c.items); setSettings(s); setGap(s.travelGapMinutes); }
    catch (e) { if (!isAborted(e)) setError(t('Could not load school settings. Please retry.', '未能載入學校設定，請重試。')); }
    finally { setLoading(false); }
  }
  useEffect(() => { void load(); }, [api]);
  function editVenue(value: Venue | null) { setVenue(value); setClassType(null); setForm('venue'); setName(value?.name || ''); setAddress(value?.address || ''); setRegion(value?.region || 'hk-island'); setActive(value?.active ?? true); setError(''); setNotice(''); }
  function editType(value: ClassType | null) { setClassType(value); setVenue(null); setForm('type'); setName(value?.name || ''); setLevel(value?.level || ''); setActive(value?.active ?? true); setError(''); setNotice(''); }
  async function save(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(''); setNotice('');
    const editing = form === 'venue' ? venue : classType;
    const input = form === 'venue' ? { name: name.trim(), address: address.trim(), region } : { name: name.trim(), level: level.trim() };
    try { await api(`/${form === 'venue' ? 'venues' : 'class-types'}${editing ? `/${editing.id}` : ''}`, { method: editing ? 'PATCH' : 'POST', headers, body: JSON.stringify({ ...input, ...(editing ? { active, version: editing.version } : {}) }) }); setForm(null); setNotice(t('School record saved.', '已儲存學校資料。')); await load(); }
    catch (e) { if (!isAborted(e)) setError(e instanceof APIError && e.status === 409 ? t('This record changed or conflicts with an existing record. Cancel, refresh and review before saving again.', '此資料已變更或與現有紀錄衝突。請取消、重新載入並檢查後再儲存。') : t('Could not save. Your entries are preserved; check the fields and retry.', '未能儲存。已保留輸入內容，請檢查後再試。')); }
    finally { setBusy(false); }
  }
  async function saveSettings(event: FormEvent) {
    event.preventDefault(); if (!settings) return;
    setBusy(true); setError(''); setNotice('');
    try { const value = await api<SchedulingSettings>('/settings/scheduling', { method: 'PATCH', headers, body: JSON.stringify({ travelGapMinutes: gap, version: settings.version }) }); setSettings(value); setNotice(t('Travel time saved.', '已儲存交通時間。')); }
    catch (e) { if (!isAborted(e)) setError(e instanceof APIError && e.status === 409 ? t('The settings changed or existing future classes conflict with this travel time. Refresh and review the schedule before retrying.', '設定已變更或現有未來課堂與此交通時間衝突。請重新載入並檢查時間表後再試。') : t('Could not save travel time. Your value is preserved.', '未能儲存交通時間，已保留輸入值。')); }
    finally { setBusy(false); }
  }
  return <section><div className="page-heading"><div><p className="eyebrow">{t('SCHOOL OPERATIONS', '學校營運')}</p><h1>{t('School setup', '學校設定')}</h1><p className="muted">{t('Manage teaching locations, class types and coach travel time.', '管理上課地點、班別及教練交通時間。')}</p></div><button className="outline" disabled={loading || busy} onClick={load}>{t('Refresh', '重新載入')}</button></div>{error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    {form && <section className="panel"><h2>{form === 'venue' ? venue ? t('Edit venue', '編輯場地') : t('New venue', '新增場地') : classType ? t('Edit class type', '編輯班別') : t('New class type', '新增班別')}</h2><form onSubmit={save} className="field-grid"><div><label htmlFor="resource-name">{form === 'venue' ? t('Venue name', '場地名稱') : t('Class type name', '班別名稱')}</label><input id="resource-name" required maxLength={100} value={name} onChange={e => setName(e.target.value)}/></div>{form === 'venue' ? <><div><label htmlFor="venue-region">{t('Region', '地區')}</label><select id="venue-region" value={region} onChange={e => setRegion(e.target.value as Venue['region'])}>{Object.entries(regions).map(([id, label]) => <option key={id} value={id}>{label}</option>)}</select></div><div className="full-row"><label htmlFor="venue-address">{t('Address', '地址')}</label><input id="venue-address" required maxLength={300} value={address} onChange={e => setAddress(e.target.value)}/></div></> : <div><label htmlFor="type-level">{t('Class level', '班別程度')}</label><input id="type-level" required maxLength={100} value={level} onChange={e => setLevel(e.target.value)}/></div>}{(venue || classType) && <div className="full-row"><label className="checkbox-label"><input type="checkbox" checked={active} onChange={e => setActive(e.target.checked)}/>{t('Available for new classes', '可供新課堂使用')}</label><p className="muted">{t('Uncheck to archive. Existing sessions remain in the schedule.', '取消勾選以封存，現有課堂仍保留在時間表。')}</p></div>}<div className="form-actions full-row"><button disabled={busy || !name.trim()}>{form === 'venue' ? t('Save venue', '儲存場地') : t('Save class type', '儲存班別')}</button><button type="button" className="outline" disabled={busy} onClick={() => { setForm(null); setError(''); }}>{t('Cancel', '取消')}</button></div></form></section>}
    {loading ? <p role="status">{t('Loading school setup…', '載入學校設定中…')}</p> : <><section className="section-gap"><div className="page-heading"><h2>{t('Venues', '場地')}</h2><button disabled={busy} onClick={() => editVenue(null)}>{t('Add venue', '新增場地')}</button></div><div className="record-list">{venues.map(v => <article className="panel record-row" key={v.id}><div><h3>{v.name}</h3><p>{v.address}</p><p className="muted">{regions[v.region]} · {v.active ? t('Active', '使用中') : t('Archived', '已封存')}</p></div><button className="outline" disabled={busy} aria-label={`${t('Edit venue', '編輯場地')} ${v.name}`} onClick={() => editVenue(v)}>{t('Edit', '編輯')}</button></article>)}{!venues.length && <p>{t('No venues yet.', '暫無場地。')}</p>}</div></section><section className="section-gap"><div className="page-heading"><h2>{t('Class types', '班別')}</h2><button disabled={busy} onClick={() => editType(null)}>{t('Add class type', '新增班別')}</button></div><div className="record-list">{types.map(c => <article className="panel record-row" key={c.id}><div><h3>{c.name}</h3><p className="muted">{c.level} · {c.active ? t('Active', '使用中') : t('Archived', '已封存')}</p></div><button className="outline" disabled={busy} aria-label={`${t('Edit class type', '編輯班別')} ${c.name}`} onClick={() => editType(c)}>{t('Edit', '編輯')}</button></article>)}{!types.length && <p>{t('No class types yet.', '暫無班別。')}</p>}</div></section>{settings && <section className="panel section-gap"><h2>{t('Coach travel time', '教練交通時間')}</h2><p className="muted">{t('Minimum gap between a coach’s classes at different venues. Classes at the same venue may run in parallel with different coaches.', '教練於不同場地授課所需的最短間隔。不同教練可在同一場地同時授課。')}</p><form onSubmit={saveSettings}><label htmlFor="travel-gap">{t('Travel gap (minutes)', '交通間隔（分鐘）')}</label><input id="travel-gap" type="number" min={0} max={1440} step={1} required value={gap} onChange={e => setGap(Number(e.target.value))}/><button className="section-gap" disabled={busy}>{t('Save travel time', '儲存交通時間')}</button></form></section>}</>}
    <SkillCatalogue csrfToken={csrfToken} expired={expired} t={t}/>
    <AvailabilitySettings csrfToken={csrfToken} expired={expired} t={t}/>
    <LessonSettings csrfToken={csrfToken} expired={expired} t={t}/>
    <BookingValiditySettings csrfToken={csrfToken} expired={expired} t={t}/>
  </section>;
}
