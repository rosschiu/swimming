import { useEffect, useState, type MouseEvent } from 'react';
import { routePath, type Route } from './navigation';
import type { components } from './generated/api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Notification = components['schemas']['Notification'];
type Preferences = components['schemas']['NotificationPreferences'];
type Props = ProtectedProps & { coach?: boolean; follow: (event: MouseEvent<HTMLAnchorElement>, route: Route) => void; onUnreadCount: (count: number) => void };

export default function Notifications({ csrfToken, expired, t, coach, follow, onUnreadCount }: Props) {
  const api = useProtectedRequest(expired);
  const [items, setItems] = useState<Notification[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [preferences, setPreferences] = useState<Preferences | null>(null);
  const [preferenceError, setPreferenceError] = useState('');
  const [saving, setSaving] = useState(false);
  const [reading, setReading] = useState<string | null>(null);
  async function load(append = false) {
    setLoading(true); setError('');
    const query = new URLSearchParams({ limit: '25' });
    if (append && cursor) query.set('cursor', cursor);
    try {
      const result = await api<{ items: Notification[]; nextCursor: string | null; unreadCount: number }>(`/notifications?${query}`);
      setItems(previous => append ? [...previous, ...result.items] : result.items);
      setCursor(result.nextCursor); onUnreadCount(result.unreadCount);
    } catch (e) { if (!isAborted(e)) setError(t('Could not load notifications. Previously loaded updates may be out of date. Please retry.', '未能載入通知。先前載入的更新可能已過時，請重試。')); }
    finally { setLoading(false); }
  }
  async function loadPreferences() {
    setPreferenceError('');
    try { setPreferences(await api<Preferences>('/notification-preferences')); }
    catch (e) { if (!isAborted(e)) setPreferenceError(t('Could not load email preference. Please retry.', '未能載入電郵設定，請重試。')); }
  }
  useEffect(() => { void load(); if (!coach) void loadPreferences(); }, [api, coach]);
  async function savePreference(enabled: boolean, field: 'emailEnabled' | 'serviceEmailEnabled' = 'emailEnabled') {
    setSaving(true); setPreferenceError('');
    try { setPreferences(await api<Preferences>('/notification-preferences', { method: 'PUT', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ [field]: enabled }) })); }
    catch (e) { if (!isAborted(e)) setPreferenceError(t('Could not save email preference. Please retry.', '未能儲存電郵設定，請重試。')); }
    finally { setSaving(false); }
  }
  async function markRead(item: Notification) {
    setReading(item.id); setError('');
    try {
      const updated = await api<Notification>(`/notifications/${item.id}/read`, { method: 'PUT', headers: { 'X-CSRF-Token': csrfToken } });
      setItems(previous => previous.map(entry => entry.id === item.id ? updated : entry));
      // Refresh the scoped count: other tabs may have marked updates read too.
      const result = await api<{ unreadCount: number }>('/notifications?limit=1'); onUnreadCount(result.unreadCount);
    } catch (e) { if (!isAborted(e)) setError(t('Could not refresh read status. Please reload and review.', '未能更新已讀狀態，請重新載入並查看。')); }
    finally { setReading(null); }
  }
  function description(kind: Notification['kind']) {
    switch (kind) {
      case 'lesson_change.requested': return t('A guardian requested a lesson change.', '有家長已提交調課申請。');
      case 'lesson_change.approved': return t('Your lesson change was approved.', '你的調課申請已獲批准。');
      case 'lesson_change.rejected': return t('Your lesson change was rejected.', '你的調課申請已被拒絕。');
      case 'booking.created': return t('A lesson was booked.', '已預約課堂。');
      case 'booking.cancelled': return t('A lesson was cancelled.', '課堂已取消。');
      case 'class.cancelled': return t('The school cancelled a lesson. The credit has been returned.', '學校已取消課堂，堂數已退回。');
      case 'progress.created': return t('A teaching update was shared.', '已分享教學更新。');
      case 'progress.updated': return t('A teaching update was corrected.', '教學更新已修正。');
      case 'milestone.achieved': return t('A milestone was achieved.', '已達成里程碑。');
      case 'milestone.revoked': return t('A milestone achievement was revoked.', '里程碑成就已撤銷。');
    }
  }
  return <section><div className="page-heading"><h1>{t('Notifications', '通知')}</h1><button className="outline" disabled={loading || reading !== null} onClick={() => load()}>{t('Refresh', '重新載入')}</button></div>
    {!coach && <section className="panel"><h2>{t('Email shared teaching updates', '以電郵接收已分享的教學更新')}</h2>{preferences ? <><label><input type="checkbox" checked={preferences.emailEnabled} disabled={saving} onChange={e => savePreference(e.target.checked)}/>{t('Email shared teaching updates', '以電郵接收已分享的教學更新')}</label><p>{t('Your login email:', '你的登入電郵：')} {preferences.emailAddress}</p><p className="muted">{t('Off by default. Changes apply to future teaching updates only. Invitation and password recovery emails are unaffected.', '預設關閉。更改只適用於日後的教學更新，不影響邀請及密碼重設電郵。')}</p><label><input type="checkbox" checked={preferences.serviceEmailEnabled} disabled={saving} onChange={e => savePreference(e.target.checked, 'serviceEmailEnabled')}/>{t('Email booking changes such as cancellations', '以電郵接收取消課堂等預約變更')}</label><p className="muted">{t('On by default. Emails contain only a sign-in link.', '預設開啟。電郵只包含登入連結。')}</p>{!preferences.emailDeliveryAvailable && <p>{t('Demo email delivery is currently unavailable for this login address. Enabling this preference alone will not send email until delivery is available.', '此登入電郵目前無法接收示範電郵。即使開啟此設定，也須待電郵服務可用後才會寄送。')}</p>}</> : !preferenceError && <p role="status">{t('Loading email preference…', '載入電郵設定中…')}</p>}{preferenceError && <><p role="alert" className="error">{preferenceError}</p><button disabled={saving} onClick={loadPreferences}>{t('Reload email preference', '重新載入電郵設定')}</button></>}</section>}
    {error && <p role="alert" className="error">{error}</p>}{loading && <p role="status">{t('Loading notifications…', '載入通知中…')}</p>}
    <p className="muted">{t('Times shown in Hong Kong time.', '時間以香港時間顯示。')}</p><div className="record-list">{items.map(item => {
      const isDecision = item.kind === 'lesson_change.approved' || item.kind === 'lesson_change.rejected';
      const isRequest = item.kind === 'lesson_change.requested';
      const isBooking = item.kind === 'booking.created' || item.kind === 'booking.cancelled' || item.kind === 'class.cancelled';
      // Coaches cannot open a family's booking page, so a cancellation takes them to their schedule.
      const cancelledNotice = item.kind === 'booking.cancelled' || item.kind === 'class.cancelled';
      const destination: Route = coach && cancelledNotice ? { view: 'schedule' } : { view: isRequest ? 'lesson-changes' : 'students', studentId: item.studentId,
        bookingId: isBooking ? item.sourceId : undefined, lessonChangeId: isRequest || isDecision ? item.sourceId : undefined };
      return <article className="panel" key={item.id}><h2>{item.studentName}</h2><p>{description(item.kind)}</p><p><time dateTime={item.createdAt}>{new Date(item.createdAt).toLocaleString(undefined, { timeZone: 'Asia/Hong_Kong' })}</time> · {item.readAt ? t('Read', '已讀') : t('Unread', '未讀')}</p><div className="form-actions"><a className="action-link" href={routePath(destination)} onClick={event => follow(event, destination)}>{isRequest ? t('View lesson change request', '查看調課申請') : isDecision ? t('View lesson change decision', '查看調課結果') : coach && cancelledNotice ? t('Open schedule', '開啟時間表') : isBooking ? t('View booking', '查看預約') : t('Open current student profile', '開啟學員最新資料')}</a>{!item.readAt && <button className="outline" disabled={reading !== null || loading} onClick={() => markRead(item)}>{t('Mark read', '標記已讀')}</button>}</div></article>;
    })}</div>
    {!loading && !error && !items.length && <p className="panel">{t('No notifications yet.', '暫無通知。')}</p>}{cursor && <button disabled={loading || reading !== null} onClick={() => load(true)}>{t('Load more', '載入更多')}</button>}
  </section>;
}
