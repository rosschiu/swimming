import { useEffect, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Settings = components['schemas']['BookingValiditySettings'];
type Rule = Settings['validityRule'];
const path = '/settings/booking-validity';

export default function BookingValiditySettings({ csrfToken, expired, t }: ProtectedProps) {
  const api = useProtectedRequest(expired);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [draft, setDraft] = useState<Rule | null>(null);
  const [stale, setStale] = useState(false);
  const [busy, setBusy] = useState(true);
  const [message, setMessage] = useState<'load' | 'save' | 'stale' | 'saved' | null>(null);
  const labels: Record<Rule, string> = {
    lesson_end: t('Through lesson end', '有效至課堂結束'),
    lesson_start: t('At lesson start', '課堂開始時有效'),
  };
  const messages = {
    load: t('Could not load booking validity. Please retry.', '未能載入預約有效期設定，請重試。'),
    save: t('Could not save booking validity. Your selection is preserved.', '未能儲存預約有效期設定。已保留你的選擇。'),
    stale: t('Booking validity changed. Reload to review the current setting before saving. Your selection is preserved.', '預約有效期設定已變更。請重新載入並確認目前設定後再儲存。已保留你的選擇。'),
    saved: t('Booking validity saved.', '已儲存預約有效期設定。'),
  };

  async function load() {
    setBusy(true);
    setMessage(null);
    try {
      setSettings(await api<Settings>(path));
      setStale(false);
    } catch (error) {
      if (!isAborted(error)) setMessage('load');
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => { void load(); }, [api]);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || stale || !settings || !draft || draft === settings.validityRule) return;
    setBusy(true);
    setMessage(null);
    try {
      const value = await api<Settings>(path, {
        method: 'PATCH',
        headers: { 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ validityRule: draft, version: settings.version }),
      });
      setSettings(value);
      setDraft(null);
      setMessage('saved');
    } catch (error) {
      if (isAborted(error)) return;
      if (error instanceof APIError && error.status === 409) {
        setStale(true);
        setMessage('stale');
      } else {
        setMessage('save');
      }
    } finally {
      setBusy(false);
    }
  }

  return <section className="panel section-gap">
    <h2>{t('Booking validity', '預約有效期')}</h2>
    <p className="muted">{t('Choose when a credit must be valid for new bookings across the school. The default is through lesson end.', '設定全校新預約的堂數須在何時有效。預設為有效至課堂結束。')}</p>
    {message && <p role={message === 'saved' ? 'status' : 'alert'} className={message === 'saved' ? 'success' : 'error'}>{messages[message]}</p>}
    {settings ? <form onSubmit={save}>
      <p>{t('Last loaded setting', '上次載入的設定')} · {labels[settings.validityRule]}</p>
      <label htmlFor="booking-validity-rule">{t('Credit validity rule', '堂數有效期規則')}</label>
      <select id="booking-validity-rule" value={draft ?? settings.validityRule} disabled={busy}
        onChange={event => setDraft(event.target.value as Rule)}>
        <option value="lesson_end">{labels.lesson_end}</option>
        <option value="lesson_start">{labels.lesson_start}</option>
      </select>
      <div className="form-actions">
        <button disabled={busy || stale || !draft || draft === settings.validityRule}>{t('Save booking validity', '儲存預約有效期設定')}</button>
        <button type="button" className="outline" disabled={busy} onClick={load}>{t('Reload booking validity', '重新載入預約有效期設定')}</button>
      </div>
    </form> : busy ? <p role="status">{t('Loading booking validity…', '正在載入預約有效期設定…')}</p> :
      <button onClick={load}>{t('Retry booking validity', '重試載入預約有效期設定')}</button>}
  </section>;
}
