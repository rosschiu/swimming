import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';
import { money, PolicyTerms } from './PackageCatalogue';
import { hkDate } from './scheduling';
type Purchase = components['schemas']['Purchase'];
type Attempt = components['schemas']['PaymentAttempt'];
type Receipt = components['schemas']['Receipt'];
type CreditPackage = components['schemas']['CreditPackage'];
export function Credits({ items, t }: { items: CreditPackage[]; t: ProtectedProps['t'] }) {
  return <section className="section-gap"><h2>{t('Credits and ledger', '堂數及明細')}</h2><p className="muted">{t('Expired credits remain in history and cannot be used.', '已到期堂數保留於歷史紀錄，不能使用。')}</p>{!items.length && <p>{t('No credit packages yet.', '暫無堂數套票。')}</p>}<div className="record-list">{items.map(item => <article className="panel" key={item.id}><h3>{t('Credit package', '堂數套票')} {item.id}</h3><p>{t('Purchase', '購買')} {item.purchaseId}</p><p>{t('Expiry (inclusive, Hong Kong)', '到期日（包括當日，香港）')}: {item.expiresOn ?? t('No expiry', '無到期日')}</p><dl><dt>{t('Available', '可用')}</dt><dd>{item.available}</dd><dt>{t('Reserved', '已預留')}</dt><dd>{item.reserved}</dd><dt>{t('Consumed', '已使用')}</dt><dd>{item.consumed}</dd><dt>{t('Expired', '已到期')}</dt><dd>{item.expired}</dd></dl><details><summary>{t('Credit movements', '堂數變動')}</summary><ul>{item.movements.map(movement => <li key={movement.id}><p>{movement.reason} · <time dateTime={movement.createdAt}>{new Date(movement.createdAt).toLocaleString(undefined, {timeZone: 'Asia/Hong_Kong'})}</time></p><p>{t('Available', '可用')} {movement.available} · {t('Reserved', '已預留')} {movement.reserved} · {t('Consumed', '已使用')} {movement.consumed} · {t('Expired', '已到期')} {movement.expired}</p></li>)}</ul></details></article>)}</div></section>;
}
export default function PurchaseHistory({ items, admin, csrfToken, expired, t, reload, invalidate, loading, studentNames }: ProtectedProps & { items: Purchase[]; admin: boolean; studentNames: Record<string, string>; reload: () => Promise<void>; invalidate: () => void; loading: boolean }) {
  return <section className="section-gap"><h2>{admin ? t('Purchases and expiry queue', '購買及到期日待辦') : t('Purchase history', '購買紀錄')}</h2>{!items.length && <p>{t('No purchases yet.', '暫無購買紀錄。')}</p>}<div className="record-list">{items.map(item => <PurchaseCard key={item.id} purchase={item} studentName={studentNames[item.studentId]} admin={admin} csrfToken={csrfToken} expired={expired} t={t} reload={reload} invalidate={invalidate} loading={loading}/>)}</div></section>;
}
function PurchaseCard({ purchase, admin, csrfToken, expired, t, reload, invalidate, loading, studentName }: ProtectedProps & { purchase: Purchase; admin: boolean; studentName?: string; reload: () => Promise<void>; invalidate: () => void; loading: boolean }) {
  const api = useProtectedRequest(expired);
  const expiryInputId = useId();
  const [date, setDate] = useState(''); const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [notice, setNotice] = useState('');
  const [receipt, setReceipt] = useState<Receipt | null>(null); const [review, setReview] = useState(false);
  const [retry, setRetry] = useState(false);
  const pending = useRef<{path: string; body: object} | null>(null); const submitting = useRef(false);
  useEffect(() => { setReceipt(null); }, [purchase.receiptId]);
  const scopeError = (e: unknown) => e instanceof APIError && (e.status === 403 || e.status === 404);
  async function mutate(path: string, body: object, uncertainReplay = true) {
    if (submitting.current || review) return;
    pending.current ??= { path, body }; submitting.current = true; setBusy(true); setError(''); setNotice('');
    try {
      await api<Purchase | Attempt>(pending.current.path, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(pending.current.body) });
      pending.current = null; setRetry(false); setNotice(t('Simulated workflow updated.', '已更新模擬流程。')); await reload();
    } catch (e) {
      if (!isAborted(e)) {
        if (scopeError(e)) { invalidate(); return; }
        const uncertain = !(e instanceof APIError) || e.status >= 500;
        if (uncertain && uncertainReplay) { setRetry(true); setError(t('The result could not be confirmed. Retry the same action; other controls are locked.', '未能確認結果，請重試同一操作，其他控制已鎖定。')); }
        else { pending.current = null; setRetry(false); setReview(true); setError(t('The purchase changed or this action is unavailable. Reload and review the current terms before continuing.', '購買已變更或此操作無法使用。請重新載入並檢查最新條款後再繼續。')); await reload(); }
      }
    } finally { submitting.current = false; setBusy(false); }
  }
  async function readReceipt() {
    if (!purchase.receiptId || submitting.current) return; submitting.current = true; setBusy(true); setError(''); setReceipt(null);
    try { setReceipt(await api<Receipt>(`/receipts/${purchase.receiptId}`)); }
    catch (e) { if (!isAborted(e)) { if (scopeError(e)) invalidate(); else setError(t('Could not load receipt. Please retry.', '未能載入收據，請重試。')); } }
    finally { submitting.current = false; setBusy(false); }
  }
  function assign(event: FormEvent) { event.preventDefault(); if (date) void mutate(`/purchases/${purchase.id}/expiry`, {expiresOn: date, version: purchase.version}, false); }
  const pendingAttempt = purchase.attempts.find(attempt => attempt.status === 'pending');
  const locked = busy || loading || retry || review;
  function attempt() { void mutate(`/purchases/${purchase.id}/payment-attempts`, {idempotencyKey: crypto.randomUUID()}); }
  const outcome = (id: string, value: 'pending' | 'failed' | 'succeeded') => void mutate(`/payment-attempts/${id}/mock-outcome`, {outcome: value});
  return <article className="panel"><h3>{purchase.offerName}</h3><p>{t('Purchase', '購買')} {purchase.id}{admin && <> · {t('Student', '學員')} {studentName ?? purchase.studentId}</>}</p><p>{purchase.credits} {t('credits', '堂')} · {money(purchase.priceMinor)}</p><p><strong>{purchase.status === 'awaiting_expiry' ? t('Awaiting administrator expiry', '等待管理員設定到期日') : purchase.status === 'paid' ? t('Paid (simulated)', '已付款（模擬）') : t('Awaiting simulated payment', '等待模擬付款')}</strong></p><p>{t('Expiry (inclusive, Hong Kong)', '到期日（包括當日，香港）')}: {purchase.expiresOn ?? (purchase.policy.expiryEnabled ? t('Not assigned yet', '尚未指定') : t('No expiry', '無到期日'))}</p><details><summary>{t('Frozen purchase policy', '已固定的購買政策')}</summary><p>{t('Offer version', '套票版本')} {purchase.offerVersion} · {t('Policy version', '政策版本')} {purchase.policyVersion}</p><PolicyTerms policy={purchase.policy} t={t}/></details>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    {admin && purchase.status === 'awaiting_expiry' && <form onSubmit={assign}><label htmlFor={expiryInputId}>{t('Expiry date (Hong Kong)', '到期日（香港）')}</label><input id={expiryInputId} type="date" min={hkDate()} required disabled={locked} value={date} onChange={e => setDate(e.target.value)}/><button disabled={locked || !date}>{t('Assign expiry', '指定到期日')}</button></form>}
    {!admin && purchase.status === 'awaiting_expiry' && <p>{t('An administrator must assign an expiry date before you can review the final terms and start mock payment.', '管理員須先指定到期日，然後你才可檢查最終條款並開始模擬付款。')}</p>}
    {!admin && purchase.status === 'pending_payment' && !pendingAttempt && <button className="section-gap" disabled={locked} onClick={attempt}>{purchase.attempts.length ? t('Retry mock payment', '重試模擬付款') : t('Start mock payment', '開始模擬付款')}</button>}
    {!!purchase.attempts.length && <section className="section-gap"><h4>{t('Mock payment attempts', '模擬付款嘗試')}</h4><ol>{purchase.attempts.map(payment => <li key={payment.id}><p>{payment.id} · {payment.status === 'pending' ? t('Pending', '處理中') : payment.status === 'failed' ? t('Failed', '失敗') : t('Succeeded (simulated)', '成功（模擬）')}</p>{!admin && payment.status === 'pending' && <div className="form-actions"><button disabled={locked} onClick={() => outcome(payment.id, 'pending')}>{t('Mark pending', '設為處理中')}</button><button className="outline" disabled={locked} onClick={() => outcome(payment.id, 'failed')}>{t('Mark failed', '設為失敗')}</button><button disabled={locked} onClick={() => outcome(payment.id, 'succeeded')}>{t('Mark succeeded', '設為成功')}</button></div>}{!admin && payment.status === 'succeeded' && <button className="outline" disabled={locked} onClick={() => outcome(payment.id, 'succeeded')}>{t('Replay successful outcome', '重播成功結果')}</button>}</li>)}</ol></section>}
    {retry && <button disabled={busy || loading} onClick={() => { const action = pending.current; if (action) void mutate(action.path, action.body); }}>{t('Retry same action', '重試同一操作')}</button>}
    {review && <div><button disabled={busy || loading} onClick={async () => { await reload(); }}>{t('Reload purchase', '重新載入購買')}</button><button disabled={busy || loading} onClick={() => {setReview(false); setError('');}}>{t('I reviewed the current terms', '我已檢查最新條款')}</button></div>}
    {purchase.receiptId && <button className="outline section-gap" disabled={locked} onClick={readReceipt}>{t('View simulated receipt', '查看模擬收據')}</button>}
    {receipt && <section className="section-gap" aria-label={t('Simulated receipt', '模擬收據')}><h4>{t('Simulated receipt — no real payment', '模擬收據 — 沒有真實付款')}</h4><p>{receipt.id} · {receipt.studentName} · {receipt.offerName}</p><p>{money(receipt.priceMinor)} · {receipt.credits} {t('credits', '堂')}</p><p>{t('Expiry (inclusive, Hong Kong)', '到期日（包括當日，香港）')}: {receipt.expiresOn ?? t('No expiry', '無到期日')}</p><p>{t('Purchase', '購買')} {receipt.purchaseId}</p></section>}
  </article>;
}
