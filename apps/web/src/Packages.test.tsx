import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import Packages from './Packages';
import type { components } from './generated/api';
const policy = {expiryEnabled:true,carryForwardEnabled:true,earlyCancellationRetainsCredit:true,lateCancellationRetainsCredit:true,absenceRetainsCredit:true,illnessRetainsCredit:true};
const offer = {id:'offer',name:'Ten lessons',credits:10,priceMinor:123400,currency:'HKD',active:true,version:1,policyOverride:null};
const purchase: components['schemas']['Purchase'] = {id:'purchase',studentId:'amy',createdBy:'parent',offerId:'offer',offerVersion:1,offerName:'Amy frozen purchase',credits:10,priceMinor:123400,currency:'HKD',policy,policyVersion:1,status:'awaiting_expiry',expiresOn:null,expiryAssignedBy:null,expiryAssignedAt:null,version:1,createdAt:'2026-11-18T00:00:00Z',attempts:[],receiptId:null};
const props = {csrfToken:'csrf',expired:vi.fn(() => false),t:(en:string)=>en};
const response = (body:unknown,status=200)=>new Response(JSON.stringify(body),{status});
type Override = (path:string, options?:RequestInit)=>Response | Promise<Response> | undefined;
function mockApi(override?:Override) {
 const mock=vi.fn(async(input:string|URL|Request,options?:RequestInit)=>{
  const path=String(input);const custom=override?.(path,options);if(custom!==undefined)return custom;
  if(path.endsWith('/package-offers'))return response({items:[offer]});
  if(path.endsWith('/settings/credits'))return response({policy,version:1});
  if(path.endsWith('/students'))return response({items:[{id:'amy',name:'Amy',level:'Beginner',version:1},{id:'ben',name:'Ben',level:'Beginner',version:1}]});
  return response({items:[]});
 });vi.stubGlobal('fetch',mock);return mock;
}
async function selectAmy() {const user=userEvent.setup();await user.selectOptions(await screen.findByLabelText('Choose child'),'amy');await screen.findByRole('heading',{name:'Purchase history'});return user;}
beforeEach(()=>{vi.unstubAllGlobals();props.expired.mockClear();});
it('retries uncertain creation with the identical trusted-offer request and disables payment before admin expiry',async()=>{
 let posts=0;const bodies:unknown[]=[];
 mockApi((p,o)=>{if(p.endsWith('/students/amy/purchases')){if(o?.method==='POST'){bodies.push(JSON.parse(String(o.body)));return response(++posts===1?{}:purchase,posts===1?500:201);}return response({items:posts>1?[purchase]:[]});}return undefined;});
 render(<Packages role="parent" {...props}/>);const user=await selectAmy();
 expect(screen.getByText('Simulation only. No money is charged or transferred.')).toBeVisible();
 await user.selectOptions(screen.getByLabelText('Package offer'),'offer');await user.click(screen.getByRole('button',{name:'Create purchase'}));
 await screen.findByRole('button',{name:'Retry same purchase submission'});expect(screen.getByLabelText('Package offer')).toBeDisabled();
 await user.click(screen.getByRole('button',{name:'Retry same purchase submission'}));await screen.findByText('Awaiting administrator expiry');
 expect(bodies).toHaveLength(2);expect(bodies[0]).toEqual(bodies[1]);expect(bodies[0]).toEqual({offerId:'offer',offerVersion:1,idempotencyKey:expect.any(String)});
 expect(screen.queryByRole('button',{name:'Start mock payment'})).not.toBeInTheDocument();expect(screen.getByText('No credit packages yet.')).toBeVisible();
});
it.each([200,401,500])('ignores an obsolete child response %i after switching children',async status=>{
 let resolve!:(r:Response)=>void;const old=new Promise<Response>(r=>{resolve=r;});
 const mock=mockApi(p=>p.endsWith('/students/amy/purchases')?old:undefined);
 render(<Packages role="parent" {...props}/>);const user=userEvent.setup();await user.selectOptions(await screen.findByLabelText('Choose child'),'amy');
 await waitFor(()=>expect(mock.mock.calls.some(([p])=>String(p).endsWith('/students/amy/purchases'))).toBe(true));
 await user.selectOptions(screen.getByLabelText('Choose child'),'ben');await screen.findByText('No purchases yet.');
 await act(async()=>{resolve(response({items:[purchase]},status));});
 expect(screen.queryByText('Amy frozen purchase')).not.toBeInTheDocument();expect(screen.getByLabelText('Choose child')).toHaveValue('ben');expect(props.expired).not.toHaveBeenCalled();
});
it.each([403,404])('clears visible child finance after scope is lost (%i)',async status=>{
 let denied=false;mockApi(p=>p.endsWith('/students/amy/purchases')?response(denied?{}:{items:[purchase]},denied?status:200):undefined);
 render(<Packages role="parent" {...props}/>);const user=await selectAmy();expect(screen.getByText('Amy frozen purchase')).toBeVisible();denied=true;
 await user.click(screen.getByRole('button',{name:'Reload purchases and credits'}));await screen.findByRole('alert');expect(screen.queryByText('Amy frozen purchase')).not.toBeInTheDocument();expect(screen.queryByRole('button',{name:'Create purchase'})).not.toBeInTheDocument();
});
it('freezes a stale-offer submission until current offers are explicitly reviewed',async()=>{
 mockApi((p,o)=>p.endsWith('/students/amy/purchases')&&o?.method==='POST'?response({},409):undefined);
 render(<Packages role="parent" {...props}/>);const user=await selectAmy();await user.selectOptions(screen.getByLabelText('Package offer'),'offer');await user.click(screen.getByRole('button',{name:'Create purchase'}));
 const reviewed=await screen.findByRole('button',{name:'I reviewed the current offers'});await waitFor(()=>expect(reviewed).toBeEnabled());expect(screen.getByRole('button',{name:'Create purchase'})).toBeDisabled();await user.click(reviewed);expect(screen.getByRole('button',{name:'Create purchase'})).toBeEnabled();
});
it('keeps failed attempts visible and explicitly starts a new simulated attempt',async()=>{
 const failed={...purchase,status:'pending_payment',expiresOn:'2035-12-31',attempts:[{id:'failed-attempt',purchaseId:purchase.id,status:'failed',createdAt:purchase.createdAt}]};
 const mock=mockApi((p,o)=>{if(p.endsWith('/students/amy/purchases'))return response({items:[failed]});if(p.endsWith('/payment-attempts')&&o?.method==='POST')return response({},201);return undefined;});
 render(<Packages role="parent" {...props}/>);const user=await selectAmy();expect(screen.getByText('No credit packages yet.')).toBeVisible();
 await user.click(screen.getByRole('button',{name:'Retry mock payment'}));await waitFor(()=>expect(mock.mock.calls.some(([p,o])=>String(p).endsWith('/purchases/purchase/payment-attempts')&&o?.method==='POST')).toBe(true));
 expect(screen.queryByRole('button',{name:'Mark succeeded'})).not.toBeInTheDocument();
});
it('administrator submits individual expiry with the purchase version',async()=>{
 const mock=mockApi((p,o)=>{if(p.includes('/purchases?'))return response({items:[purchase]});if(p.endsWith('/expiry')&&o?.method==='POST')return response({...purchase,status:'pending_payment'},200);return undefined;});
 render(<Packages role="admin" {...props}/>);const user=userEvent.setup();const date=await screen.findByLabelText('Expiry date (Hong Kong)');await user.type(date,'2035-12-31');await user.click(screen.getByRole('button',{name:'Assign expiry'}));
 await waitFor(()=>expect(mock.mock.calls.some(([p,o])=>String(p).endsWith('/purchases/purchase/expiry')&&o?.body===JSON.stringify({expiresOn:'2035-12-31',version:1}))).toBe(true));
});
it('administrator reviews a selected student ledger without guardian checkout controls',async()=>{
 const mock=mockApi();render(<Packages role="admin" {...props}/>);const user=userEvent.setup();await user.selectOptions(await screen.findByLabelText('Choose student for credit history'),'amy');
 await screen.findByRole('heading',{name:'Credits and ledger'});expect(screen.queryByRole('button',{name:'Create purchase'})).not.toBeInTheDocument();expect(mock.mock.calls.some(([p])=>String(p).endsWith('/students/amy/credits'))).toBe(true);
});
it('administrator enters dollars exactly and rejects fractional cents',async()=>{
 const bodies:Record<string,unknown>[]=[];mockApi((p,o)=>{if(p.endsWith('/package-offers')&&o?.method==='POST'){bodies.push(JSON.parse(String(o.body)));return response(offer,201);}return undefined;});
 render(<Packages role="admin" {...props}/>);const user=userEvent.setup();await user.click(await screen.findByRole('button',{name:'Add package offer'}));await user.type(screen.getByLabelText('Package name'),'Decimal offer');const price=screen.getByLabelText('Price (HKD)');await user.clear(price);await user.type(price,'1.001');await user.click(screen.getByRole('button',{name:'Save package offer'}));expect(bodies).toHaveLength(0);
 await user.clear(price);await user.type(price,'123.45');await user.click(screen.getByRole('button',{name:'Save package offer'}));await waitFor(()=>expect(bodies).toHaveLength(1));expect(bodies[0].priceMinor).toBe(12345);
});
it('assigns distinct accessible expiry inputs when a purchase appears in both admin views',async()=>{
 mockApi(p=>p.includes('/purchases?')||p.endsWith('/students/amy/purchases')?response({items:[purchase]}):undefined);render(<Packages role="admin" {...props}/>);const user=userEvent.setup();await user.selectOptions(await screen.findByLabelText('Choose student for credit history'),'amy');
 await waitFor(()=>expect(screen.getAllByLabelText('Expiry date (Hong Kong)')).toHaveLength(2));const inputs=screen.getAllByLabelText('Expiry date (Hong Kong)');expect(inputs[0].id).not.toBe(inputs[1].id);
});
