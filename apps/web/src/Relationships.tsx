import { useEffect, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type Relationship = components['schemas']['Relationship'];
type AdminUser = components['schemas']['AdminUser'];

export default function Relationships({ studentId, csrfToken, expired, t }: ProtectedProps & { studentId: string }) {
  const api = useProtectedRequest(expired);
  const [links, setLinks] = useState<Relationship[]>([]);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [kind, setKind] = useState<Relationship['kind']>('guardian');
  const [userId, setUserId] = useState('');
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const names = { guardian: t('Guardian', '監護人'), coach: t('Coach', '教練'), self: t('Student login', '學員登入') };
  const roles = { guardian: 'parent', coach: 'coach', self: 'student' };
  const base = `/students/${studentId}/relationships`;
  async function load() {
    setLoading(true); setError('');
    try {
      const [relations, accounts] = await Promise.all([api<{items: Relationship[]}>(base), api<{items: AdminUser[]}>('/users')]);
      setLinks(relations.items); setUsers(accounts.items);
    } catch (e) { if (!isAborted(e)) setError(t('Could not load linked accounts. Please retry.', '未能載入已連結帳戶，請重試。')); }
    finally { setLoading(false); }
  }
  useEffect(() => { void load(); }, [api, studentId]);
  async function link(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(''); setNotice('');
    try {
      await api<Relationship>(`${base}/${userId}`, { method: 'PUT', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ kind }) });
      setUserId(''); setNotice(t('Account linked.', '已連結帳戶。')); await load();
    } catch (e) { if (!isAborted(e)) setError(e instanceof APIError && e.status === 409 ? t('This student or account already has a student-login link. Remove the old link first.', '此學員或帳戶已有學員登入連結，請先移除舊連結。') : t('Could not link this account. Check its role and try again.', '未能連結此帳戶，請檢查身份後再試。')); }
    finally { setBusy(false); }
  }
  async function unlink(id: string) {
    setBusy(true); setError(''); setNotice('');
    try {
      await api(`${base}/${id}`, { method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken } });
      setNotice(t('Account unlinked. Its access to this student has been removed.', '已取消連結，該帳戶已失去此學員的存取權限。')); await load();
    } catch (e) { if (!isAborted(e)) setError(t('Could not unlink the account. Please retry.', '未能取消帳戶連結，請重試。')); }
    finally { setBusy(false); }
  }
  const candidates = users.filter(user => user.role === roles[kind] && !links.some(link => link.userId === user.id));
  return <section className="section-gap" aria-labelledby="relationships-title"><div className="page-heading"><h2 id="relationships-title">{t('Linked accounts', '已連結帳戶')}</h2><button className="outline" disabled={loading || busy} onClick={load}>{t('Refresh links', '重新載入連結')}</button></div>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    {loading ? <p role="status">{t('Loading linked accounts…', '載入已連結帳戶中…')}</p> : <ul className="link-list">{links.map(link => <li key={link.userId}><span><strong>{link.name}</strong><span className="muted">{names[link.kind]}</span></span><button className="outline" disabled={busy} onClick={() => unlink(link.userId)} aria-label={`${t('Unlink', '取消連結')} ${link.name}`}>{t('Unlink', '取消連結')}</button></li>)}{!links.length && <li>{t('No linked accounts.', '尚未連結帳戶。')}</li>}</ul>}
    <form onSubmit={link} className="field-grid"><div><label htmlFor="link-kind">{t('Relationship', '關係')}</label><select id="link-kind" value={kind} onChange={e => { setKind(e.target.value as Relationship['kind']); setUserId(''); }}>{Object.entries(names).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></div><div><label htmlFor="link-user">{t('Account to link', '要連結的帳戶')}</label><select id="link-user" value={userId} required onChange={e => setUserId(e.target.value)}><option value="">{t('Choose an account', '選擇帳戶')}</option>{candidates.map(user => <option key={user.id} value={user.id}>{user.name} · {user.email}{!user.active ? ` (${t('inactive', '已停用')})` : ''}</option>)}</select></div><div className="full-row"><button disabled={busy || loading || !userId}>{t('Link account', '連結帳戶')}</button></div></form>
  </section>;
}
