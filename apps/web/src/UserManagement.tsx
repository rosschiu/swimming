import { useEffect, useState, type FormEvent } from 'react';
import { APIError, type Student } from './api';
import type { components } from './generated/api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';

type AdminUser = components['schemas']['AdminUser'];
type Invitation = components['schemas']['Invitation'];

export default function UserManagement({ csrfToken, expired, t, students }: ProtectedProps & { students: Student[] }) {
  const api = useProtectedRequest(expired);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [role, setRole] = useState<AdminUser['role']>('parent');
  const [studentId, setStudentId] = useState('');
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState('');
  const [editing, setEditing] = useState<AdminUser | null>(null);
  const [editName, setEditName] = useState('');
  const [active, setActive] = useState(true);
  const roleNames = { admin: t('Administrator', '管理員'), parent: t('Parent', '家長'), coach: t('Coach', '教練'), student: t('Student', '學員') };
  const statusNames = { pending: t('Pending', '待接受'), accepted: t('Accepted', '已接受'), revoked: t('Revoked', '已撤銷'), expired: t('Expired', '已過期') };
  const deliveryNames = { pending: t('Delivery pending', '等待發送'), sent: t('Email sent', '已發送電郵'), failed: t('Delivery failed', '發送失敗') };
  const headers = { 'X-CSRF-Token': csrfToken };

  async function load() {
    setLoading(true); setError('');
    try {
      const [userList, invitationList] = await Promise.all([api<{items: AdminUser[]}>('/users'), api<{items: Invitation[]}>('/invitations')]);
      setUsers(userList.items); setInvitations(invitationList.items);
    } catch (e) { if (!isAborted(e)) setError(t('Could not load accounts. Please retry.', '未能載入帳戶，請重試。')); }
    finally { setLoading(false); }
  }
  useEffect(() => { void load(); }, [api]);
  async function invite(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(''); setNotice('');
    try {
      const invitation = await api<Invitation>('/invitations', { method: 'POST', headers, body: JSON.stringify({ email: email.trim(), name: name.trim(), role, ...(studentId && role !== 'admin' ? { studentId } : {}) }) });
      setEmail(''); setName(''); setStudentId('');
      setNotice(invitation.deliveryStatus === 'failed' ? t('Invitation created, but email delivery failed. Retry delivery below.', '已建立邀請，但電郵發送失敗。請在下方重新發送。') : t('Invitation created. Check its delivery status below.', '已建立邀請，請在下方查看發送狀態。'));
      await load();
    } catch (e) { if (!isAborted(e)) setError(e instanceof APIError && e.status === 409 ? t('An account or invitation already exists for this email. Review the list below.', '此電郵已有帳戶或邀請，請查看下方名單。') : t('Could not create the invitation. Your entries are preserved; check the details and try again.', '未能建立邀請。已保留輸入內容，請檢查資料後再試。')); }
    finally { setBusy(false); }
  }
  async function update(event: FormEvent) {
    event.preventDefault(); if (!editing) return;
    setBusy(true); setError(''); setNotice('');
    try {
      await api<AdminUser>(`/users/${editing.id}`, { method: 'PATCH', headers, body: JSON.stringify({ name: editName.trim(), active, version: editing.version }) });
      setEditing(null); setNotice(t('Account updated.', '已更新帳戶。')); await load();
    } catch (e) { if (!isAborted(e)) setError(e instanceof APIError && e.status === 409 ? t('This account changed or the update is not allowed. Refresh the list and review the account before retrying.', '此帳戶已變更或不允許此操作。請重新載入名單並檢查帳戶後再試。') : t('Could not update the account. Your entries are preserved.', '未能更新帳戶，已保留輸入內容。')); }
    finally { setBusy(false); }
  }
  async function invitationAction(id: string, action: 'resend' | 'revoke') {
    setBusy(true); setError(''); setNotice('');
    try {
      const result = await api<Invitation | undefined>(`/invitations/${id}/${action}`, { method: 'POST', headers, body: '{}' });
      setNotice(action === 'revoke' ? t('Invitation revoked.', '已撤銷邀請。') : result?.deliveryStatus === 'failed' ? t('Email delivery failed. Please retry or check email configuration.', '電郵發送失敗，請重試或檢查電郵設定。') : t('Invitation resent. Previous links can no longer be used.', '已重新發送邀請，舊連結已失效。')); await load();
    } catch (e) { if (!isAborted(e)) setError(t('Could not update the invitation. Refresh and try again.', '未能更新邀請，請重新載入後再試。')); }
    finally { setBusy(false); }
  }
  const visible = users.filter(user => (!filter || user.role === filter) && `${user.name} ${user.email}`.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
  return <section><div className="page-heading"><div><p className="eyebrow">{t('ACCESS MANAGEMENT', '權限管理')}</p><h1>{t('Accounts & invitations', '帳戶及邀請')}</h1><p className="muted">{t('Manage school access and invite new members.', '管理學校權限並邀請新成員。')}</p></div><button className="outline" disabled={loading || busy} onClick={load}>{t('Refresh', '重新載入')}</button></div>
    {error && <p className="error" role="alert">{error}</p>}{notice && <p className="success" role="status">{notice}</p>}
    <section className="panel"><h2>{t('Invite a member', '邀請成員')}</h2><form onSubmit={invite} className="field-grid"><div><label htmlFor="invite-name">{t('Member name', '成員姓名')}</label><input id="invite-name" required maxLength={100} value={name} onChange={e => setName(e.target.value)}/></div><div><label htmlFor="invite-email">{t('Member email', '成員電郵')}</label><input id="invite-email" type="email" required value={email} onChange={e => setEmail(e.target.value)}/></div><div><label htmlFor="invite-role">{t('Role', '身份')}</label><select id="invite-role" value={role} onChange={e => { setRole(e.target.value as AdminUser['role']); setStudentId(''); }}>{Object.entries(roleNames).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></div>{role !== 'admin' && <div><label htmlFor="invite-student">{role === 'student' ? t('Student profile (required)', '學員資料（必填）') : t('Link student (optional)', '連結學員（選填）')}</label><select id="invite-student" required={role === 'student'} value={studentId} onChange={e => setStudentId(e.target.value)}><option value="">{t('Choose a student', '選擇學員')}</option>{students.map(student => <option key={student.id} value={student.id}>{student.name}</option>)}</select></div>}<div className="full-row"><button disabled={busy || !name.trim()}>{t('Send invitation', '發送邀請')}</button></div></form></section>
    {editing && <section className="panel section-gap"><h2>{t('Edit account', '編輯帳戶')} · {editing.email}</h2><form onSubmit={update}><label htmlFor="account-name">{t('Account name', '帳戶姓名')}</label><input id="account-name" required maxLength={100} value={editName} onChange={e => setEditName(e.target.value)}/><label className="checkbox-label"><input type="checkbox" checked={active} onChange={e => setActive(e.target.checked)}/>{t('Account active', '啟用帳戶')}</label><p className="muted">{t('Deactivating an account ends its access. Email and role cannot be changed here.', '停用帳戶將終止其存取權限。此處不能更改電郵及身份。')}</p><div className="form-actions"><button disabled={busy || !editName.trim()}>{t('Save account', '儲存帳戶')}</button><button type="button" className="outline" disabled={busy} onClick={() => setEditing(null)}>{t('Cancel', '取消')}</button></div></form></section>}
    <section className="section-gap"><h2>{t('School accounts', '學校帳戶')}</h2><div className="field-grid"><div><label htmlFor="user-search">{t('Search accounts', '搜尋帳戶')}</label><input id="user-search" type="search" value={query} onChange={e => setQuery(e.target.value)}/></div><div><label htmlFor="user-role">{t('Filter role', '篩選身份')}</label><select id="user-role" value={filter} onChange={e => setFilter(e.target.value)}><option value="">{t('All roles', '所有身份')}</option>{Object.entries(roleNames).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></div></div>
    {loading ? <p role="status">{t('Loading accounts…', '載入帳戶中…')}</p> : <div className="record-list">{visible.map(user => <article key={user.id} className="panel record-row"><div><h3>{user.name}</h3><p>{user.email}</p><p className="muted">{roleNames[user.role]} · {user.active ? t('Active', '已啟用') : t('Inactive', '已停用')}{user.invitationPending && ` · ${t('Invitation pending', '等待接受邀請')}`}</p></div><button className="outline" disabled={busy} onClick={() => { setEditing(user); setEditName(user.name); setActive(user.active); setError(''); }} aria-label={`${t('Edit account', '編輯帳戶')} ${user.name}`}>{t('Edit', '編輯')}</button></article>)}{!visible.length && <p className="panel">{t('No accounts match your search.', '沒有符合搜尋的帳戶。')}</p>}</div>}</section>
    <section className="section-gap"><h2>{t('Invitation history', '邀請紀錄')}</h2><div className="record-list">{invitations.map(invitation => <article key={invitation.id} className="panel record-row"><div><h3>{invitation.email}</h3><p>{roleNames[invitation.role]} · {statusNames[invitation.status]} · {deliveryNames[invitation.deliveryStatus]}</p><p className="muted">{t('Expires', '有效至')}: <time dateTime={invitation.expiresAt}>{new Date(invitation.expiresAt).toLocaleString(undefined, { timeZone: 'Asia/Hong_Kong' })} (HK)</time></p></div>{invitation.status !== 'accepted' && <div className="form-actions"><button disabled={busy} onClick={() => invitationAction(invitation.id, 'resend')} aria-label={`${invitation.status === 'revoked' ? t('Reissue invitation', '重新邀請') : t('Resend invitation', '重新發送邀請')} ${invitation.email}`}>{invitation.status === 'revoked' ? t('Reissue', '重新邀請') : t('Resend', '重新發送')}</button>{invitation.status !== 'revoked' && <button disabled={busy} className="outline" onClick={() => invitationAction(invitation.id, 'revoke')} aria-label={`${t('Revoke invitation', '撤銷邀請')} ${invitation.email}`}>{t('Revoke', '撤銷')}</button>}</div>}</article>)}{!loading && !invitations.length && <p className="panel">{t('No invitations yet.', '暫無邀請。')}</p>}</div></section>
  </section>;
}
