import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Settings = components['schemas']['LessonSettings'];
type Field = Exclude<keyof Settings, 'version' | 'cancellationEmail'>;
type Draft = Record<Field, string>;

function draftFrom(settings: Settings): Draft {
  return {
    parentChangeCutoffHours: String(settings.parentChangeCutoffHours),
    attendanceOpenMinutes: String(settings.attendanceOpenMinutes),
    correctionWindowDays: String(settings.correctionWindowDays),
  };
}

export default function LessonSettings({ csrfToken, expired, t }: ProtectedProps) {
  const api = useProtectedRequest(expired);
  const id = useId();
  const [settings, setSettings] = useState<Settings | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [email, setEmail] = useState(true);
  const [busy, setBusy] = useState(true);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState<'load' | 'save' | 'stale' | 'denied' | null>(null);
  const [saved, setSaved] = useState(false);
  const submitting = useRef(false);
  const loadGeneration = useRef(0);

  const fields: { key: Field; max: number; label: string; help: string }[] = [
    {
      key: 'parentChangeCutoffHours', max: 8760,
      label: t('Parent change cutoff (hours)', '家長更改申請期限（小時）'),
      help: t('How many hours before a lesson starts parents must request a change.', '家長須在課堂開始前最少多少小時提出更改申請。'),
    },
    {
      key: 'attendanceOpenMinutes', max: 1440,
      label: t('Attendance opens after start (minutes)', '開始後開放點名（分鐘）'),
      help: t('How many minutes after a lesson starts coaches can record attendance.', '課堂開始後多少分鐘，教練可記錄出席情況。'),
    },
    {
      key: 'correctionWindowDays', max: 365,
      label: t('Attendance correction window (days)', '出席紀錄更正期限（日）'),
      help: t('The number of days allowed for attendance corrections.', '可更正出席紀錄的日數。'),
    },
  ];

  function deny(error: unknown) {
    if (!(error instanceof APIError) || error.status !== 403) return false;
    setSettings(null);
    setDraft(null);
    setError('denied');
    return true;
  }

  async function load(keepDraft = false) {
    const generation = ++loadGeneration.current;
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      const value = await api<Settings>('/settings/lessons');
      if (generation !== loadGeneration.current) return;
      const latest = draftFrom(value);
      if (keepDraft && settings && draft) {
        for (const { key } of fields) {
          if (draft[key] !== String(settings[key])) latest[key] = draft[key];
        }
      }
      if (!keepDraft || !settings || email === settings.cancellationEmail) setEmail(value.cancellationEmail);
      setSettings(value);
      setDraft(latest);
      setStale(false);
    } catch (error) {
      if (generation === loadGeneration.current && !isAborted(error) && !deny(error)) setError('load');
    } finally {
      if (generation === loadGeneration.current) setBusy(false);
    }
  }

  useEffect(() => { void load(); }, [api]);

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!settings || !draft || stale || busy || submitting.current || error === 'load') return;
    const values = {
      parentChangeCutoffHours: Number(draft.parentChangeCutoffHours),
      attendanceOpenMinutes: Number(draft.attendanceOpenMinutes),
      correctionWindowDays: Number(draft.correctionWindowDays),
    };
    if (fields.some(({ key, max }) => draft[key] === '' || !Number.isInteger(values[key]) || values[key] < 0 || values[key] > max)) return;
    submitting.current = true;
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      const value = await api<Settings>('/settings/lessons', {
        method: 'PATCH',
        headers: { 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ ...values, cancellationEmail: email, version: settings.version }),
      });
      setSettings(value);
      setDraft(draftFrom(value));
      setEmail(value.cancellationEmail);
      setSaved(true);
    } catch (error) {
      if (!isAborted(error) && !deny(error)) {
        const conflict = error instanceof APIError && error.status === 409;
        setStale(conflict);
        setError(conflict ? 'stale' : 'save');
      }
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  }

  return <section className="panel section-gap" aria-labelledby={`${id}-title`}>
    <h2 id={`${id}-title`}>{t('Lesson timing', '課堂時間規則')}</h2>
    {error === 'denied' && <p role="alert" className="error">{t('Lesson timing is unavailable for your current access.', '目前權限無法存取課堂時間規則。')}</p>}
    {error === 'load' && <p role="alert" className="error">{t('Could not load lesson timing. Reload to retry.', '未能載入課堂時間規則，請重新載入。')}</p>}
    {error === 'save' && <p role="alert" className="error">{t('Could not save lesson timing. Your entries are preserved.', '未能儲存課堂時間規則，已保留輸入值。')}</p>}
    {error === 'stale' && <p role="alert" className="error">{t('Lesson timing changed. Reload and review the saved values before saving again. Your entries are preserved.', '課堂時間規則已變更。請重新載入並檢查已儲存的數值後再儲存。已保留輸入值。')}</p>}
    {saved && <p role="status" className="success">{t('Lesson timing saved.', '已儲存課堂時間規則。')}</p>}
    {busy && !draft && <p role="status">{t('Loading lesson timing…', '正在載入課堂時間規則…')}</p>}
    {settings && draft ? <form onSubmit={save}>
      {fields.map(({ key, max, label, help }) => <div key={key}>
        <label htmlFor={`${id}-${key}`}>{label}</label>
        <input id={`${id}-${key}`} aria-describedby={`${id}-${key}-help`} type="number" min={0} max={max} step={1} required disabled={busy}
          value={draft[key]} onChange={event => { setDraft({ ...draft, [key]: event.target.value }); setSaved(false); }}/>
        <p className="muted" id={`${id}-${key}-help`}>{help}
          {draft[key] !== String(settings[key]) && <> {t(`Saved value: ${settings[key]}.`, `已儲存數值：${settings[key]}。`)}</>}
        </p>
      </div>)}
      <label className="checkbox-label"><input type="checkbox" checked={email} disabled={busy} onChange={event => { setEmail(event.target.checked); setSaved(false); }}/>
        {t('Email families when a guardian cancels a lesson', '家長取消課堂時以電郵通知家庭')}</label>
      <p className="muted">{t('In-app notices are always sent. Each family can turn off service email in their notification settings.', '系統內通知一定會發出。每個家庭可在通知設定中關閉服務電郵。')}</p>
      <div className="form-actions">
        <button disabled={busy || stale || error === 'load'}>{t('Save lesson timing', '儲存課堂時間規則')}</button>
        <button type="button" className="outline" disabled={busy} onClick={() => void load(true)}>{t('Reload lesson timing', '重新載入課堂時間規則')}</button>
      </div>
    </form> : error === 'load' && <button className="outline" onClick={() => void load()}>{t('Reload lesson timing', '重新載入課堂時間規則')}</button>}
  </section>;
}
