import { useEffect, useRef, useState } from 'react';
import type { components } from './generated/api';
import type { Translate } from './useProtectedRequest';

type NoteMedia = components['schemas']['NoteMedia'];
type Kind = NoteMedia['kind'];
export type MediaDraft = { key: string; blob: Blob; kind: Kind; label: string; uploadedId?: string };
type Upload = (path: string, options: RequestInit) => Promise<NoteMedia>;

export const maxAttachments = 6;
const limits: Record<Kind, number> = { image: 15 << 20, audio: 25 << 20, video: 100 << 20 };
const kindOf = (type: string): Kind | null => type.startsWith('image/') ? 'image' : type.startsWith('video/') ? 'video' : type.startsWith('audio/') ? 'audio' : null;
const size = (bytes: number) => bytes >= 1 << 20 ? `${(bytes / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB`;

// Uploads drafts not yet on the server, recording each ID so a retry skips it. Returns the IDs in order.
export async function uploadDrafts(upload: Upload, studentId: string, csrfToken: string, drafts: MediaDraft[], saved: (key: string, id: string) => void) {
  const ids: string[] = [];
  for (const draft of drafts) {
    if (!draft.uploadedId) {
      const media = await upload(`/students/${encodeURIComponent(studentId)}/note-media`, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken, 'Content-Type': draft.blob.type }, body: draft.blob });
      draft.uploadedId = media.id;
      saved(draft.key, media.id);
    }
    ids.push(draft.uploadedId);
  }
  return ids;
}

// Photos, videos and voice recordings to send with a note.
export function MediaPicker({ id, drafts, onChange, disabled, t }: { id: string; drafts: MediaDraft[]; onChange: (drafts: MediaDraft[]) => void; disabled: boolean; t: Translate }) {
  const [error, setError] = useState('');
  const [recording, setRecording] = useState<{ recorder: MediaRecorder; started: number } | null>(null);
  const latest = useRef(drafts);
  latest.current = drafts;
  const canRecord = typeof MediaRecorder !== 'undefined' && !!navigator.mediaDevices?.getUserMedia;
  const kindLabel = (kind: Kind) => ({ image: t('Photo', '相片'), video: t('Video', '影片'), audio: t('Voice recording', '錄音') }[kind]);
  useEffect(() => () => { if (recording?.recorder.state === 'recording') recording.recorder.stop(); }, [recording]);
  function add(items: MediaDraft[]) {
    const room = maxAttachments - latest.current.length;
    if (items.length > room) setError(t(`A note can have at most ${maxAttachments} attachments.`, `每則紀錄最多可附 ${maxAttachments} 個附件。`));
    onChange([...latest.current, ...items.slice(0, Math.max(0, room))]);
  }
  function pick(files: FileList | null) {
    setError('');
    const items: MediaDraft[] = [];
    for (const file of Array.from(files ?? [])) {
      const kind = kindOf(file.type);
      if (!kind) { setError(t(`${file.name} is not a photo, video or recording.`, `${file.name} 不是相片、影片或錄音。`)); continue; }
      if (file.size > limits[kind]) { setError(t(`${file.name} is larger than ${size(limits[kind])}.`, `${file.name} 超過 ${size(limits[kind])}。`)); continue; }
      items.push({ key: crypto.randomUUID(), blob: file, kind, label: file.name });
    }
    add(items);
  }
  async function record() {
    setError('');
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const recorder = new MediaRecorder(stream);
      const chunks: Blob[] = [];
      const started = Date.now();
      recorder.ondataavailable = event => { if (event.data.size) chunks.push(event.data); };
      recorder.onstop = () => {
        stream.getTracks().forEach(track => track.stop());
        setRecording(null);
        const blob = new Blob(chunks, { type: recorder.mimeType || 'audio/webm' });
        if (blob.size) add([{ key: crypto.randomUUID(), blob, kind: 'audio', label: `${kindLabel('audio')} · ${Math.max(1, Math.round((Date.now() - started) / 1000))}s` }]);
      };
      recorder.start();
      setRecording({ recorder, started });
    } catch { setError(t('Could not use the microphone. Check that this site may use it.', '未能使用咪高峰，請檢查網站權限。')); }
  }
  const full = drafts.length >= maxAttachments;
  return <div className="note-media-picker">
    <label htmlFor={id}>{t('Add photos or videos', '加入相片或影片')}</label>
    <input id={id} type="file" accept="image/jpeg,image/png,image/webp,video/mp4,video/quicktime,video/webm,image/*,video/*" multiple disabled={disabled || full} onChange={event => { pick(event.target.files); event.target.value = ''; }}/>
    {canRecord && (recording
      ? <button type="button" onClick={() => recording.recorder.stop()}>{t('Stop recording', '停止錄音')}</button>
      : <button type="button" className="outline" disabled={disabled || full} onClick={() => void record()}>{t('Record voice', '錄音')}</button>)}
    {recording && <p role="status">{t('Recording…', '錄音中…')}</p>}
    {error && <p role="alert" className="error">{error}</p>}
    {drafts.length > 0 && <ul className="attachment-list" aria-label={t('Attachments', '附件')}>{drafts.map(draft => <li key={draft.key}>
      <span>{kindLabel(draft.kind)} · {draft.label} · {size(draft.blob.size)}</span>
      <button type="button" className="text-button" disabled={disabled} aria-label={t(`Remove ${draft.label}`, `移除 ${draft.label}`)} onClick={() => onChange(latest.current.filter(item => item.key !== draft.key))}>{t('Remove', '移除')}</button>
    </li>)}</ul>}
  </div>;
}

// Attachments as families see them: photos inline, recordings and videos with playback controls.
export function NoteMediaList({ media, t }: { media: NoteMedia[]; t: Translate }) {
  return <div className="note-media">{media.map(item => item.url && (item.kind === 'image'
    ? <a key={item.id} href={item.url} target="_blank" rel="noreferrer"><img src={item.url} alt={t('Photo from the lesson', '課堂相片')} loading="lazy"/></a>
    : item.kind === 'audio'
      ? <audio key={item.id} controls preload="metadata" src={item.url} aria-label={t('Voice recording', '錄音')}/>
      : <video key={item.id} controls playsInline preload="metadata" src={item.url} aria-label={t('Video from the lesson', '課堂影片')}/>))}</div>;
}
