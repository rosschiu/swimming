import { useEffect, useRef, useState, type FormEvent } from 'react';
import { APIError, request } from './api';
import type { Translate } from './useProtectedRequest';

export type AccessLink = { kind: 'invite' | 'reset'; token: string };

export function readAccessLink(): AccessLink | null {
  const path = window.location.pathname;
  if (path !== '/accept-invitation' && path !== '/reset-password') return null;
  return { kind: path === '/accept-invitation' ? 'invite' : 'reset', token: new URLSearchParams(window.location.search).get('token') || '' };
}

export default function AccountAccess({ link, t, onBack }: { link: AccessLink | null; t: Translate; onBack: () => void }) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [error, setError] = useState('');
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  async function submit(event: FormEvent) {
    event.preventDefault(); setError('');
    if (link && (new TextEncoder().encode(password).length < 12 || new TextEncoder().encode(password).length > 72 || !password.trim())) { setError(t('Use 12–72 bytes for your password; non-Latin characters may use more than one byte.', '密碼須為 12–72 位元組，中文字元可能佔多個位元組。')); return; }
    if (link && password !== confirmation) { setError(t('Passwords do not match.', '兩次輸入的密碼不相同。')); return; }
    setBusy(true); controller.current = new AbortController();
    try {
      await request(link ? link.kind === 'invite' ? '/auth/accept-invitation' : '/auth/password-reset/confirm' : '/auth/password-reset/request', {
        method: 'POST', signal: controller.current.signal,
        body: JSON.stringify(link ? { token: link.token, password } : { email }),
      });
      if (controller.current.signal.aborted) return;
      setDone(true); setPassword(''); setConfirmation('');
    } catch (e) {
      if (controller.current.signal.aborted) return;
      setError(link && e instanceof APIError && e.status === 422 ? t('Please use a non-blank password of 12–72 UTF-8 bytes.', '請設定非空白、12–72 UTF-8 位元組的密碼。') : link ? t('This link could not be used. It may have expired or already been used. Request a new link or contact your school.', '未能使用此連結，可能已過期或已使用。請索取新連結或聯絡學校。') : t('The request could not be sent. Please try again.', '未能發送要求，請再試。'));
    } finally { if (!controller.current.signal.aborted) setBusy(false); }
  }
  const heading = link?.kind === 'invite' ? t('Activate your account', '啟用帳戶') : t('Reset your password', '重設密碼');
  return <section className="login-card access-card"><p className="eyebrow">SWIMCOACH PRO</p><h1>{heading}</h1>
    {done ? <><p role="status" className="success">{link ? t('Your password has been saved. Sign in to continue.', '已儲存密碼，請登入以繼續。') : t('If an eligible account exists, password-reset instructions will be sent to that email address.', '如有符合資格的帳戶，重設密碼指示將發送至該電郵地址。')}</p><button onClick={onBack}>{t('Back to sign in', '返回登入')}</button></> : link && !link.token ? <><p role="alert" className="error">{t('This link is missing its access token. Please use the complete link sent by your school.', '此連結缺少驗證資料，請使用學校發送的完整連結。')}</p><button onClick={onBack}>{t('Back to sign in', '返回登入')}</button></> : <form onSubmit={submit}>
      {link ? <><p className="muted">{t('Choose a password of 12–72 UTF-8 bytes. English letters use one byte; Chinese characters usually use three.', '請設定 12–72 UTF-8 位元組的密碼。英文字母佔一個位元組，中文字通常佔三個。')}</p><label htmlFor="new-password">{t('New password', '新密碼')}</label><input id="new-password" type="password" autoComplete="new-password" maxLength={72} required value={password} onChange={event => setPassword(event.target.value)}/><label htmlFor="confirm-password">{t('Confirm password', '確認密碼')}</label><input id="confirm-password" type="password" autoComplete="new-password" maxLength={72} required value={confirmation} onChange={event => setConfirmation(event.target.value)}/></> : <><p className="muted">{t('Enter your account email. We will send instructions if it is eligible for recovery.', '請輸入帳戶電郵。如符合資格，我們會發送重設指示。')}</p><label htmlFor="reset-email">{t('Email', '電郵')}</label><input id="reset-email" type="email" autoComplete="email" required value={email} onChange={event => setEmail(event.target.value)}/></>}
      {error && <p role="alert" className="error">{error}</p>}<div className="form-actions"><button disabled={busy}>{busy ? t('Submitting…', '提交中…') : link ? t('Save password', '儲存密碼') : t('Send reset instructions', '發送重設指示')}</button><button type="button" className="outline" onClick={onBack} disabled={busy}>{t('Back to sign in', '返回登入')}</button></div>
    </form>}
  </section>;
}
