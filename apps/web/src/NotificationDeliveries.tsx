import { useEffect, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { deliveryStatuses, type DeliveryStatus } from './navigation';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Delivery = components['schemas']['NotificationDelivery'];
export default function NotificationDeliveries({ csrfToken, expired, t, status = '', onStatusChange }: ProtectedProps & { status?: DeliveryStatus | ''; onStatusChange?: (status: DeliveryStatus | '') => void }) {
  const api = useProtectedRequest(expired);
  const [items, setItems] = useState<Delivery[]>([]);
  const [localStatus, setLocalStatus] = useState<DeliveryStatus | ''>(status);
  const currentStatus = onStatusChange ? status : localStatus;
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [selected, setSelected] = useState<Delivery | null>(null);
  const [reason, setReason] = useState('');
  const [stale, setStale] = useState(false);
  const [busy, setBusy] = useState(false);
  async function load(filter: string, append = false) {
    setLoading(true); setError('');
    const query = new URLSearchParams({ limit: '25' });
    if (filter) query.set('status', filter);
    if (append && cursor) query.set('cursor', cursor);
    try {
      const result = await api<components['schemas']['NotificationDeliveryList']>(`/notification-deliveries?${query}`);
      setItems(previous => append ? [...previous, ...result.items] : result.items); setCursor(result.nextCursor);
      if (!append && filter === currentStatus && selected) {
        const current = result.items.find(item => item.id === selected.id);
        if (current) { setSelected(current); setStale(false); }
        else { setStale(true); setError(t('The selected delivery is not in this refreshed page. Cancel and select a current failed delivery to review.', '所選寄送紀錄不在此最新頁面。請取消並選擇目前失敗的紀錄以查看。')); }
      }
    } catch (e) { if (!isAborted(e)) setError(t('Could not load deliveries. Previously loaded statuses may be out of date. Please retry.', '未能載入寄送紀錄。先前載入的狀態可能已過時，請重試。')); }
    finally { setLoading(false); }
  }
  useEffect(() => { void load(currentStatus); }, [api, currentStatus]);
  async function retry(event: FormEvent) {
    event.preventDefault(); if (!selected || stale || !reason.trim()) return;
    if ([...reason.trim()].length > 500) { setError(t('Enter a retry reason of 1–500 characters.', '請輸入 1 至 500 字的重試原因。')); return; }
    setBusy(true); setError(''); setNotice('');
    try {
      const updated = await api<Delivery>(`/notification-deliveries/${selected.id}/retry`, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ version: selected.version, reason: reason.trim() }) });
      setItems(previous => previous.map(item => item.id === updated.id ? updated : item).filter(item => !currentStatus || item.status === currentStatus)); setSelected(null); setReason('');
      setNotice(updated.status === 'pending' || updated.status === 'retry' ? t('Retry queued. This does not confirm inbox delivery.', '已排入重試佇列，並不代表已送達收件匣。') : t('Delivery status updated. Review its current status below.', '寄送狀態已更新，請查看以下最新狀態。'));
    } catch (e) {
      if (!isAborted(e)) { if (e instanceof APIError && e.status === 409) { setStale(true); setError(t('This delivery changed. Your reason is preserved. Reload deliveries and review the current status before retrying.', '此寄送紀錄已變更，已保留原因。請重新載入並查看最新狀態後再重試。')); } else setError(t('Retry failed. Your reason is preserved; please review and try again.', '重試失敗，已保留原因。請查看後再試。')); }
    } finally { setBusy(false); }
  }
  const label = (value: string) => ({ pending: t('Pending', '待寄送'), leased: t('Sending', '寄送中'), sent: t('SMTP accepted', 'SMTP 已接受'), retry: t('Awaiting retry', '待重試'), failed: t('Failed', '失敗'), suppressed: t('Suppressed', '已停止') })[value] || value;
  return <section><div className="page-heading"><h1>{t('Notification deliveries', '通知寄送')}</h1><button className="outline" disabled={loading || busy} onClick={() => load(currentStatus)}>{t('Reload deliveries', '重新載入寄送紀錄')}</button></div><p>{t('Times shown in Hong Kong time. SMTP acceptance does not confirm inbox delivery. Recovery after a crash may send a duplicate email.', '時間以香港時間顯示。SMTP 接受不代表已送達收件匣。故障復原後可能重複寄送電郵。')}</p><label htmlFor="delivery-status">{t('Delivery status', '寄送狀態')}</label><select id="delivery-status" value={currentStatus} disabled={loading || busy} onChange={e => { const value = deliveryStatuses.find(option => option === e.target.value) ?? ''; if (onStatusChange) onStatusChange(value); else setLocalStatus(value); setItems([]); setCursor(null); setSelected(null); setReason(''); setStale(false); setNotice(''); }}><option value="">{t('All statuses', '所有狀態')}</option>{deliveryStatuses.map(value => <option key={value} value={value}>{label(value)}</option>)}</select>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}{loading && <p role="status">{t('Loading deliveries…', '載入寄送紀錄中…')}</p>}
    {selected && <form className="panel" onSubmit={retry}><h2>{t('Review retry', '查看重試')}</h2><p>{selected.recipientName} · {label(selected.status)} · {t('Version', '版本')} {selected.version}</p><label htmlFor="retry-reason">{t('Reason for retry', '重試原因')}</label><textarea id="retry-reason" value={reason} disabled={busy} required onChange={e => setReason(e.target.value)}/><div className="form-actions"><button disabled={busy || loading || stale || selected.status !== 'failed' || !reason.trim()}>{t('Retry failed delivery', '重試失敗寄送')}</button><button type="button" className="outline" disabled={busy} onClick={() => { setSelected(null); setReason(''); setStale(false); }}>{t('Cancel', '取消')}</button></div></form>}
    <div className="record-list">{items.map(item => <article className="panel" key={item.id}><h2>{item.recipientName}</h2><p>{label(item.status)}</p><dl><dt>{t('Lifetime attempts', '累計嘗試次數')}</dt><dd>{item.attempts}</dd><dt>{t('Current cycle attempts', '本輪嘗試次數')}</dt><dd>{item.cycleAttempts}</dd><dt>{t('Created', '建立時間')}</dt><dd>{new Date(item.createdAt).toLocaleString(undefined, { timeZone: 'Asia/Hong_Kong' })}</dd>{item.nextAttemptAt && <><dt>{t('Next attempt', '下次嘗試')}</dt><dd>{new Date(item.nextAttemptAt).toLocaleString(undefined, { timeZone: 'Asia/Hong_Kong' })}</dd></>}{item.completedAt && <><dt>{t('Completed', '完成時間')}</dt><dd>{new Date(item.completedAt).toLocaleString(undefined, { timeZone: 'Asia/Hong_Kong' })}</dd></>}{item.lastFailure && <><dt>{t('Last failure category', '最近失敗類別')}</dt><dd>{item.lastFailure}</dd></>}</dl>{item.status === 'failed' && <button disabled={busy || loading} onClick={() => { setSelected(item); setReason(''); setStale(false); setNotice(''); }}>{t('Review retry', '查看重試')}</button>}</article>)}</div>
    {!loading && !error && !items.length && <p className="panel">{t('No deliveries match this filter.', '沒有符合篩選條件的寄送紀錄。')}</p>}{cursor && <button disabled={loading || busy} onClick={() => load(currentStatus, true)}>{t('Load more', '載入更多')}</button>}
  </section>;
}
