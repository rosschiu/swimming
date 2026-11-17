import { useEffect, useRef, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError, type User } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Milestone = components['schemas']['StudentMilestone'];
type EventInput = components['schemas']['MilestoneEventInput'];

export default function StudentMilestones({ studentId, role, csrfToken, expired, t }: ProtectedProps & { studentId: string; role: User['role'] }) {
  const api = useProtectedRequest(expired);
  const base = `/students/${studentId}/milestones`;
  const [items, setItems] = useState<Milestone[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [editing, setEditing] = useState<Milestone | null>(null);
  const [state, setState] = useState<EventInput['state']>('achieved');
  const [reason, setReason] = useState('');
  const [review, setReview] = useState(false);
  const [retry, setRetry] = useState(false);
  const pending = useRef<EventInput | null>(null);
  const submitting = useRef(false);
  const staff = role === 'admin' || role === 'coach';
  const time = (value: string) => new Date(value).toLocaleString(undefined, {timeZone: 'Asia/Hong_Kong', hour12: false});
  async function load() {
    setLoading(true); setLoadError(false);
    try { const result = await api<{items: Milestone[]}>(base); setItems(result.items); }
    catch (e) { if (!isAborted(e)) { setItems([]); setLoadError(true); } }
    finally { setLoading(false); }
  }
  useEffect(() => { void load(); }, [api, studentId]);
  function edit(item: Milestone, next: EventInput['state']) { setEditing(item); setState(next); setReason(''); setReview(false); setError(''); setNotice(''); }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (!editing || submitting.current || review) return;
    if (!pending.current && ([...reason.trim()].length > 500 || (state === 'revoked' && !reason.trim()))) { setError(t('Enter up to 500 characters; a revocation reason is required.', '請輸入最多 500 字；撤銷時必須填寫原因。')); return; }
    pending.current ??= { requestId: crypto.randomUUID(), state, version: editing.current?.version ?? 0, skillVersion: editing.skill.version, reason: reason.trim() };
    submitting.current = true; setBusy(true); setError(''); setNotice('');
    try {
      await api(`${base}/${editing.skill.id}/events`, {method: 'POST', headers: {'X-CSRF-Token': csrfToken}, body: JSON.stringify(pending.current)});
      // Replay returns the original immutable event; only a fresh read establishes current status.
      pending.current = null; setRetry(false); setEditing(null); setReason(''); setNotice(t('Milestone recorded and shared.', '已記錄並分享技能進度。')); await load();
    } catch (e) {
      if (!isAborted(e)) {
        const uncertain = !(e instanceof APIError) || e.status >= 500;
        if (uncertain) { setRetry(true); setError(t('Recording could not be confirmed. Retry the same submission; your entries are locked to avoid duplicates.', '未能確認記錄結果。請重試同一次提交；輸入內容已鎖定以避免重複。')); }
        else {
          pending.current = null; setRetry(false);
          if (e instanceof APIError && e.status === 409) { setReview(true); setError(t('The milestone or skill definition changed. Your draft is preserved. Review the reloaded current state and definition before submitting again.', '技能進度或定義已更改。已保留草稿，請檢查重新載入的最新狀態及定義後再提交。')); await load(); }
          else setError(t('Could not record this milestone. Your entries are preserved; check your access and try again.', '未能記錄技能進度。已保留輸入內容，請檢查權限後再試。'));
        }
      }
    } finally { submitting.current = false; setBusy(false); }
  }
  const current = items.find(item => item.skill.id === editing?.skill.id);
  const allowed = current && (state === 'achieved' ? current.canAchieve : current.canRevoke);
  return <section className="section-gap" aria-labelledby="milestones-title">
    <div className="page-heading"><h2 id="milestones-title">{t('Skill milestones', '技能進度')}</h2><button className="outline" onClick={load} disabled={busy || loading}>{t('Reload milestones', '重新載入技能進度')}</button></div>
    <p className="muted">{t('Achievements and revocations are shared immediately with the student and linked families. Times show when recorded, in Hong Kong time. Swimming levels do not change automatically.', '達標及撤銷紀錄會即時分享予學員及已連結的家長。時間為記錄當刻的香港時間，游泳程度不會自動更改。')}</p>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    {loading ? <p role="status">{t('Loading milestones…', '載入技能進度中…')}</p> : loadError ? <div><p role="alert">{t('Could not load milestones. Your entries are preserved.', '未能載入技能進度，已保留輸入內容。')}</p><button onClick={load} disabled={busy}>{t('Retry loading milestones', '重試載入技能進度')}</button></div> : <div className="record-list">{items.map(item => <article className="panel" key={item.skill.id}><h3>{item.skill.name}</h3><p className="muted">{t('Current definition', '最新定義')} · {t('Version', '版本')} {item.skill.version} · {item.skill.active ? t('Active', '使用中') : t('Archived', '已封存')}</p><p style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{item.skill.description}</p><p><strong>{item.current?.state === 'achieved' ? t('Achieved', '已達標') : item.current?.state === 'revoked' ? t('Revoked', '已撤銷') : t('Not yet recorded', '尚未記錄')}</strong></p>{staff && <div className="form-actions">{item.canAchieve && <button disabled={busy || !!editing} onClick={() => edit(item, 'achieved')}>{t('Record achievement', '記錄達標')}</button>}{item.canRevoke && <button className="outline" disabled={busy || !!editing} onClick={() => edit(item, 'revoked')}>{t('Revoke achievement', '撤銷達標')}</button>}</div>}<details><summary>{t('Milestone history', '技能進度歷史')}</summary>{item.history.length ? <ol>{item.history.map(event => <li key={event.id}><p><strong>{event.state === 'achieved' ? t('Achieved', '已達標') : t('Revoked', '已撤銷')}</strong> · {event.actorName} · <time dateTime={event.recordedAt}>{time(event.recordedAt)}</time> · {t('Event version', '紀錄版本')} {event.version}</p><p>{t('Assessed definition', '評核時定義')}: {event.skillName} · {t('Version', '版本')} {event.skillVersion}</p><p style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{event.skillDescription}</p>{event.reason && <p style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{event.reason}</p>}</li>)}</ol> : <p>{t('No history yet.', '暫無歷史紀錄。')}</p>}</details></article>)}{!items.length && <p>{t('No skills available yet.', '暫無可用技能。')}</p>}</div>}
    {editing && <form onSubmit={save}><h3>{state === 'achieved' ? t('Record achievement', '記錄達標') : t('Revoke achievement', '撤銷達標')}: {editing.skill.name}</h3><p>{t('Reviewed skill definition', '已檢視技能定義')} · {t('Version', '版本')} {editing.skill.version}</p><p style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{editing.skill.description}</p><p>{state === 'achieved' ? t('This records an achievement against the reviewed definition and shares it immediately.', '此操作會按已檢視的定義記錄達標，並即時分享。') : t('This revokes the current achievement and preserves its history. It is shared immediately.', '此操作會撤銷目前達標狀態並保留歷史，且即時分享。')}</p><label htmlFor="milestone-reason">{state === 'achieved' ? t('Achievement comment', '達標評語') : t('Revocation reason', '撤銷原因')}</label><textarea id="milestone-reason" required={state === 'revoked'} disabled={busy || retry} value={reason} onChange={e => setReason(e.target.value)}/>{review && <div><p>{t('Compare your draft with the current milestone and definition above. Cancel if the intended action is already recorded.', '請比較草稿與上方最新技能進度及定義。如預期操作已記錄，請取消。')}</p><button type="button" disabled={busy || loading || loadError || !allowed} onClick={() => { if (current) { setEditing(current); setReview(false); setError(''); } }}>{t('Use current state and definition; keep my draft', '使用最新狀態及定義並保留草稿')}</button></div>}<div className="form-actions"><button disabled={busy || loading || loadError || review || (!retry && !allowed)}>{busy ? t('Sharing…', '分享中…') : retry ? t('Retry same submission', '重試同一次提交') : state === 'achieved' ? t('Share achievement', '分享達標紀錄') : t('Share revocation', '分享撤銷紀錄')}</button><button type="button" className="outline" disabled={busy || retry} onClick={() => { setEditing(null); setReview(false); setReason(''); setError(''); }}>{t('Cancel', '取消')}</button></div></form>}
  </section>;
}
