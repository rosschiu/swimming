import { useEffect, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Settings = components['schemas']['AvailabilitySettings'];
export default function AvailabilitySettings({ csrfToken, expired, t }: ProtectedProps) {
  const api = useProtectedRequest(expired);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [hours, setHours] = useState(72);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  async function load() {
    setBusy(true); setError('');
    try { const value = await api<Settings>('/settings/availability'); setSettings(value); setHours(value.lockHours); }
    catch (e) { if (!isAborted(e)) setError(t('Could not load the availability cutoff. Please retry.', '未能載入可授課時間期限，請重試。')); }
    finally { setBusy(false); }
  }
  useEffect(() => { void load(); }, [api]);
  async function save(event: FormEvent) {
    event.preventDefault(); if (!settings) return;
    setBusy(true); setError(''); setNotice('');
    try { const value = await api<Settings>('/settings/availability', { method: 'PATCH', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ lockHours: hours, version: settings.version }) }); setSettings(value); setNotice(t('Availability cutoff saved.', '已儲存可授課時間期限。')); }
    catch (e) { if (!isAborted(e)) setError(e instanceof APIError && e.status === 409 ? t('The cutoff changed. Reload before saving again; your entry is preserved.', '期限已變更，請重新載入後再儲存。已保留輸入值。') : t('Could not save the cutoff. Your entry is preserved.', '未能儲存期限，已保留輸入值。')); }
    finally { setBusy(false); }
  }
  return <section className="panel section-gap"><h2>{t('Availability editing cutoff', '可授課時間編輯期限')}</h2><p className="muted">{t('Coaches cannot create, edit or cancel availability inside this many hours before the slot starts. This setting does not publish classes.', '教練不可在時段開始前指定小時內新增、編輯或取消可授課時間。此設定不會發佈課堂。')}</p>{error && <p className="error" role="alert">{error}</p>}{notice && <p className="success" role="status">{notice}</p>}{settings ? <form onSubmit={save}><label htmlFor="availability-lock">{t('Availability lock (hours)', '可授課時間鎖定（小時）')}</label><input id="availability-lock" type="number" min={0} max={8760} step={1} required value={hours} onChange={e => setHours(Number(e.target.value))}/><div className="form-actions"><button disabled={busy}>{t('Save availability cutoff', '儲存可授課時間期限')}</button><button type="button" className="outline" disabled={busy} onClick={load}>{t('Reload cutoff', '重新載入期限')}</button></div></form> : <button disabled={busy} onClick={load}>{busy ? t('Loading cutoff…', '載入期限中…') : t('Retry cutoff', '重試載入期限')}</button>}</section>;
}
