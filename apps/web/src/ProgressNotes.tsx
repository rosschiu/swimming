import { useEffect, useRef, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError, type User } from './api';
import { MediaPicker, NoteMediaList, uploadDrafts, type MediaDraft } from './NoteMedia';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type ProgressNote = components['schemas']['ProgressNote'];
type ProgressNoteInput = components['schemas']['ProgressNoteInput'];
type ProgressNoteUpdate = components['schemas']['ProgressNoteUpdate'];

export default function ProgressNotes({ studentId, role, csrfToken, expired, t }: ProtectedProps & { studentId: string; role: User['role'] }) {
  const api = useProtectedRequest(expired);
  const base = `/students/${studentId}/progress-notes`;
  const [notes, setNotes] = useState<ProgressNote[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [body, setBody] = useState('');
  const [drafts, setDrafts] = useState<MediaDraft[]>([]);
  const pending = useRef<ProgressNoteInput | null>(null);
  const submitting = useRef(false);
  const [retryCreate, setRetryCreate] = useState(false);
  const [editing, setEditing] = useState<ProgressNote | null>(null);
  const [correction, setCorrection] = useState('');
  const [reason, setReason] = useState('');
  const [needsReview, setNeedsReview] = useState(false);
  const canCreate = role === 'admin' || role === 'coach';
  const lessonTime = (value: string) => new Date(value).toLocaleString(undefined, { timeZone: 'Asia/Hong_Kong', hour12: false, dateStyle: 'medium', timeStyle: 'short' });
  const time = (value: string) => new Date(value).toLocaleString(undefined, { timeZone: 'Asia/Hong_Kong', hour12: false });
  async function load() {
    setLoading(true); setLoadError(false);
    try { const result = await api<{ items: ProgressNote[] }>(base); setNotes(result.items); return true; }
    catch (e) { if (!isAborted(e)) { setLoadError(true); setNotes([]); } return false; }
    finally { setLoading(false); }
  }
  useEffect(() => { void load(); }, [api, studentId]);
  async function create(event: FormEvent) {
    event.preventDefault();
    if (submitting.current) return;
    if (!pending.current && (!body.trim() && !drafts.length || [...body.trim()].length > 4000)) { setError(t('Enter a note of up to 4000 characters, or add a photo, video or recording.', '請輸入最多 4000 字的紀錄，或加入相片、影片或錄音。')); return; }
    submitting.current = true; setBusy(true); setError(''); setNotice('');
    if (!pending.current) {
      try {
        const mediaIds = await uploadDrafts(api, studentId, csrfToken, drafts, (key, uploadedId) => setDrafts(items => items.map(item => item.key === key ? { ...item, uploadedId } : item)));
        pending.current = { requestId: crypto.randomUUID(), body: body.trim(), ...(mediaIds.length ? { mediaIds } : {}) };
      } catch (e) {
        if (!isAborted(e)) setError(e instanceof APIError && e.status < 500 ? e.message : t('An attachment could not be uploaded. Try again.', '未能上載附件，請再試。'));
        submitting.current = false; setBusy(false);
        return;
      }
    }
    try {
      const note = await api<ProgressNote>(base, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(pending.current) });
      setNotes(items => [note, ...items.filter(item => item.id !== note.id)].sort((a, b) => b.createdAt.localeCompare(a.createdAt)));
      pending.current = null; setRetryCreate(false); setBody(''); setDrafts([]); setNotice(t('Progress note shared.', '已分享進度紀錄。'));
    } catch (e) {
      if (!isAborted(e)) {
        const uncertain = !(e instanceof APIError) || e.status >= 500 || e.status === 409;
        if (!uncertain) pending.current = null;
        setRetryCreate(uncertain);
        setError(uncertain ? t('Sharing could not be confirmed. Your text is preserved. Retry the same submission to avoid duplicates.', '未能確認是否已分享。已保留內容，請重試同一次提交以避免重複。') : t('Could not share this note. Your text is preserved; check your access and try again.', '未能分享紀錄。已保留內容，請檢查權限後再試。'));
      }
    } finally { submitting.current = false; setBusy(false); }
  }
  async function correct(event: FormEvent) {
    event.preventDefault();
    if (!editing || submitting.current || needsReview) return;
    if (!correction.trim() && !editing.media?.length || [...correction.trim()].length > 4000 || !reason.trim() || [...reason.trim()].length > 500) { setError(t('Enter a note of 1–4000 characters and a correction reason of 1–500 characters.', '請輸入 1 至 4000 字的紀錄及 1 至 500 字的更正原因。')); return; }
    submitting.current = true; setBusy(true); setError(''); setNotice('');
    const input: ProgressNoteUpdate = { body: correction.trim(), version: editing.version, reason: reason.trim() };
    try {
      const note = await api<ProgressNote>(`${base}/${editing.id}`, { method: 'PATCH', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(input) });
      setNotes(items => items.map(item => item.id === note.id ? note : item)); setEditing(null); setCorrection(''); setReason(''); setNotice(t('Correction saved and shared.', '已儲存並分享更正。'));
    } catch (e) {
      if (!isAborted(e)) {
        if (!(e instanceof APIError) || e.status >= 500 || e.status === 409) {
          setNeedsReview(true);
          setError(e instanceof APIError && e.status === 409 ? t('This note changed. Your correction is preserved. Review the reloaded current note before using its version.', '此紀錄已更改。已保留你的更正，請檢查重新載入的最新紀錄後再使用該版本。') : t('The correction result is uncertain. Your text is preserved. Reload and review the current note before making another correction.', '未能確認更正結果。已保留內容，請重新載入並檢查最新紀錄後再更正。'));
          await load();
        } else setError(t('Could not correct this note. Your entries are preserved; check your access and try again.', '未能更正紀錄。已保留輸入內容，請檢查權限後再試。'));
      }
    } finally { submitting.current = false; setBusy(false); }
  }
  const current = notes.find(note => note.id === editing?.id);
  return <section className="section-gap" aria-labelledby="progress-title">
    <div className="page-heading"><h2 id="progress-title">{t('Progress notes', '進度紀錄')}</h2><button className="outline" onClick={load} disabled={loading || busy}>{t('Reload notes', '重新載入紀錄')}</button></div>
    <p className="muted">{t('Notes and corrections are shared immediately with linked families and the student. Times are Hong Kong time.', '紀錄及更正會即時分享予已連結的家長及學員。時間以香港時間顯示。')}</p>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    {loading ? <p role="status">{t('Loading progress notes…', '載入進度紀錄中…')}</p> : loadError ? <div><p role="alert">{t('Could not load progress notes. Your entries are preserved.', '未能載入進度紀錄，已保留輸入內容。')}</p><button onClick={load} disabled={busy}>{t('Retry loading notes', '重試載入紀錄')}</button></div> : notes.length ? <ul className="link-list">{notes.map(note => <li key={note.id}><article>{note.lesson && <p className="muted">{t('Lesson', '課堂')}: {note.lesson.name} · <time dateTime={note.lesson.startsAt}>{lessonTime(note.lesson.startsAt)}</time></p>}{note.body && <p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{note.body}</p>}{note.media?.length ? <NoteMediaList media={note.media} t={t}/> : null}<p className="muted">{t('Recorded by', '記錄者')} {note.authorName} · <time dateTime={note.createdAt}>{time(note.createdAt)}</time></p>{note.version > 1 && <p className="muted">{t('Last corrected by', '最後更正者')} {note.updatedByName} · <time dateTime={note.updatedAt}>{time(note.updatedAt)}</time> · {t('Version', '版本')} {note.version}</p>}{note.editable && <button className="outline" disabled={busy || !!editing} onClick={() => { setEditing(note); setCorrection(note.body); setReason(''); setNeedsReview(false); setError(''); setNotice(''); }}>{t('Correct note', '更正紀錄')}</button>}</article></li>)}</ul> : <p>{t('No progress notes yet.', '暫無進度紀錄。')}</p>}
    {editing && <form onSubmit={correct}><h3>{t('Correct progress note', '更正進度紀錄')}</h3><label htmlFor="progress-correction">{t('Corrected note', '更正內容')}</label><textarea id="progress-correction" value={correction} onChange={event => setCorrection(event.target.value)} required={!editing.media?.length} disabled={busy}/><label htmlFor="progress-reason">{t('Correction reason', '更正原因')}</label><textarea id="progress-reason" value={reason} onChange={event => setReason(event.target.value)} required disabled={busy}/>{needsReview && <div><p>{t('Compare your draft with the current note above. If it already contains your correction, cancel. Otherwise, use the current version before saving.', '請比較草稿與上方最新紀錄。如已包含你的更正，請取消；否則請先使用最新版本再儲存。')}</p><button type="button" disabled={busy || loading || loadError || !current?.editable} onClick={() => { if (current) { setEditing(current); setNeedsReview(false); setError(''); } }}>{t('Use current version; keep my draft', '使用最新版本並保留草稿')}</button></div>}<div className="form-actions"><button disabled={busy || loading || loadError || needsReview || !current?.editable}>{busy ? t('Saving…', '儲存中…') : t('Share correction', '分享更正')}</button><button type="button" className="outline" disabled={busy} onClick={() => { setEditing(null); setCorrection(''); setReason(''); setNeedsReview(false); setError(''); }}>{t('Cancel correction', '取消更正')}</button></div></form>}
    {canCreate && <form onSubmit={create}><h3>{t('Add progress note', '新增進度紀錄')}</h3><label htmlFor="progress-body">{t('Progress note', '進度紀錄內容')}</label><textarea id="progress-body" value={body} onChange={event => setBody(event.target.value)} disabled={busy || retryCreate}/><MediaPicker id="progress-media" drafts={drafts} onChange={setDrafts} disabled={busy || retryCreate} t={t}/><button disabled={busy || loading || loadError}>{busy ? t('Sharing…', '分享中…') : retryCreate ? t('Retry same submission', '重試同一次提交') : t('Share progress note', '分享進度紀錄')}</button></form>}
  </section>;
}
