import { useEffect, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type AuditEvent = components['schemas']['AuditEvent'];

export default function AuditViewer({ expired, t }: ProtectedProps) {
  const api = useProtectedRequest(expired);
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [entityId, setEntityId] = useState('');
  const [appliedEntity, setAppliedEntity] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [more, setMore] = useState(false);
  async function load(entity: string, append = false) {
    setLoading(true); setError('');
    const query = new URLSearchParams({ limit: '25' });
    if (entity) query.set('entityId', entity);
    if (append && events.length) query.set('afterId', String(events.at(-1)!.id));
    try {
      const result = await api<{items: AuditEvent[]}>(`/audit?${query}`);
      setEvents(previous => append ? [...previous, ...result.items] : result.items);
      setAppliedEntity(entity); setMore(result.items.length === 25);
    } catch (e) { if (!isAborted(e)) setError(t('Could not load audit history. Please retry.', '未能載入操作紀錄，請重試。')); }
    finally { setLoading(false); }
  }
  useEffect(() => { void load(''); }, [api]);
  function filter(event: FormEvent) { event.preventDefault(); void load(entityId.trim()); }
  return <section><div className="page-heading"><div><p className="eyebrow">{t('ACCOUNTABILITY', '操作追蹤')}</p><h1>{t('Audit history', '操作紀錄')}</h1><p className="muted">{t('Review recorded changes and the account responsible.', '查看已記錄的變更及操作帳戶。')}</p></div></div><form onSubmit={filter} className="directory-tools"><div><label htmlFor="audit-entity">{t('Filter by record ID', '按紀錄編號篩選')}</label><input id="audit-entity" value={entityId} onChange={e => setEntityId(e.target.value)}/></div><button disabled={loading}>{t('Apply filter', '套用篩選')}</button><button type="button" className="outline" disabled={loading} onClick={() => { setEntityId(''); void load(''); }}>{t('Clear', '清除')}</button></form>
    {error && <p role="alert" className="error">{error}</p>}{loading && <p role="status">{t('Loading history…', '載入紀錄中…')}</p>}
    <div className="record-list">{events.map(event => <article key={event.id} className="panel audit-record"><h2>{event.action}</h2><dl><dt>{t('Time (Hong Kong)', '時間（香港）')}</dt><dd><time dateTime={event.createdAt}>{new Date(event.createdAt).toLocaleString(undefined, { timeZone: 'Asia/Hong_Kong' })}</time></dd><dt>{t('Actor ID', '操作帳戶編號')}</dt><dd>{event.actorId || t('System', '系統')}</dd><dt>{t('Record ID', '紀錄編號')}</dt><dd>{event.entityId}</dd></dl><details><summary>{t('Change details', '變更詳情')}</summary><pre>{JSON.stringify(event.details, null, 2)}</pre></details></article>)}</div>
    {!loading && !error && !events.length && <p className="panel">{t('No recorded changes match this filter.', '沒有符合篩選條件的操作紀錄。')}</p>}{more && <button className="outline section-gap" disabled={loading} onClick={() => load(appliedEntity, true)}>{t('Load more', '載入更多')}</button>}
  </section>;
}
