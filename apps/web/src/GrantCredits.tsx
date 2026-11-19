import { useId, useRef, useState } from 'react';
import type { components } from './generated/api';
import { hkDate } from './scheduling';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Grant = components['schemas']['CreditGrant'];

// Administrators add complimentary credits in their own package; no purchase or receipt is created.
export default function GrantCredits({ studentId, csrfToken, expired, t, onGranted }: ProtectedProps & { studentId: string; onGranted: (grant: Grant) => void }) {
  const api = useProtectedRequest(expired);
  const id = useId();
  const [open, setOpen] = useState(false);
  const [credits, setCredits] = useState('1');
  const [expiresOn, setExpiresOn] = useState('');
  const [reason, setReason] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const intent = useRef<{ fingerprint: string; key: string } | null>(null);
  const amount = Number(credits);
  const valid = Number.isInteger(amount) && amount >= 1 && amount <= 100 && expiresOn >= hkDate() && !!reason.trim();

  async function submit() {
    if (!valid || saving) return;
    const body = { credits: amount, expiresOn, reason: reason.trim() };
    const fingerprint = JSON.stringify(body);
    if (intent.current?.fingerprint !== fingerprint) intent.current = { fingerprint, key: crypto.randomUUID() };
    setSaving(true); setError('');
    try {
      const grant = await api<Grant>(`/students/${encodeURIComponent(studentId)}/credit-grants`, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ ...body, idempotencyKey: intent.current.key }) });
      setNotice(t(`${grant.credits} complimentary credits added, usable until ${grant.expiresOn}.`, `已加入 ${grant.credits} 堂贈送堂數，有效至 ${grant.expiresOn}。`));
      setOpen(false); setCredits('1'); setExpiresOn(''); setReason(''); intent.current = null;
      onGranted(grant);
    } catch (cause) {
      if (!isAborted(cause)) setError(t('Could not add the credits. Your entries are preserved; try again.', '未能加入堂數，已保留輸入內容，請再試。'));
    } finally { setSaving(false); }
  }

  return <div className="section-gap">
    {notice && <p role="status" className="success">{notice}</p>}
    {!open ? <button type="button" className="outline" onClick={() => { setOpen(true); setNotice(''); }}>{t('Grant extra credits', '贈送額外堂數')}</button>
      : <div className="panel" role="group" aria-label={t('Grant extra credits', '贈送額外堂數')}>
        <p className="muted">{t('Complimentary credits go into their own package with their own expiry. They create no payment or receipt and cannot be used to renew a paid weekly enrolment.', '贈送堂數會放入獨立套票並有各自的到期日，不會產生付款或收據，亦不能用於續報已付款的每週課堂。')}</p>
        <label htmlFor={`${id}-credits`}>{t('Credits', '堂數')}</label>
        <input id={`${id}-credits`} type="number" min={1} max={100} step={1} value={credits} disabled={saving} onChange={event => setCredits(event.target.value)}/>
        <label htmlFor={`${id}-expiry`}>{t('Usable until (Hong Kong)', '有效至（香港）')}</label>
        <input id={`${id}-expiry`} type="date" min={hkDate()} value={expiresOn} disabled={saving} onChange={event => setExpiresOn(event.target.value)}/>
        <label htmlFor={`${id}-reason`}>{t('Reason shown in the audit history', '記錄於操作紀錄的原因')}</label>
        <input id={`${id}-reason`} maxLength={500} value={reason} disabled={saving} onChange={event => setReason(event.target.value)}/>
        {error && <p role="alert" className="error">{error}</p>}
        <div className="form-actions">
          <button disabled={!valid || saving} onClick={() => void submit()}>{saving ? t('Adding…', '加入中…') : t('Add complimentary credits', '加入贈送堂數')}</button>
          <button className="outline" disabled={saving} onClick={() => { setOpen(false); setError(''); }}>{t('Cancel', '取消')}</button>
        </div>
      </div>}
  </div>;
}
