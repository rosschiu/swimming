import { useEffect, useRef, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps, type Translate } from './useProtectedRequest';
export type Policy = components['schemas']['CreditPolicy'];
export type Offer = components['schemas']['PackageOffer'];
type Settings = components['schemas']['CreditSettings'];
export const money = (minor: number) => `HKD ${(minor / 100).toFixed(2)}`;
const policyLabels: [keyof Policy, string, string][] = [
  ['expiryEnabled', 'Expiry enabled', '啟用有效期限'], ['carryForwardEnabled', 'Carry-forward allowed', '允許結轉'],
  ['earlyCancellationRetainsCredit', 'Early cancellation retains credit', '提前取消保留堂數'], ['lateCancellationRetainsCredit', 'Late cancellation retains credit', '逾期取消保留堂數'],
  ['absenceRetainsCredit', 'Absence retains credit', '缺席保留堂數'], ['illnessRetainsCredit', 'Illness retains credit', '病假保留堂數'],
];
export function PolicyFields({ value, change, disabled, t }: { value: Policy; change: (policy: Policy) => void; disabled: boolean; t: Translate }) {
  return <>{policyLabels.map(([key, en, zh]) => <label className="checkbox-label" key={key}><input type="checkbox" checked={value[key]} disabled={disabled} onChange={e => change({ ...value, [key]: e.target.checked })}/>{t(en, zh)}</label>)}</>;
}
export function PolicyTerms({ policy, t }: { policy: Policy; t: Translate }) {
  return <ul>{policyLabels.map(([key, en, zh]) => <li key={key}>{t(en, zh)}: {policy[key] ? t('Yes', '是') : t('No', '否')}</li>)}</ul>;
}
export default function PackageCatalogue({ csrfToken, expired, t, offers, settings, reload, loading }: ProtectedProps & { offers: Offer[]; settings: Settings; reload: () => Promise<void>; loading: boolean }) {
  const api = useProtectedRequest(expired);
  const [editing, setEditing] = useState<Offer | null>(null);
  const [open, setOpen] = useState(false);
  const [name, setName] = useState(''); const [credits, setCredits] = useState(1); const [price, setPrice] = useState('1.00');
  const [active, setActive] = useState(true); const [override, setOverride] = useState(false);
  const [policy, setPolicy] = useState(settings.policy); const [defaults, setDefaults] = useState(settings.policy);
  const [settingsVersion, setSettingsVersion] = useState(settings.version);
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [notice, setNotice] = useState('');
  const [review, setReview] = useState<'offer' | 'settings' | null>(null); const submitting = useRef(false);
  useEffect(() => { if (!review) { setDefaults(settings.policy); setSettingsVersion(settings.version); } }, [settings]);
  function edit(item: Offer | null) {
    setEditing(item); setName(item?.name ?? ''); setCredits(item?.credits ?? 1); setPrice(((item?.priceMinor ?? 100) / 100).toFixed(2)); setActive(item?.active ?? true); setOverride(!!item?.policyOverride); setPolicy(item?.policyOverride ?? settings.policy); setOpen(true); setError(''); setNotice('');
  }
  async function save(event: FormEvent, kind: 'offer' | 'settings') {
    event.preventDefault(); if (submitting.current || review) return;
    const priceParts = /^(\d{1,7})(?:\.(\d{1,2}))?$/.exec(price.trim());
    const priceMinor = priceParts ? Number(priceParts[1]) * 100 + Number((priceParts[2] ?? '').padEnd(2, '0')) : 0;
    if (kind === 'offer' && (!name.trim() || [...name.trim()].length > 100 || !Number.isInteger(credits) || credits < 1 || credits > 10000 || !priceParts || priceMinor < 1 || priceMinor > 100000000)) { setError(t('Check the package name, credits and price in HKD (up to two decimal places).', '請檢查套票名稱、堂數及港幣價格（最多兩位小數）。')); return; }
    submitting.current = true; setBusy(true); setError(''); setNotice('');
    try {
      await api(kind === 'settings' ? '/settings/credits' : `/package-offers${editing ? `/${editing.id}` : ''}`, { method: kind === 'settings' || editing ? 'PATCH' : 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify(kind === 'settings' ? { policy: defaults, version: settingsVersion } : { name: name.trim(), credits, priceMinor, currency: 'HKD', active, policyOverride: override ? policy : null, ...(editing ? {version: editing.version} : {}) }) });
      if (kind === 'offer') setOpen(false); setNotice(t('Saved. Existing purchase terms are unchanged.', '已儲存，現有購買條款維持不變。')); await reload();
    } catch (e) { if (!isAborted(e)) { if (!(e instanceof APIError) || e.status >= 500 || e.status === 409) { setReview(kind); await reload(); setError(t('The record changed or saving could not be confirmed. Review the reloaded records before using the current version. For a new offer, check whether it was already created.', '資料已變更或未能確認儲存結果。請檢查重新載入的資料後才使用最新版本。新增套票時，請先確認是否已建立。')); } else setError(t('Could not save. Check the fields and your access; your draft is preserved.', '未能儲存，請檢查欄位及權限。已保留草稿。')); } }
    finally { submitting.current = false; setBusy(false); }
  }
  const locked = busy || loading || !!review;
  return <>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    <section className="panel section-gap"><h2>{t('Credit policy defaults', '堂數政策預設值')}</h2><p className="muted">{t('Changes apply to new purchases only. Each expiry date is assigned individually after purchase creation. Booking, renewal and recovery are delivered separately.', '更改只適用於新購買。每次購買建立後，須個別指定到期日。預約、續期及恢復堂數將另行提供。')}</p><form onSubmit={e => save(e, 'settings')}><PolicyFields value={defaults} change={setDefaults} disabled={locked} t={t}/><button disabled={locked}>{t('Save credit policy', '儲存堂數政策')}</button></form></section>
    <section className="section-gap"><div className="page-heading"><h2>{t('Package offers', '套票選項')}</h2><button disabled={locked || open} onClick={() => edit(null)}>{t('Add package offer', '新增套票')}</button></div><div className="record-list">{offers.map(offer => <article className="panel" key={offer.id}><h3>{offer.name}</h3><p>{offer.credits} {t('credits', '堂')} · {money(offer.priceMinor)} · {offer.active ? t('Active', '使用中') : t('Archived', '已封存')} · {t('Version', '版本')} {offer.version}</p><p>{offer.policyOverride ? t('Package policy override', '套票自訂政策') : t('Uses school defaults for new purchases', '新購買採用學校預設政策')}</p><button className="outline" disabled={locked || open} aria-label={`${t('Edit package offer', '編輯套票')} ${offer.name}`} onClick={() => edit(offer)}>{t('Edit', '編輯')}</button></article>)}</div></section>
    {open && <section className="panel section-gap"><h2>{editing ? t('Edit package offer', '編輯套票') : t('New package offer', '新套票')}</h2><form onSubmit={e => save(e, 'offer')}><label htmlFor="package-name">{t('Package name', '套票名稱')}</label><input id="package-name" required maxLength={100} disabled={locked} value={name} onChange={e => setName(e.target.value)}/><label htmlFor="package-credits">{t('Credits', '堂數')}</label><input id="package-credits" type="number" min={1} max={10000} step={1} required disabled={locked} value={credits} onChange={e => setCredits(Number(e.target.value))}/><label htmlFor="package-price">{t('Price (HKD)', '價格（港幣）')}</label><input id="package-price" type="text" inputMode="decimal" required disabled={locked} value={price} onChange={e => setPrice(e.target.value)}/><label className="checkbox-label"><input type="checkbox" checked={active} disabled={locked} onChange={e => setActive(e.target.checked)}/>{t('Available for new purchases', '可供新購買')}</label><label className="checkbox-label"><input type="checkbox" checked={override} disabled={locked} onChange={e => setOverride(e.target.checked)}/>{t('Override school credit policy', '自訂套票堂數政策')}</label>{override && <PolicyFields value={policy} change={setPolicy} disabled={locked} t={t}/>}<div className="form-actions"><button disabled={locked}>{t('Save package offer', '儲存套票')}</button><button type="button" className="outline" disabled={busy} onClick={() => {setOpen(false); setReview(null); setError('');}}>{t('Cancel', '取消')}</button></div></form></section>}
    {review && <section className="panel section-gap"><p>{t('Compare your preserved draft with the current records before continuing. Cancel an offer draft if it is already saved.', '請先比較保留的草稿及最新資料。如套票已儲存，請取消草稿。')}</p><button disabled={busy || loading || (review === 'offer' && !!editing && !offers.some(o => o.id === editing.id))} onClick={() => { if (review === 'offer' && editing) setEditing(offers.find(o => o.id === editing.id)!); if (review === 'settings') setSettingsVersion(settings.version); setReview(null); setError(''); }}>{t('Use current version; keep my draft', '使用最新版本並保留草稿')}</button></section>}
  </>;
}
