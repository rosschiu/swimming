import { useId, useRef, useState } from 'react';
import { APIError } from './api';
import type { components } from './generated/api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type ClassSession = components['schemas']['ClassSession'];
type Substitution = components['schemas']['ClassSubstitution'];
type Coach = { id: string; name: string; active: boolean };

// Administrators hand one upcoming lesson to another coach; families and both coaches are told in-app.
export default function SubstituteCoach({ lesson, coaches, csrfToken, expired, t, onSubstituted, onClose }: ProtectedProps & {
  lesson: ClassSession; coaches: Coach[]; onSubstituted: (lesson: ClassSession) => void; onClose: () => void;
}) {
  const api = useProtectedRequest(expired);
  const id = useId();
  const [coachId, setCoachId] = useState('');
  const [reason, setReason] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const intent = useRef<{ fingerprint: string; key: string } | null>(null);
  const choices = coaches.filter(coach => coach.active && coach.id !== lesson.coachId);

  async function submit() {
    if (!coachId || !reason.trim() || saving) return;
    const body = { coachId, reason: reason.trim(), version: lesson.version };
    const fingerprint = JSON.stringify(body);
    if (intent.current?.fingerprint !== fingerprint) intent.current = { fingerprint, key: crypto.randomUUID() };
    setSaving(true); setError('');
    try {
      const result = await api<Substitution>(`/classes/${encodeURIComponent(lesson.id)}/substitution`, { method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ ...body, idempotencyKey: intent.current.key }) });
      onSubstituted(result.lesson);
    } catch (cause) {
      if (isAborted(cause)) return;
      const code = cause instanceof APIError ? cause.code : undefined;
      setError(code === 'scheduling_conflict' ? t('That coach has an overlapping lesson or not enough travel time. Choose another coach.', '該教練有重疊課堂或交通時間不足，請選擇其他教練。')
        : code === 'version_conflict' ? t('The lesson changed. Reload the schedule and try again.', '課堂已變更，請重新載入時間表後再試。')
        : code === 'class_started' ? t('This lesson has started and can no longer be reassigned.', '課堂已開始，不能再更換教練。')
        : t('Could not change the coach. Your entries are preserved; try again.', '未能更換教練，已保留輸入內容，請再試。'));
    } finally { setSaving(false); }
  }

  return <div className="panel cancel-review" role="group" aria-label={t('Substitute coach', '代課教練')}>
    <p>{t('Bookings, times and credits stay the same. The new coach gets the roster and any pending change requests; families and both coaches are told in the app.', '預約、時間及堂數維持不變。新教練會接手名單及待處理的調課申請；系統會通知家庭及兩位教練。')}</p>
    <label htmlFor={`${id}-coach`}>{t('New coach', '新教練')}</label>
    <select id={`${id}-coach`} value={coachId} disabled={saving} onChange={event => setCoachId(event.target.value)}>
      <option value="">{t('Choose a coach', '選擇教練')}</option>
      {choices.map(coach => <option key={coach.id} value={coach.id}>{coach.name}</option>)}
    </select>
    <label htmlFor={`${id}-reason`}>{t('Reason shown in the audit history', '記錄於操作紀錄的原因')}</label>
    <input id={`${id}-reason`} maxLength={500} value={reason} disabled={saving} onChange={event => setReason(event.target.value)}/>
    {error && <p role="alert" className="error">{error}</p>}
    <div className="form-actions">
      <button disabled={!coachId || !reason.trim() || saving} onClick={() => void submit()}>{saving ? t('Saving…', '儲存中…') : t('Confirm substitute', '確認代課')}</button>
      <button className="outline" disabled={saving} onClick={onClose}>{t('Cancel', '取消')}</button>
    </div>
  </div>;
}
