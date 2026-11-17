import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import StudentMilestones from './StudentMilestones';
const skill={id:'skill',name:'Float',description:'Ten seconds',active:true,version:1,createdAt:'2030-01-01T00:00:00Z',updatedAt:'2030-01-01T00:00:00Z'};
const achieved={id:'first',studentId:'amy',skillId:'skill',state:'achieved',reason:'Assessed',version:1,skillVersion:1,skillName:'Float',skillDescription:'Ten seconds',actorId:'coach',actorName:'Coach',recordedAt:'2030-01-01T00:00:00Z',reversesEventId:null};
const item={skill,current:null,history:[],canAchieve:true,canRevoke:false};
const t=(en:string)=>en;
const expired=()=>false;
function response(body:unknown,status=200){return new Response(JSON.stringify(body),{status});}
beforeEach(()=>vi.unstubAllGlobals());
it('retries verbatim after uncertain achievement and reloads current state instead of trusting old replay',async()=>{
 const posts:string[]=[];let reads=0;const revoked={...achieved,id:'second',state:'revoked',reason:'Later correction',version:2,reversesEventId:'first'};
 vi.stubGlobal('fetch',vi.fn(async(_path:string,options?:RequestInit)=>{
  if(options?.method==='POST'){posts.push(options.body as string);return posts.length===1?response({error:{code:'failure',message:'failed'}},500):response(achieved);}
  reads++;return response({items:[reads===1?item:{...item,current:revoked,history:[revoked,achieved]}]});
 }));
 const user=userEvent.setup();render(<StudentMilestones studentId="amy" role="coach" csrfToken="csrf" expired={expired} t={t}/>);
 await user.click(await screen.findByRole('button',{name:'Record achievement'}));await user.type(screen.getByLabelText('Achievement comment'),'泳 🏊');await user.click(screen.getByRole('button',{name:'Share achievement'}));await screen.findByRole('alert');expect(screen.getByLabelText('Achievement comment')).toBeDisabled();expect(screen.getByRole('button',{name:'Cancel'})).toBeDisabled();
 await user.click(screen.getByRole('button',{name:'Retry same submission'}));await screen.findByText('Milestone recorded and shared.');expect(posts).toHaveLength(2);expect(posts[0]).toBe(posts[1]);expect(JSON.parse(posts[0])).toMatchObject({state:'achieved',version:0,skillVersion:1,reason:'泳 🏊',requestId:expect.any(String)});expect(reads).toBe(2);expect(screen.getByRole('button',{name:'Record achievement'})).toBeEnabled();expect(screen.queryByRole('button',{name:'Revoke achievement'})).not.toBeInTheDocument();expect(screen.getAllByText('Revoked').length).toBeGreaterThan(0);
});
it('preserves the comment but requires explicit review and a new request key for stale definitions',async()=>{
 const posts:Record<string,unknown>[]=[];let version=1;
 vi.stubGlobal('fetch',vi.fn(async(_path:string,options?:RequestInit)=>{
  if(options?.method==='POST'){posts.push(JSON.parse(options.body as string));if(posts.length===1){version=2;return response({error:{code:'skill_version_conflict',message:'changed'}},409);}return response(achieved,201);}
  return response({items:[{...item,skill:{...skill,version,description:version===1?'Ten seconds':'Twenty seconds'}}]});
 }));
 const user=userEvent.setup();render(<StudentMilestones studentId="amy" role="coach" csrfToken="csrf" expired={expired} t={t}/>);
 await user.click(await screen.findByRole('button',{name:'Record achievement'}));await user.type(screen.getByLabelText('Achievement comment'),'My assessment');await user.click(screen.getByRole('button',{name:'Share achievement'}));await screen.findByText('Twenty seconds');expect(screen.getByLabelText('Achievement comment')).toHaveValue('My assessment');expect(screen.getByRole('button',{name:'Share achievement'})).toBeDisabled();
 await user.click(screen.getByRole('button',{name:'Use current state and definition; keep my draft'}));await user.click(screen.getByRole('button',{name:'Share achievement'}));await screen.findByText('Milestone recorded and shared.');expect(posts[0]).toMatchObject({version:0,skillVersion:1,reason:'My assessment'});expect(posts[1]).toMatchObject({version:0,skillVersion:2,reason:'My assessment'});expect(posts[0].requestId).not.toBe(posts[1].requestId);
});
it('cannot approve a stale revocation when a different coach owns the new achievement',async()=>{
 let canRevoke=true;
 vi.stubGlobal('fetch',vi.fn(async(_path:string,options?:RequestInit)=>{
  if(options?.method==='POST'){canRevoke=false;return response({error:{code:'version_conflict',message:'changed'}},409);}
  return response({items:[{...item,current:{...achieved,version:canRevoke?1:3,actorId:canRevoke?'coach':'coach2'},history:[achieved],canAchieve:false,canRevoke}]});
 }));
 const user=userEvent.setup();render(<StudentMilestones studentId="amy" role="coach" csrfToken="csrf" expired={expired} t={t}/>);await user.click(await screen.findByRole('button',{name:'Revoke achievement'}));expect(screen.getByLabelText('Revocation reason')).toBeRequired();await user.type(screen.getByLabelText('Revocation reason'),'Wrong distance');await user.click(screen.getByRole('button',{name:'Share revocation'}));await screen.findByRole('alert');expect(screen.getByRole('button',{name:'Use current state and definition; keep my draft'})).toBeDisabled();expect(screen.getByRole('button',{name:'Share revocation'})).toBeDisabled();expect(screen.getByLabelText('Revocation reason')).toHaveValue('Wrong distance');
});
it('shows Chinese loading, failure and empty states without family write controls',async()=>{
 let finish!:(value:Response)=>void;let calls=0;vi.stubGlobal('fetch',vi.fn(()=>++calls===1?new Promise<Response>(resolve=>{finish=resolve;}):Promise.resolve(response({items:[]}))));
 const user=userEvent.setup();render(<StudentMilestones studentId="amy" role="parent" csrfToken="csrf" expired={expired} t={(_en,zh)=>zh}/>);expect(screen.getByRole('status')).toHaveTextContent('載入技能進度中');finish(response({error:{code:'failure',message:'failed'}},500));await screen.findByRole('alert');await user.click(screen.getByRole('button',{name:'重試載入技能進度'}));await screen.findByText('暫無可用技能。');expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
});
it('account/student keyed remount aborts old loads and discards drafts and late responses',async()=>{
 let finish!:(value:Response)=>void;let oldSignal:AbortSignal|undefined;let amyCalls=0;
 vi.stubGlobal('fetch',vi.fn(async(path:string,options:RequestInit)=>{
  if(path.includes('/amy/')&&++amyCalls===2){oldSignal=options.signal as AbortSignal;return new Promise<Response>(resolve=>{finish=resolve;});}
  return response({items:[item]});
 }));
 const user=userEvent.setup();const {rerender}=render(<StudentMilestones key="coach:amy" studentId="amy" role="coach" csrfToken="one" expired={expired} t={t}/>);await user.click(await screen.findByRole('button',{name:'Record achievement'}));await user.type(screen.getByLabelText('Achievement comment'),'Amy only draft');await user.click(screen.getByRole('button',{name:'Reload milestones'}));
 rerender(<StudentMilestones key="admin:ben" studentId="ben" role="admin" csrfToken="two" expired={expired} t={t}/>);await user.click(await screen.findByRole('button',{name:'Record achievement'}));expect(screen.getByLabelText('Achievement comment')).toHaveValue('');expect(oldSignal?.aborted).toBe(true);await act(async()=>finish(response({items:[{...item,skill:{...skill,name:'Amy private skill'}}]})));expect(screen.queryByText('Amy private skill')).not.toBeInTheDocument();
});
