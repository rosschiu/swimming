import { useEffect, useRef, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Skill = components['schemas']['Skill'];
type SkillInput = components['schemas']['SkillInput'];

export default function SkillCatalogue({ csrfToken, expired, t }: ProtectedProps) {
  const api = useProtectedRequest(expired);
  const [items, setItems] = useState<Skill[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Skill | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [active, setActive] = useState(true);
  const [reason, setReason] = useState('');
  const [review, setReview] = useState(false);
  const [retry, setRetry] = useState(false);
  const pending = useRef<SkillInput | null>(null);
  const submitting = useRef(false);
  async function load() {
    setLoading(true); setLoadError(false);
    try { const result = await api<{items: Skill[]}>('/skills'); setItems(result.items); }
    catch (e) { if (!isAborted(e)) { setItems([]); setLoadError(true); } }
    finally { setLoading(false); }
  }
  useEffect(() => { void load(); }, [api]);
  function edit(skill: Skill | null) {
    setEditing(skill); setName(skill?.name ?? ''); setDescription(skill?.description ?? ''); setActive(skill?.active ?? true); setReason(''); setReview(false); setError(''); setNotice(''); setOpen(true);
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (submitting.current || review) return;
    if (!pending.current && (!name.trim() || [...name.trim()].length > 100 || [...description.trim()].length > 1000 || (editing && (!reason.trim() || [...reason.trim()].length > 500)))) {
      setError(t('Enter a name of 1–100 characters, a description up to 1000 characters and a change reason of 1–500 characters when editing.', '請輸入 1 至 100 字的名稱、最多 1000 字的描述，編輯時須填寫 1 至 500 字的原因。')); return;
    }
    if (!editing) pending.current ??= { requestId: crypto.randomUUID(), name: name.trim(), description: description.trim() };
    submitting.current = true; setBusy(true); setError(''); setNotice('');
    try {
      await api<Skill>(`/skills${editing ? `/${editing.id}` : ''}`, { method: editing ? 'PATCH' : 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(editing ? { name: name.trim(), description: description.trim(), active, version: editing.version, reason: reason.trim() } : pending.current) });
      pending.current = null; setRetry(false); setOpen(false); setNotice(t('Skill saved.', '已儲存技能。')); await load();
    } catch (e) {
      if (!isAborted(e)) {
        const uncertain = !(e instanceof APIError) || e.status >= 500;
        if (editing && (uncertain || e instanceof APIError && e.status === 409)) {
          setReview(true); setError(t('The skill changed or the result is uncertain. Your draft is preserved. Reload and review the current definition before saving.', '技能已更改或未能確認結果。已保留草稿，請重新載入並檢查最新定義後再儲存。')); await load();
        } else {
          if (!uncertain) pending.current = null;
          setRetry(uncertain); setError(uncertain ? t('Saving could not be confirmed. Retry the same submission; your entries are locked to avoid duplicates.', '未能確認儲存結果。請重試同一次提交；輸入內容已鎖定以避免重複。') : t('Could not save this skill. Your entries are preserved; check the fields and your access.', '未能儲存技能。已保留輸入內容，請檢查欄位及權限。'));
        }
      }
    } finally { submitting.current = false; setBusy(false); }
  }
  const current = items.find(item => item.id === editing?.id);
  return <section className="section-gap" aria-labelledby="skills-title">
    <div className="page-heading"><h2 id="skills-title">{t('Skills', '技能')}</h2><button className="outline" disabled={busy || loading} onClick={load}>{t('Reload skills', '重新載入技能')}</button><button disabled={busy || open || loading || loadError} onClick={() => edit(null)}>{t('Add skill', '新增技能')}</button></div>
    <p className="muted">{t('Archiving prevents new achievements and preserves all recorded history. Skills do not automatically change swimming levels.', '封存會停止新增達標紀錄，並保留所有歷史紀錄。技能不會自動更改游泳程度。')}</p>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    {loading ? <p role="status">{t('Loading skills…', '載入技能中…')}</p> : loadError ? <div><p role="alert">{t('Could not load skills. Your entries are preserved.', '未能載入技能，已保留輸入內容。')}</p><button onClick={load} disabled={busy}>{t('Retry loading skills', '重試載入技能')}</button></div> : <div className="record-list">{items.map(skill => <article className="panel" key={skill.id}><h3>{skill.name}</h3><p style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{skill.description}</p><p className="muted">{skill.active ? t('Active', '使用中') : t('Archived', '已封存')} · {t('Version', '版本')} {skill.version}</p><button className="outline" disabled={busy || open} aria-label={`${t('Edit skill', '編輯技能')} ${skill.name}`} onClick={() => edit(skill)}>{t('Edit', '編輯')}</button></article>)}{!items.length && <p>{t('No skills yet.', '暫無技能。')}</p>}</div>}
    {open && <form onSubmit={save}><h3>{editing ? t('Edit skill', '編輯技能') : t('New skill', '新增技能')}</h3><label htmlFor="skill-name">{t('Skill name', '技能名稱')}</label><input id="skill-name" required disabled={busy || retry} value={name} onChange={e => setName(e.target.value)}/><label htmlFor="skill-description">{t('Skill description', '技能描述')}</label><textarea id="skill-description" disabled={busy || retry} value={description} onChange={e => setDescription(e.target.value)}/>{editing && <><label className="checkbox-label"><input type="checkbox" checked={active} disabled={busy} onChange={e => setActive(e.target.checked)}/>{t('Available for new achievements', '可供新增達標紀錄')}</label><label htmlFor="skill-reason">{t('Change reason', '更改原因')}</label><textarea id="skill-reason" required disabled={busy} value={reason} onChange={e => setReason(e.target.value)}/></>}{review && <div><p>{t('Compare your draft with the current definition above. Cancel if your change is already saved.', '請比較草稿與上方最新定義。如更改已儲存，請取消。')}</p><button type="button" disabled={busy || loading || loadError || !current} onClick={() => { if (current) { setEditing(current); setReview(false); setError(''); } }}>{t('Use current version; keep my draft', '使用最新版本並保留草稿')}</button></div>}<div className="form-actions"><button disabled={busy || loading || loadError || review}>{busy ? t('Saving…', '儲存中…') : retry ? t('Retry same submission', '重試同一次提交') : t('Save skill', '儲存技能')}</button><button type="button" className="outline" disabled={busy || retry} onClick={() => { setOpen(false); setReview(false); setError(''); }}>{t('Cancel', '取消')}</button></div></form>}
  </section>;
}
