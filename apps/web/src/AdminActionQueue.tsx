import { useEffect, useState, type MouseEvent } from 'react';
import type { components } from './generated/api';
import { routePath, type Route } from './navigation';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Summary = components['schemas']['AdminActionQueue'];
type Props = Pick<ProtectedProps, 'expired' | 't'> & {
  follow: (event: MouseEvent<HTMLAnchorElement>, next: Route) => void;
};

const purchases: Route = { view: 'packages' };
const deliveries: Route = { view: 'deliveries', deliveryStatus: 'failed' };

export default function AdminActionQueue({ expired, t, follow }: Props) {
  const api = useProtectedRequest(expired);
  const [summary, setSummary] = useState<Summary | 'error' | null>(null);

  async function load() {
    setSummary(null);
    try {
      setSummary(await api<Summary>('/admin/action-queue'));
    } catch (error) {
      if (!isAborted(error)) setSummary('error');
    }
  }
  useEffect(() => { void load(); }, [api]);

  return <section className="panel section-gap" aria-labelledby="action-queue-title">
    <h2 id="action-queue-title">{t('Action queue', '待辦事項')}</h2>
    {summary === null ? <p role="status">{t('Loading action queue…', '正在載入待辦事項…')}</p>
      : summary === 'error' ? <>
        <p role="alert">{t('Could not load the action queue.', '未能載入待辦事項。')}</p>
        <button className="outline" onClick={() => void load()}>{t('Retry action queue', '重試載入待辦事項')}</button>
      </> : <>
        {summary.purchasesAwaitingExpiry === 0 && summary.failedEmailDeliveries === 0 &&
          <p>{t('No items need attention.', '目前沒有待處理事項。')}</p>}
        <ul className="link-list">
          <li><a aria-describedby="action-queue-purchases" href={routePath(purchases)} onClick={event => follow(event, purchases)}>{t('Purchases awaiting expiry', '待設定到期日的購買')}</a>
            <strong id="action-queue-purchases">{summary.purchasesAwaitingExpiry}</strong></li>
          <li><a aria-describedby="action-queue-deliveries" href={routePath(deliveries)} onClick={event => follow(event, deliveries)}>{t('Failed email deliveries', '電郵寄送失敗')}</a>
            <strong id="action-queue-deliveries">{summary.failedEmailDeliveries}</strong></li>
        </ul>
      </>}
  </section>;
}
