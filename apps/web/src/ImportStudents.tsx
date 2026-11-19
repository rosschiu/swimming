import { useId, useState } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Preview = components['schemas']['ImportPreview'];
type Result = components['schemas']['ImportResult'];
const xlsx = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';

// Moves an existing school's swimmers, guardians and remaining credits in from one Excel sheet: check first, then import.
export default function ImportStudents({ csrfToken, expired, t, onImported }: ProtectedProps & { onImported: () => void }) {
  const api = useProtectedRequest(expired);
  const id = useId();
  const [file, setFile] = useState<File | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [result, setResult] = useState<Result | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const headers = { 'X-CSRF-Token': csrfToken, 'Content-Type': xlsx };

  async function check(selected: File) {
    setBusy(true); setError(''); setPreview(null); setResult(null);
    try { setPreview(await api<Preview>('/imports/preview', { method: 'POST', headers, body: selected })); }
    catch (e) { if (!isAborted(e)) setError(e instanceof APIError && e.status !== 500 ? e.message : t('Could not check the file. Try again.', '未能檢查檔案，請再試。')); }
    finally { setBusy(false); }
  }
  async function run() {
    if (!file || !preview?.valid || busy) return;
    setBusy(true); setError('');
    try {
      setResult(await api<Result>(`/imports?fileSha256=${preview.fileSha256}`, { method: 'POST', headers, body: file }));
      setPreview(null); setFile(null); onImported();
    } catch (e) {
      if (isAborted(e)) return;
      // The sheet or the school changed since the check, so check again and show what is wrong now.
      if (e instanceof APIError && (e.status === 409 || e.status === 422)) { setBusy(false); await check(file); setError(t('Something changed since the check. Review the updated results.', '檢查後資料有變，請查看最新結果。')); return; }
      setError(t('Could not import. Nothing was changed; try again.', '未能匯入，資料未有改動，請再試。'));
    } finally { setBusy(false); }
  }
  const problems = preview ? preview.rows.filter(row => row.errors.length) : [];
  return <section className="section-gap" aria-labelledby={`${id}-title`}>
    <h2 id={`${id}-title`}>{t('Import from Excel', '從 Excel 匯入')}</h2>
    <p className="muted">{t('Move swimmers, guardians and remaining credits from your previous records. Use one row per swimmer. New guardian accounts get no email until you send their invitation from Invitations.', '從舊紀錄轉移學員、家長及剩餘堂數。每位學員一行。新家長帳戶不會收到電郵，直至你在「邀請紀錄」發送邀請。')}</p>
    <p><a className="action-link" href="/api/v1/imports/template" download>{t('Download the template', '下載範本')}</a></p>
    <label htmlFor={`${id}-file`}>{t('Excel file (.xlsx)', 'Excel 檔案（.xlsx）')}</label>
    <input id={`${id}-file`} type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" disabled={busy}
      onChange={event => { const selected = event.target.files?.[0] ?? null; event.target.value = ''; setFile(selected); if (selected) void check(selected); }}/>
    {busy && <p role="status">{t('Working…', '處理中…')}</p>}
    {error && <p role="alert" className="error">{error}</p>}
    {result && <p role="status" className="success">{t(`Imported ${result.students} swimmers, ${result.guardiansCreated} new and ${result.guardiansLinked} existing guardians, and ${result.credits} credits in ${result.packages} migrated balances.`, `已匯入 ${result.students} 位學員、${result.guardiansCreated} 位新家長及 ${result.guardiansLinked} 位現有家長，以及 ${result.packages} 個遷移餘額共 ${result.credits} 堂。`)}</p>}
    {preview && <div className="panel section-gap">
      <h3>{preview.valid ? t('Ready to import', '可以匯入') : t('Fix these problems in the sheet, then choose the file again', '請在試算表修正以下問題，然後重新選擇檔案')}</h3>
      <p>{t(`${preview.summary.students} swimmers · ${preview.summary.guardiansCreated} new guardians · ${preview.summary.guardiansLinked} existing guardians · ${preview.summary.credits} credits`, `${preview.summary.students} 位學員 · ${preview.summary.guardiansCreated} 位新家長 · ${preview.summary.guardiansLinked} 位現有家長 · ${preview.summary.credits} 堂`)}</p>
      {!!preview.ignoredColumns.length && <p className="muted">{t('Ignored columns:', '已略過的欄：')} {preview.ignoredColumns.join(', ')}</p>}
      {preview.errors.map(message => <p className="error" key={message}>{message}</p>)}
      {!!problems.length && <ul>{problems.map(row => <li key={row.row}>{t(`Row ${row.row}`, `第 ${row.row} 行`)} · {row.studentName || '—'}: {row.errors.join(' ')}</li>)}</ul>}
      {preview.valid && <div className="form-actions"><button disabled={busy} onClick={() => void run()}>{t(`Import ${preview.summary.students} swimmers`, `匯入 ${preview.summary.students} 位學員`)}</button></div>}
    </div>}
  </section>;
}
