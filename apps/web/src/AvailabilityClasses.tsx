import { useEffect, useRef, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';
import { hkDate, hkTime } from './scheduling';

type Slot = components['schemas']['AvailabilitySlot'];
type ClassSession = components['schemas']['ClassSession'];
type ClassType = components['schemas']['ClassType'];

export default function AvailabilityClasses({ slot: initial, admin, close, csrfToken, expired, t }: ProtectedProps & { slot: Slot; admin: boolean; close: () => void }) {
  const api = useProtectedRequest(expired);
  const heading = useRef<HTMLHeadingElement>(null);
  const [slot, setSlot] = useState(initial);
  const [classes, setClasses] = useState<ClassSession[]>([]);
  const [types, setTypes] = useState<ClassType[]>([]);
  const [kind, setKind] = useState('');
  const [start, setStart] = useState(`${hkDate(new Date(initial.startsAt))}T${hkTime(initial.startsAt)}`);
  const [end, setEnd] = useState(`${hkDate(new Date(initial.endsAt))}T${hkTime(initial.endsAt)}`);
  const [capacity, setCapacity] = useState(initial.capacity);
  const [ready, setReady] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const pending = useRef<components['schemas']['AvailabilityClassInput'] | null>(null);
  const [uncertain, setUncertain] = useState(false);
  async function load() {
    setLoading(true); setReady(false); setError('');
    try {
      const [current, linked, kinds] = await Promise.all([
        api<Slot>(`/availability/${initial.id}`),
        api<{items: ClassSession[]}>(`/availability/${initial.id}/classes`),
        admin ? api<{items: ClassType[]}>('/class-types') : Promise.resolve({ items: [] }),
      ]);
      setSlot(current); setClasses(linked.items); setTypes(kinds.items); setReady(true);
    } catch (e) { if (!isAborted(e)) setError(t('Could not load scheduled lessons. Retry before scheduling.', '未能載入已排定課堂，請重試後再安排。')); }
    finally { setLoading(false); }
  }
  useEffect(() => { heading.current?.focus(); void load(); }, [api]);
  async function save(event: FormEvent) {
    event.preventDefault(); if (busy) return;
    setError(''); setNotice('');
    if (!pending.current) {
      if (end <= start || Date.parse(`${start}:00+08:00`) < Date.parse(slot.startsAt) || Date.parse(`${end}:00+08:00`) > Date.parse(slot.endsAt)) { setError(t('The lesson must fit inside the window and end after it starts.', '課堂必須在此時段內，且結束時間須在開始時間之後。')); return; }
      pending.current = { requestId: crypto.randomUUID(), availabilityVersion: slot.version, classTypeId: kind, startsAt: `${start}:00+08:00`, endsAt: `${end}:00+08:00`, capacity };
    }
    setBusy(true);
    try {
      await api<ClassSession>(`/availability/${slot.id}/classes`, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(pending.current) });
      pending.current = null; setUncertain(false);
      setNotice(t('Lesson scheduled. It is now in the class schedule. You can schedule another lesson in this window.', '已安排課堂，可於課堂時間表查看。你可在此時段內安排另一課堂。'));
      await load();
    } catch (e) {
      if (!isAborted(e)) {
        if (e instanceof APIError && e.status >= 400 && e.status < 500) {
          pending.current = null; setUncertain(false);
          setError(t('Could not schedule. Your entries are preserved; refresh the window and check for conflicts before trying again.', '未能安排，已保留輸入內容。請重新載入時段並檢查衝突後再試。') + ` (${e.message})`);
        } else {
          setUncertain(true);
          setError(t('The result is uncertain. Retry this same submission to check safely before changing lesson details.', '未能確定結果。請重試同一提交以安全確認，然後才更改課堂資料。'));
        }
      }
    } finally { setBusy(false); }
  }
  return <section className="panel section-gap" aria-labelledby="window-lessons-title">
    <h2 ref={heading} tabIndex={-1} id="window-lessons-title">{t('Lessons from this window', '此時段安排的課堂')}</h2>
    <p>{slot.coachName} · {slot.venueName} · {hkDate(new Date(slot.startsAt))} {hkTime(slot.startsAt)}–{hkDate(new Date(slot.endsAt))} {hkTime(slot.endsAt)}</p>
    <p>{t('These are current lesson details. The window records their origin; changing or cancelling availability does not change scheduled lessons. Administrators manage lesson changes in the schedule.', '以下為目前課堂資料。此時段記錄課堂來源；更改或取消可授課時間不會更改已排定課堂。管理員須於時間表另行更改課堂。')}</p>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    <div className="form-actions"><button className="outline" disabled={busy || loading} onClick={load}>{t('Refresh window', '重新載入時段')}</button><button className="outline" disabled={busy || uncertain} onClick={close}>{t('Close lessons', '關閉課堂')}</button></div>
    {loading ? <p role="status">{t('Loading lessons…', '載入課堂中…')}</p> : <ul>{classes.map(c => <li key={c.id}>{c.classTypeName} · {c.coachName} · {c.venueName} · {hkDate(new Date(c.startsAt))} {hkTime(c.startsAt)}–{hkDate(new Date(c.endsAt))} {hkTime(c.endsAt)} · {t('Capacity', '名額')} {c.capacity}</li>)}</ul>}
    {!loading && !error && classes.length === 0 && <p>{t('No scheduled lessons to show.', '暫無可顯示的已排定課堂。')}</p>}
    {admin && (slot.status === 'submitted' || uncertain) && <form onSubmit={save} className="field-grid section-gap">
      <fieldset disabled={busy || loading || !ready || uncertain} className="full-row"><legend>{t('Schedule a lesson', '安排課堂')}</legend>
        <div><label htmlFor="window-class-type">{t('Lesson type', '課堂類型')}</label><select id="window-class-type" required value={kind} onChange={e => setKind(e.target.value)}><option value="">{t('Choose a class type', '選擇課堂類型')}</option>{types.filter(x => x.active).map(x => <option key={x.id} value={x.id}>{x.name}</option>)}</select></div>
        <div><label htmlFor="window-start">{t('Lesson start (Hong Kong)', '課堂開始（香港）')}</label><input id="window-start" type="datetime-local" required value={start} onChange={e => setStart(e.target.value)}/></div>
        <div><label htmlFor="window-end">{t('Lesson end (Hong Kong)', '課堂結束（香港）')}</label><input id="window-end" type="datetime-local" required value={end} onChange={e => setEnd(e.target.value)}/></div>
        <div><label htmlFor="window-capacity">{t('Lesson capacity', '課堂名額')}</label><input id="window-capacity" type="number" min={1} max={2147483647} required value={capacity} onChange={e => setCapacity(Number(e.target.value))}/></div>
      </fieldset>
      <button disabled={busy || loading || (!ready && !uncertain)}>{uncertain ? t('Retry same submission', '重試同一提交') : t('Schedule lesson', '安排課堂')}</button>
    </form>}
  </section>;
}
