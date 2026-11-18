import { useEffect, useRef, useState, type FormEvent } from 'react';
import type { components } from './generated/api';
import { APIError, type Student, type User } from './api';
import { isAborted, useProtectedRequest, type ProtectedProps } from './useProtectedRequest';
import PackageCatalogue, { money, PolicyTerms, type Offer } from './PackageCatalogue';
import PurchaseHistory, { Credits } from './PurchaseHistory';
type Settings = components['schemas']['CreditSettings'];
type Purchase = components['schemas']['Purchase'];
type CreditPackage = components['schemas']['CreditPackage'];
export default function Packages({ role, csrfToken, expired, t }: ProtectedProps & {role: User['role']}) {
  const api = useProtectedRequest(expired);
  const [offers, setOffers] = useState<Offer[]>([]); const [settings, setSettings] = useState<Settings | null>(null);
  const [students, setStudents] = useState<Student[]>([]); const [studentId, setStudentId] = useState('');
  const [purchases, setPurchases] = useState<Purchase[]>([]); const [queue, setQueue] = useState('awaiting_expiry');
  const [revision, setRevision] = useState(0); const [loading, setLoading] = useState(true); const [error, setError] = useState(false); const sequence = useRef(0);
  const admin = role === 'admin';
  async function load() {
    const current = ++sequence.current; setLoading(true); setError(false);
    try {
      const [catalogue, policy, directory, orders] = await Promise.all([
        api<{items: Offer[]}>('/package-offers'), api<Settings>('/settings/credits'),
        api<{items: Student[]}>('/students'),
        admin ? api<{items: Purchase[]}>(`/purchases${queue ? `?status=${queue}` : ''}`) : Promise.resolve({items: [] as Purchase[]}),
      ]);
      if (current !== sequence.current) return;
      setOffers(catalogue.items); setSettings(policy); setStudents(directory.items); setPurchases(orders.items); setRevision(value => value + 1);
      // A guardian with a single linked child starts on that child's checkout.
      setStudentId(id => directory.items.some(student => student.id === id) ? id : !admin && directory.items.length === 1 ? directory.items[0].id : '');
    } catch (e) { if (current === sequence.current && !isAborted(e)) {if (e instanceof APIError && (e.status === 403 || e.status === 404)) {setOffers([]); setSettings(null); setStudents([]); setPurchases([]); setStudentId('');} setError(true);} }
    finally {if (current === sequence.current) setLoading(false);}
  }
  useEffect(() => { if (role === 'admin' || role === 'parent') void load(); }, [api, queue]);
  if (role !== 'admin' && role !== 'parent') return null;
  return <section><div className="page-heading"><div><h1>{t('Packages and payments', '套票及付款')}</h1><p className="muted">{t('Simulation only. No money is charged or transferred.', '僅供模擬，不會收取或轉移任何款項。')}</p></div><button className="outline" disabled={loading} onClick={load}>{t('Reload packages', '重新載入套票')}</button></div>
    {error && <section className="panel"><p role="alert">{t('Could not load packages. Please retry.', '未能載入套票，請重試。')}</p><button disabled={loading} onClick={load}>{t('Retry loading packages', '重試載入套票')}</button></section>}{!settings ? !error && <p role="status">{t('Loading packages…', '載入套票中…')}</p> : admin ? <>
      <PackageCatalogue csrfToken={csrfToken} expired={expired} t={t} offers={offers} settings={settings} reload={load} loading={loading || error}/>
      <label htmlFor="purchase-queue">{t('Purchase queue', '購買待辦')}</label><select id="purchase-queue" value={queue} disabled={loading} onChange={e => setQueue(e.target.value)}><option value="awaiting_expiry">{t('Awaiting expiry', '等待到期日')}</option><option value="">{t('All purchases', '所有購買')}</option></select>
      <PurchaseHistory studentNames={Object.fromEntries(students.map(student => [student.id, student.name]))} items={purchases} admin csrfToken={csrfToken} expired={expired} t={t} reload={load} loading={loading || error} invalidate={() => { setPurchases([]); void load(); }}/>
    </> : null}
      {(admin || role === 'parent') && settings && <>
      <label htmlFor="purchase-child">{admin ? t('Choose student for credit history', '選擇學員以查看堂數紀錄') : t('Choose child', '選擇子女')}</label><select id="purchase-child" disabled={loading} value={studentId} onChange={e => setStudentId(e.target.value)}><option value="">{admin ? t('Choose a student', '選擇學員') : t('Choose a linked child', '選擇已連結子女')}</option>{students.map(student => <option key={student.id} value={student.id}>{student.name}</option>)}</select>
      {!students.length && <p>{admin ? t('No students are available.', '暫無學員。') : t('No linked children are available.', '暫無已連結子女。')}</p>}
      {studentId && <ChildPackages revision={revision} admin={admin} studentName={students.find(student => student.id === studentId)?.name ?? studentId} key={studentId} studentId={studentId} offers={offers} settings={settings} csrfToken={csrfToken} expired={expired} t={t} reloadCatalogue={load} catalogueLoading={loading || error}/>}
    </>}
  </section>;
}
function ChildPackages({studentId, studentName, admin, revision, offers, settings, csrfToken, expired, t, reloadCatalogue, catalogueLoading}: ProtectedProps & {studentId: string; studentName: string; admin: boolean; revision: number; offers: Offer[]; settings: Settings; reloadCatalogue: () => Promise<void>; catalogueLoading: boolean}) {
  const api = useProtectedRequest(expired); const [purchases, setPurchases] = useState<Purchase[]>([]); const [credits, setCredits] = useState<CreditPackage[]>([]);
  const [offerId, setOfferId] = useState(''); const [loading, setLoading] = useState(true); const [loadError, setLoadError] = useState(false); const [denied, setDenied] = useState(false);
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [retry, setRetry] = useState(false); const [review, setReview] = useState(false); const [notice, setNotice] = useState('');
  const sequence = useRef(0); const submitting = useRef(false); const pending = useRef<{offerId: string; offerVersion: number; idempotencyKey: string} | null>(null);
  const base = `/students/${studentId}`;
  function invalidate() {sequence.current++; setPurchases([]); setCredits([]); setDenied(true); setLoading(false); setError(''); setNotice('');}
  async function load() {
    const current = ++sequence.current; setLoading(true); setLoadError(false);
    try {const [orders, ledger] = await Promise.all([api<{items: Purchase[]}>(`${base}/purchases`), api<{items: CreditPackage[]}>(`${base}/credits`)]); if (current === sequence.current) {setPurchases(orders.items); setCredits(ledger.items); setDenied(false);} }
    catch (e) {if (current === sequence.current && !isAborted(e)) {setPurchases([]); setCredits([]); if (e instanceof APIError && (e.status === 403 || e.status === 404)) invalidate(); else setLoadError(true);} }
    finally {if (current === sequence.current) setLoading(false);}
  }
  useEffect(() => {void load();}, [api, revision]);
  const selected = offers.find(offer => offer.id === offerId && offer.active);
  async function create(event: FormEvent) {
    event.preventDefault(); if (submitting.current || review || (!pending.current && !selected)) return;
    pending.current ??= {offerId: selected!.id, offerVersion: selected!.version, idempotencyKey: crypto.randomUUID()};
    submitting.current = true; setBusy(true); setError(''); setNotice('');
    try {await api<Purchase>(`${base}/purchases`, {method: 'POST', headers: {'X-CSRF-Token': csrfToken}, body: JSON.stringify(pending.current)}); pending.current = null; setRetry(false); setOfferId(''); setNotice(t('Purchase created. Review its assigned terms before mock payment.', '已建立購買，請在模擬付款前檢查已指定條款。')); await load();}
    catch (e) {if (!isAborted(e)) {
      if (e instanceof APIError && (e.status === 403 || e.status === 404)) {invalidate(); return;}
      const uncertain = !(e instanceof APIError) || e.status >= 500;
      if (uncertain) {setRetry(true); setError(t('Purchase creation could not be confirmed. Retry the same submission to avoid duplicate purchases.', '未能確認建立購買結果。請重試同一次提交以避免重複購買。'));}
      else {pending.current = null; setRetry(false); setReview(true); setError(t('The offer changed or purchase is unavailable. Review the reloaded offers before continuing.', '套票已變更或無法購買。請檢查重新載入的套票後再繼續。')); await reloadCatalogue();}
    }} finally {submitting.current = false; setBusy(false);}
  }
  if (denied) return <section className="panel section-gap"><p role="alert">{t('This child’s financial records are unavailable. Reload packages to check your current access.', '無法存取此子女的財務紀錄，請重新載入套票以檢查目前權限。')}</p></section>;
  const locked = busy || loading || catalogueLoading || loadError;
  return <section className="section-gap"><div className="page-heading"><h2>{admin ? t('Student packages', '學員套票') : t('Child packages', '子女套票')}</h2><button className="outline" disabled={busy || loading} onClick={load}>{t('Reload purchases and credits', '重新載入購買及堂數')}</button></div>
    {error && <p role="alert" className="error">{error}</p>}{notice && <p role="status" className="success">{notice}</p>}
    {loading ? <p role="status">{t('Loading purchases and credits…', '載入購買及堂數中…')}</p> : loadError ? <p role="alert">{t('Could not load purchases and credits. Reload to retry.', '未能載入購買及堂數，請重新載入。')}</p> : <>
      {!admin && <form className="panel" onSubmit={create}><label htmlFor="purchase-offer">{t('Package offer', '套票選項')}</label><select id="purchase-offer" required disabled={locked || retry || review} value={offerId} onChange={e => setOfferId(e.target.value)}><option value="">{t('Choose a package offer', '選擇套票')}</option>{offers.filter(offer => offer.active).map(offer => <option value={offer.id} key={offer.id}>{offer.name} · {offer.credits} {t('credits', '堂')} · {money(offer.priceMinor)}</option>)}</select>
      {selected && <div className="section-gap"><p>{selected.name} · {selected.credits} {t('credits', '堂')} · {money(selected.priceMinor)}</p><PolicyTerms policy={selected.policyOverride ?? settings.policy} t={t}/><p>{!(selected.policyOverride ?? settings.policy).expiryEnabled ? t('This purchase has no expiry.', '此購買沒有到期日。') : selected.validityDays ? t(`Credits are valid for ${selected.validityDays} days from the payment date (Hong Kong).`, `堂數由付款日期起計 ${selected.validityDays} 日內有效（香港時間）。`) : t('Expiry is assigned individually by an administrator after purchase creation. Payment remains blocked until then.', '購買建立後，由管理員個別指定到期日。在此之前不能付款。')}</p></div>}
      <button disabled={locked || review || (!selected && !retry)}>{retry ? t('Retry same purchase submission', '重試同一次購買提交') : t('Create purchase', '建立購買')}</button>{review && <button type="button" className="outline" disabled={locked} onClick={() => {setReview(false); setError('');}}>{t('I reviewed the current offers', '我已檢查最新套票')}</button>}</form>}
      <PurchaseHistory studentNames={{[studentId]: studentName}} items={purchases} admin={admin} csrfToken={csrfToken} expired={expired} t={t} reload={admin ? async () => {await load(); await reloadCatalogue();} : load} invalidate={invalidate} loading={locked}/><Credits items={credits} t={t}/>
    </>}
  </section>;
}
