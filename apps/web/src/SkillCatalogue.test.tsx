import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import SkillCatalogue from './SkillCatalogue';
const skill={id:'skill',name:'Float',description:'Ten seconds',active:true,version:1,createdAt:'2030-01-01T00:00:00Z',updatedAt:'2030-01-01T00:00:00Z'};
const t=(en:string)=>en;
const expired=()=>false;
function response(body:unknown,status=200){return new Response(JSON.stringify(body),{status});}
beforeEach(()=>vi.unstubAllGlobals());
it('locks an uncertain create and retries the identical Unicode submission',async()=>{
 const posts:string[]=[];
 vi.stubGlobal('fetch',vi.fn(async(_path:string,options?:RequestInit)=>{
  if(options?.method==='POST'){posts.push(options.body as string);return posts.length===1?response({error:{code:'failure',message:'failed'}},500):response(skill);}
  return response({items:[]});
 }));
 const user=userEvent.setup();render(<SkillCatalogue csrfToken="csrf" expired={expired} t={t}/>);
 await user.click(await screen.findByRole('button',{name:'Add skill'}));await user.type(screen.getByLabelText('Skill name'),'浮泳 🏊');await user.type(screen.getByLabelText('Skill description'),'10 秒');await user.click(screen.getByRole('button',{name:'Save skill'}));
 await screen.findByRole('alert');expect(screen.getByLabelText('Skill name')).toBeDisabled();expect(screen.getByLabelText('Skill description')).toHaveValue('10 秒');expect(screen.getByRole('button',{name:'Cancel'})).toBeDisabled();
 await user.click(screen.getByRole('button',{name:'Retry same submission'}));await screen.findByText('Skill saved.');expect(posts).toHaveLength(2);expect(posts[0]).toBe(posts[1]);expect(JSON.parse(posts[0])).toMatchObject({name:'浮泳 🏊',description:'10 秒',requestId:expect.any(String)});
});
it('requires explicit catalogue review after a stale edit and preserves the archive draft',async()=>{
 const patches:unknown[]=[];let version=1;
 vi.stubGlobal('fetch',vi.fn(async(_path:string,options?:RequestInit)=>{
  if(options?.method==='PATCH'){patches.push(JSON.parse(options.body as string));if(patches.length===1){version=2;return response({error:{code:'version_conflict',message:'changed'}},409);}return response({...skill,version:3});}
  return response({items:[{...skill,version,name:version===1?'Float':'Updated float'}]});
 }));
 const user=userEvent.setup();render(<SkillCatalogue csrfToken="csrf" expired={expired} t={t}/>);
 await user.click(await screen.findByRole('button',{name:'Edit skill Float'}));await user.clear(screen.getByLabelText('Skill name'));await user.type(screen.getByLabelText('Skill name'),'My float');await user.type(screen.getByLabelText('Change reason'),'Retiring definition');await user.click(screen.getByLabelText('Available for new achievements'));await user.click(screen.getByRole('button',{name:'Save skill'}));
 await screen.findByText('Updated float');expect(screen.getByLabelText('Skill name')).toHaveValue('My float');expect(screen.getByLabelText('Change reason')).toHaveValue('Retiring definition');expect(screen.getByLabelText('Available for new achievements')).not.toBeChecked();expect(screen.getByRole('button',{name:'Save skill'})).toBeDisabled();
 await user.click(screen.getByRole('button',{name:'Use current version; keep my draft'}));await user.click(screen.getByRole('button',{name:'Save skill'}));await screen.findByText('Skill saved.');expect(patches).toEqual([{name:'My float',description:'Ten seconds',active:false,version:1,reason:'Retiring definition'},{name:'My float',description:'Ten seconds',active:false,version:2,reason:'Retiring definition'}]);
});
it('offers Chinese loading, recoverable failure and empty states',async()=>{
 let finish!:(value:Response)=>void;let calls=0;
 vi.stubGlobal('fetch',vi.fn(()=>++calls===1?new Promise<Response>(resolve=>{finish=resolve;}):Promise.resolve(response({items:[]}))));
 const user=userEvent.setup();render(<SkillCatalogue csrfToken="csrf" expired={expired} t={(_en,zh)=>zh}/>);expect(screen.getByRole('status')).toHaveTextContent('載入技能中');finish(response({error:{code:'failure',message:'failed'}},500));await screen.findByRole('alert');await user.click(screen.getByRole('button',{name:'重試載入技能'}));await screen.findByText('暫無技能。');
});
