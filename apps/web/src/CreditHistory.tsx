import GrantCredits from './GrantCredits';
import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError, type User } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';
import { hkDate } from './scheduling';
type Package = components['schemas']['CreditHistoryPackage'];
type Recovery = components['schemas']['CreditRecovery'];
type RecoveryInput = components['schemas']['CreditRecoveryInput'];
export default function CreditHistory({studentId, role, csrfToken, expired, t, revision = 0}: ProtectedProps & {studentId: string; role: User['role']; revision?: number}) {
  const api = useProtectedRequest(expired); const id = useId(); const sequence = useRef(0); const submitting = useRef(false);
  const [items, setItems] = useState<Package[]>([]); const [recoveries, setRecoveries] = useState<Recovery[]>([]);
  const [loading, setLoading] = useState(true); const [loadError, setLoadError] = useState(false); const [denied, setDenied] = useState(false);
  const [sourceId, setSourceId] = useState(''); const [quantity, setQuantity] = useState('1'); const [reason, setReason] = useState(''); const [date, setDate] = useState('');
  const [review, setReview] = useState(false); const [retry, setRetry] = useState(false); const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [notice, setNotice] = useState('');
  const pending = useRef<RecoveryInput | null>(null); const staff = role === 'admin' || role === 'coach';
  function deny() {sequence.current++; setItems([]); setRecoveries([]); setSourceId(''); pending.current = null; setReview(false); setRetry(false); setDenied(true); setLoading(false); setError(''); setNotice('');}
  async function load() {
    const current = ++sequence.current; setLoading(true); setLoadError(false);
    try {const result = await api<{items: Package[]; recoveries: Recovery[]}>(`/students/${studentId}/credit-history`); if (current === sequence.current) {setItems(result.items); setRecoveries(result.recoveries); setDenied(false);} }
    catch (e) {if (current === sequence.current && !isAborted(e)) {if (e instanceof APIError && (e.status === 403 || e.status === 404)) deny(); else {setItems([]); setRecoveries([]); setLoadError(true);} } }
    finally {if (current === sequence.current) setLoading(false);}
  }
  useEffect(() => {if (role !== 'student') void load();}, [api, studentId, revision]);
  if (role === 'student') return null;
  function begin(item: Package) {setSourceId(item.id); setQuantity('1'); setReason(''); setDate(''); setReview(false); setError(''); setNotice('');}
  function preview(event: FormEvent) {
    event.preventDefault(); if (submitting.current || pending.current) return;
    const source = items.find(item => item.id === sourceId); const amount = Number(quantity);
    if (!source || !Number.isInteger(amount) || amount < 1 || amount > Math.min(10000, source.expired) || !reason.trim() || [...reason.trim()].length > 500 || !date || date < hkDate()) {setError(t('Enter an available expired quantity, a reason of 1–500 characters and a new expiry date of today or later in Hong Kong.', '請輸入未恢復的已到期堂數、1 至 500 字原因，以及香港今日或之後的新到期日。')); return;}
    setReview(true); setError('');
  }
  async function save() {
    if (submitting.current || (!review && !pending.current)) return;
    pending.current ??= {sourcePackageId: sourceId, quantity: Number(quantity), reason: reason.trim(), expiresOn: date, idempotencyKey: crypto.randomUUID()};
    submitting.current = true; setBusy(true); setError(''); setNotice('');
    try {await api<Recovery>(`/students/${studentId}/credit-recoveries`, {method: 'POST', headers: {'X-CSRF-Token': csrfToken}, body: JSON.stringify(pending.current)}); pending.current = null; setRetry(false); setReview(false); setSourceId(''); setNotice(t('Expired credits recovered into a new linked package.', '已將到期堂數恢復至新的連結套票。')); await load();}
    catch (e) {if (!isAborted(e)) {
      if (e instanceof APIError && (e.status === 403 || e.status === 404)) {deny(); return;}
      if (!(e instanceof APIError) || e.status >= 500) {setRetry(true); setError(t('Recovery could not be confirmed. Retry the same recovery; your entries are locked to prevent duplicate transfers.', '未能確認恢復結果。請重試同一次恢復；輸入已鎖定以避免重複轉移。'));}
      else {pending.current = null; setRetry(false); setReview(false); setError(t('Recovery was not accepted. Your draft is preserved. Review the refreshed expired balance and your entries before submitting again.', '未能接受恢復操作。已保留草稿，請檢查重新載入的到期餘額及輸入後再提交。')); await load();}
    }} finally {submitting.current = false; setBusy(false);}
  }
  const source = items.find(item => item.id === sourceId); const locked = busy || loading || loadError || retry;
  return <section className="section-gap" aria-labelledby={`${id}-title`}><div className="page-heading"><h2 id={`${id}-title`}>{t('Credit history', '堂數紀錄')}</h2><button className="outline" disabled={busy || loading || retry} onClick={() => {setReview(false); void load();}}>{t('Reload credit history', '重新載入堂數紀錄')}</button></div><p className="muted">{staff ? t('Expiry dates include the whole Hong Kong date. Recovery transfers only expired unused credits into a new package; the original history stays intact.', '有效期包括香港到期日全日。恢復只會將已到期未使用堂數轉移至新套票，原有紀錄會保留。') : t('Credits can be used up to and including their expiry date (Hong Kong).', '堂數可於到期日（香港時間）當日或之前使用。')}</p>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    {denied ? <p role="alert">{t('Credit history is unavailable for your current access.', '目前權限無法存取此堂數紀錄。')}</p> : loading ? <p role="status">{t('Loading credit history…', '載入堂數紀錄中…')}</p> : loadError ? <p role="alert">{t('Could not load credit history. Reload to retry.', '未能載入堂數紀錄，請重新載入。')}</p> : <><div className="record-list">{items.map(item => <article className="panel" key={item.id}><h3>{item.complimentary && !item.sourcePackageId ? t('Complimentary credits', '贈送堂數') : staff ? (item.sourcePackageId ? t('Recovered credit package', '已恢復堂數套票') : t('Original credit package', '原有堂數套票')) : item.sourcePackageId ? t('Restored credits', '已恢復堂數') : t(`${item.credits}-lesson package`, `${item.credits} 堂套票`)}</h3>{staff && <p>{item.id}</p>}{staff && item.sourcePackageId && <p>{t('Recovered from package', '恢復自套票')} {item.sourcePackageId}</p>}<p>{t('Expiry (inclusive, Hong Kong)', '到期日（包括當日，香港）')}: {item.expiresOn ?? t('No expiry', '無到期日')}</p><p>{t('Available', '可用')} {item.available} · {t('Reserved', '已預留')} {item.reserved} · {t('Consumed', '已使用')} {item.consumed} · {t('Expired', '已到期')} {item.expired}</p>{staff && item.expired > 0 && <button disabled={locked || !!sourceId} onClick={() => begin(item)}>{t('Recover expired credits', '恢復到期堂數')}</button>}<details><summary>{t('Credit movements', '堂數變動')}</summary><ul>{item.movements.map(movement => <li key={movement.id}><p>{movement.reason} · {movement.actorId ?? t('System', '系統')}</p><p>{t('Available', '可用')} {movement.available} · {t('Reserved', '已預留')} {movement.reserved} · {t('Consumed', '已使用')} {movement.consumed} · {t('Expired', '已到期')} {movement.expired}</p><time dateTime={movement.createdAt}>{new Date(movement.createdAt).toLocaleString(undefined, {timeZone: 'Asia/Hong_Kong'})}</time></li>)}</ul></details></article>)}</div>{!items.length && <p>{t('No credit history yet.', '暫無堂數紀錄。')}</p>}
    {role === 'admin' && <GrantCredits studentId={studentId} csrfToken={csrfToken} expired={expired} t={t} onGranted={() => void load()}/>}
    {staff && sourceId && <form className="panel section-gap" onSubmit={preview}><h3>{t('Recover expired credits', '恢復到期堂數')}</h3><p>{t('Source package', '來源套票')}: {sourceId} · {t('Expired credits remaining', '剩餘已到期堂數')}: {source?.expired ?? 0}</p><label htmlFor={`${id}-quantity`}>{t('Recovery quantity', '恢復堂數')}</label><input id={`${id}-quantity`} type="number" min={1} max={Math.min(10000, source?.expired ?? 0)} step={1} required disabled={locked || review} value={quantity} onChange={e => setQuantity(e.target.value)}/><label htmlFor={`${id}-reason`}>{t('Recovery reason', '恢復原因')}</label><textarea id={`${id}-reason`} required maxLength={500} disabled={locked || review} value={reason} onChange={e => setReason(e.target.value)}/><label htmlFor={`${id}-date`}>{t('New expiry date (Hong Kong)', '新到期日（香港）')}</label><input id={`${id}-date`} type="date" required min={hkDate()} disabled={locked || review} value={date} onChange={e => setDate(e.target.value)}/>{review && <div className="section-gap"><p>{t(`Transfer ${quantity} expired unused credits from this package into a new linked package, valid through ${date} in Hong Kong. The source expired balance will decrease by ${quantity}.`, `將此套票的 ${quantity} 堂已到期未使用堂數轉移至新的連結套票，有效期至香港 ${date}。來源到期餘額將減少 ${quantity} 堂。`)}</p><p style={{whiteSpace: 'pre-wrap'}}>{reason.trim()}</p><button type="button" disabled={busy || loading || loadError} onClick={save}>{retry ? t('Retry same recovery', '重試同一次恢復') : t('Confirm recovery', '確認恢復')}</button></div>}<div className="form-actions">{!review && <button disabled={locked || !source || source.expired < 1}>{t('Review recovery', '檢查恢復操作')}</button>}<button type="button" className="outline" disabled={busy || retry} onClick={() => {if (review) setReview(false); else setSourceId(''); setError('');}}>{review ? t('Edit recovery', '編輯恢復操作') : t('Cancel recovery', '取消恢復操作')}</button></div></form>}
    {!!recoveries.length && <section className="section-gap"><h3>{t('Recovery history', '恢復紀錄')}</h3><ul>{recoveries.map(recovery => <li key={recovery.id}><p>{recovery.quantity} {t('credits', '堂')} · {t('New expiry', '新到期日')} {recovery.expiresOn}</p><p>{t('From package', '來源套票')} {recovery.sourcePackageId} → {recovery.destinationPackageId}</p><p style={{whiteSpace: 'pre-wrap'}}>{recovery.reason}</p><time dateTime={recovery.createdAt}>{new Date(recovery.createdAt).toLocaleString(undefined, {timeZone: 'Asia/Hong_Kong'})}</time></li>)}</ul></section>}</>}
  </section>;
}
