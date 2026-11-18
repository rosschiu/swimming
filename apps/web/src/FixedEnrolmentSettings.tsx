import { useEffect, useState, type FormEvent } from 'react';
import { APIError } from './api';
import type { components } from './generated/api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Settings = components['schemas']['FixedEnrolmentSettings'];
const path = '/settings/fixed-enrolment';

export default function FixedEnrolmentSettings({ csrfToken, expired, t }: ProtectedProps) {
  const api = useProtectedRequest(expired);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [draft, setDraft] = useState<Partial<Omit<Settings, 'version'>>>({});
  const [busy, setBusy] = useState(true);
  const [message, setMessage] = useState<'load' | 'save' | 'stale' | 'saved' | null>(null);
  const allocation = draft.allocationPolicy ?? settings?.allocationPolicy;
  const renewal = draft.renewalPolicy ?? settings?.renewalPolicy;
  const changed = allocation !== settings?.allocationPolicy || renewal !== settings?.renewalPolicy;
  const messages = {
    load: t('Could not load fixed enrolment settings. Please retry.', '未能載入每週報名設定，請重試。'),
    save: t('Could not save fixed enrolment settings.', '未能儲存每週報名設定。'),
    stale: t('Fixed enrolment settings changed. Reload the settings before saving.', '每週報名設定已變更。請重新載入設定後再儲存。'),
    saved: t('Fixed enrolment settings saved.', '已儲存每週報名設定。'),
  };

  async function load() {
    setBusy(true);
    try {
      setSettings(await api<Settings>(path));
      setMessage(null);
    } catch (error) {
      if (!isAborted(error)) setMessage('load');
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => { void load(); }, [api]);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!settings || !changed || busy || message === 'stale' || message === 'load') return;
    setBusy(true);
    setMessage(null);
    try {
      const saved = await api<Settings>(path, {
        method: 'PATCH', headers: { 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ allocationPolicy: allocation, renewalPolicy: renewal, version: settings.version }),
      });
      setSettings(saved);
      setDraft({});
      setMessage('saved');
    } catch (error) {
      if (!isAborted(error)) setMessage(error instanceof APIError && error.status === 409 ? 'stale' : 'save');
    } finally {
      setBusy(false);
    }
  }

  return <section className="panel section-gap">
    <h2>{t('Fixed enrolment settings', '每週報名設定')}</h2>
    {message && <p role={message === 'saved' ? 'status' : 'alert'}>{messages[message]}</p>}
    {settings ? <form onSubmit={save}>
      <label htmlFor="weekly-allocation-policy">{t('When some weeks are unavailable', '部分週次無法預約時')}</label>
      <select id="weekly-allocation-policy" value={allocation} disabled={busy}
        onChange={event => setDraft(previous => ({ ...previous, allocationPolicy: event.target.value as Settings['allocationPolicy'] }))}>
        <option value="all_or_none">{t('Reserve all selected weeks together', '一次過預約所有所選週次')}</option>
        <option value="confirm_available_subset">{t('Let families confirm the available weeks', '讓家長重新確認可預約的週次')}</option>
      </select>
      <label htmlFor="weekly-renewal-policy">{t('Packages eligible for renewal', '可用於續報的套票')}</label>
      <select id="weekly-renewal-policy" value={renewal} disabled={busy}
        onChange={event => setDraft(previous => ({ ...previous, renewalPolicy: event.target.value as Settings['renewalPolicy'] }))}>
        <option value="same_offer">{t('A new paid package of the same offer', '同一優惠方案的新已付款套票')}</option>
        <option value="any_paid_package">{t('Any new paid package for the same student', '同一學員的任何新已付款套票')}</option>
      </select>
      <div className="form-actions">
        <button disabled={busy || !changed || message === 'stale' || message === 'load'}>{t('Save fixed enrolment settings', '儲存每週報名設定')}</button>
        <button type="button" className="outline" disabled={busy} onClick={load}>{t('Reload fixed enrolment settings', '重新載入每週報名設定')}</button>
      </div>
    </form> : busy ? <p role="status">{t('Loading fixed enrolment settings…', '正在載入每週報名設定…')}</p> :
      <button onClick={load}>{t('Retry fixed enrolment settings', '重試載入每週報名設定')}</button>}
  </section>;
}
