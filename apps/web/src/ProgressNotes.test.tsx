import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import ProgressNotes from './ProgressNotes';
const note = {id:'note',studentId:'amy',authorId:'coach',authorName:'Coach',body:'Original feedback',createdAt:'2030-01-01T00:00:00Z',updatedAt:'2030-01-01T00:00:00Z',updatedById:'coach',updatedByName:'Coach',version:1,editable:true};
const t=(en:string)=>en;
const expired=()=>false;
function response(body:unknown,status=200){return new Response(JSON.stringify(body),{status});}
beforeEach(()=>vi.unstubAllGlobals());
it('retries an uncertain creation verbatim and preserves Unicode text',async()=>{
 const posts:string[]=[];
 vi.stubGlobal('fetch',vi.fn(async (_path:string,options?:RequestInit)=>{
  if(options?.method==='POST'){posts.push(options.body as string);return posts.length===1?response({error:{code:'failure',message:'failed'}},500):response({...note,body:'泳 🏊'},200);}
  return response({items:[]});
 }));
 const user=userEvent.setup();render(<ProgressNotes studentId="amy" role="coach" csrfToken="csrf" expired={expired} t={t}/>);
 await screen.findByText('No progress notes yet.');await user.type(screen.getByLabelText('Progress note'),'泳 🏊');await user.click(screen.getByRole('button',{name:'Share progress note'}));
 expect(await screen.findByRole('alert')).toHaveTextContent('Your text is preserved');expect(screen.getByLabelText('Progress note')).toHaveValue('泳 🏊');expect(screen.getByLabelText('Progress note')).toBeDisabled();
 await user.click(screen.getByRole('button',{name:'Retry same submission'}));await screen.findByText('Progress note shared.');
 expect(posts).toHaveLength(2);expect(posts[0]).toBe(posts[1]);expect(JSON.parse(posts[0])).toMatchObject({body:'泳 🏊',requestId:expect.any(String)});expect(screen.getByLabelText('Progress note')).toHaveValue('');
});
it('requires explicit review after stale correction while preserving body and reason',async()=>{
 const patches:unknown[]=[];let version=1;
 vi.stubGlobal('fetch',vi.fn(async (_path:string,options?:RequestInit)=>{
  if(options?.method==='PATCH'){patches.push(JSON.parse(options.body as string));if(patches.length===1){version=2;return response({error:{code:'version_conflict',message:'changed'}},409);}return response({...note,body:'My correction',version:3});}
  return response({items:[{...note,version,body:version===1?note.body:'Concurrent correction'}]});
 }));
 const user=userEvent.setup();render(<ProgressNotes studentId="amy" role="coach" csrfToken="csrf" expired={expired} t={t}/>);
 await user.click(await screen.findByRole('button',{name:'Correct note'}));await user.clear(screen.getByLabelText('Corrected note'));await user.type(screen.getByLabelText('Corrected note'),'My correction');await user.type(screen.getByLabelText('Correction reason'),'Distance typo');await user.click(screen.getByRole('button',{name:'Share correction'}));
 await screen.findByText('Concurrent correction');expect(screen.getByLabelText('Corrected note')).toHaveValue('My correction');expect(screen.getByLabelText('Correction reason')).toHaveValue('Distance typo');expect(screen.getByRole('button',{name:'Share correction'})).toBeDisabled();
 await user.click(screen.getByRole('button',{name:'Use current version; keep my draft'}));await user.click(screen.getByRole('button',{name:'Share correction'}));await screen.findByText('Correction saved and shared.');expect(patches).toEqual([{body:'My correction',reason:'Distance typo',version:1},{body:'My correction',reason:'Distance typo',version:2}]);
});
it('shows bilingual loading, empty and recoverable failure states without family write controls',async()=>{
 let finish!:(value:Response)=>void;let calls=0;
 vi.stubGlobal('fetch',vi.fn(()=>{calls++;return calls===1?new Promise<Response>(resolve=>{finish=resolve;}):Promise.resolve(response({items:[]}));}));
 const user=userEvent.setup();render(<ProgressNotes studentId="amy" role="parent" csrfToken="csrf" expired={expired} t={(_en,zh)=>zh}/>);
 expect(screen.getByRole('status')).toHaveTextContent('載入進度紀錄中');finish(response({error:{code:'failure',message:'failed'}},500));
 expect(await screen.findByRole('alert')).toHaveTextContent('未能載入進度紀錄');await user.click(screen.getByRole('button',{name:'重試載入紀錄'}));await screen.findByText('暫無進度紀錄。');expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
});
it('account/student keyed remount aborts old loads and clears private drafts',async()=>{
 let oldSignal:AbortSignal|undefined;let finish!:(value:Response)=>void;
 vi.stubGlobal('fetch',vi.fn((path:string,options:RequestInit)=>{
  if(path.includes('/amy/')){oldSignal=options.signal as AbortSignal;return new Promise<Response>(resolve=>{finish=resolve;});}return Promise.resolve(response({items:[]}));
 }));
 const user=userEvent.setup();const {rerender}=render(<ProgressNotes key="coach:amy" studentId="amy" role="coach" csrfToken="one" expired={expired} t={t}/>);
 await user.type(screen.getByLabelText('Progress note'),'Private unsent Amy draft');
 rerender(<ProgressNotes key="admin:ben" studentId="ben" role="admin" csrfToken="two" expired={expired} t={t}/>);
 await screen.findByText('No progress notes yet.');expect(oldSignal?.aborted).toBe(true);expect(screen.getByLabelText('Progress note')).toHaveValue('');finish(response({items:[note]}));expect(screen.queryByText(note.body)).not.toBeInTheDocument();
});
