import { useEffect, useState } from 'react';
import { APIError } from './api';
import type { components } from './generated/api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type RosterEntry = components['schemas']['RosterEntry'];

export default function ClassRoster({ classId, expired, t }: Pick<ProtectedProps, 'expired' | 't'> & { classId: string }) {
  const api = useProtectedRequest(expired);
  const [result, setResult] = useState<RosterEntry[] | 'unavailable' | 'error' | null>(null);
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    void api<{ classId: string; items: RosterEntry[] }>(`/classes/${encodeURIComponent(classId)}/roster`)
      .then(({ items }) => setResult(items))
      .catch(error => {
        if (!isAborted(error)) setResult(error instanceof APIError && (error.status === 403 || error.status === 404) ? 'unavailable' : 'error');
      });
  }, [api, classId, revision]);

  return <section className="panel" aria-label={t('Class roster', '課堂名單')}>
    <div className="page-heading">
      <h3>{t('Class roster', '課堂名單')}</h3>
      <button className="outline" disabled={result === null} onClick={() => { setResult(null); setRevision(previous => previous + 1); }}>
        {t('Reload roster', '重新載入名單')}
      </button>
    </div>
    {result === null ? <p role="status">{t('Loading class roster…', '正在載入課堂名單…')}</p>
      : result === 'unavailable' ? <p role="alert">{t('This roster is unavailable for your current access.', '目前權限無法存取此課堂名單。')}</p>
      : result === 'error' ? <p role="alert">{t('Could not load the roster. Reload to retry.', '未能載入名單，請重新載入。')}</p>
      : result.length === 0 ? <p role="status">{t('No reserved places for this class.', '此課堂尚無預約。')}</p>
      : <ul className="link-list">{result.map(entry => <li key={entry.bookingId}>
        <div><strong>{entry.studentName}</strong><span className="muted">{entry.level}</span></div>
        <span className="tag">{t('Reserved', '已預約')}</span>
      </li>)}</ul>}
  </section>;
}
