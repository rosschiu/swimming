import {act,render,screen,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach,expect,it,vi} from 'vitest';
import Notifications from './Notifications';
import { isLocalNavigation } from './navigation';
import type { MouseEvent } from 'react';
function notifyNavigation(next: (destination: unknown) => void) {
 return (event: MouseEvent<HTMLAnchorElement>, destination: unknown) => { if (isLocalNavigation(event)) { event.preventDefault(); next(destination); } };
}
const note={id:'notification',studentId:'amy',studentName:'Amy',kind:'progress.created',sourceId:'note',sourceVersion:1,createdAt:'2030-01-01T00:00:00Z',readAt:null};
const preference={emailEnabled:false,emailAddress:'parent@swim.test',emailDeliveryAvailable:false};
const t=(en:string)=>en;const expired=()=>false;
function response(body:unknown,status=200){return new Response(JSON.stringify(body),{status});}
beforeEach(()=>vi.unstubAllGlobals());
it('loads scoped pages, marks read and refreshes authoritative count before opening the current profile',async()=>{
 const onUnreadCount=vi.fn(),onOpenStudent=vi.fn();const calls:string[]=[];
 vi.stubGlobal('fetch',vi.fn(async(path:string,options?:RequestInit)=>{calls.push(path);if(path.endsWith('/notification-preferences'))return response(preference);if(options?.method==='PUT')return response({...note,readAt:'2030-01-02T00:00:00Z'});if(path.endsWith('?limit=1'))return response({items:[],nextCursor:null,unreadCount:1});return response({items:[path.includes('cursor=')?{...note,id:'second',kind:'milestone.achieved'}:note],nextCursor:path.includes('cursor=')?null:'opaque+cursor',unreadCount:2});}));
 const user=userEvent.setup();render(<Notifications csrfToken="csrf" expired={expired} t={t} onUnreadCount={onUnreadCount} follow={notifyNavigation(onOpenStudent)}/>);await screen.findByText('A teaching update was shared.');await user.click(screen.getByRole('button',{name:'Mark read'}));await screen.findByText('Read',{exact:false});expect(onUnreadCount).toHaveBeenLastCalledWith(1);expect(screen.queryByRole('button',{name:'Mark read'})).not.toBeInTheDocument();await user.click(screen.getByRole('link',{name:'Open current student profile'}));expect(onOpenStudent).toHaveBeenCalledWith({ view: 'students', studentId: 'amy' });await user.click(screen.getByRole('button',{name:'Load more'}));await screen.findByText('A milestone was achieved.');expect(screen.getAllByRole('article')).toHaveLength(2);expect(calls.some(path=>path.includes('cursor=opaque%2Bcursor'))).toBe(true);
});
it('sends only the self-service boolean and explains unavailable demo delivery',async()=>{
 const puts:unknown[]=[];vi.stubGlobal('fetch',vi.fn(async(path:string,options?:RequestInit)=>{if(options?.method==='PUT'){puts.push(JSON.parse(options.body as string));return response({...preference,emailEnabled:true});}return response(path.endsWith('/notification-preferences')?preference:{items:[],nextCursor:null,unreadCount:0});}));
 const user=userEvent.setup();render(<Notifications csrfToken="csrf" expired={expired} t={t} onUnreadCount={()=>{}} follow={()=>{}}/>);const checkbox=await screen.findByRole('checkbox',{name:'Email shared teaching updates'});expect(checkbox).not.toBeChecked();expect(screen.getByText(/Demo email delivery is currently unavailable/)).toBeVisible();await user.click(checkbox);expect(checkbox).toBeChecked();expect(puts).toEqual([{emailEnabled:true}]);expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
});
it('preserves the displayed preference on failure and recovers Chinese empty states',async()=>{
 let loads=0;vi.stubGlobal('fetch',vi.fn(async(path:string,options?:RequestInit)=>{if(options?.method==='PUT')return response({error:{message:'failed'}},500);if(path.endsWith('/notification-preferences'))return response(preference);if(++loads===1)return response({error:{message:'failed'}},500);return response({items:[],nextCursor:null,unreadCount:0});}));
 const user=userEvent.setup();render(<Notifications csrfToken="csrf" expired={expired} t={(_en,zh)=>zh} onUnreadCount={()=>{}} follow={()=>{}}/>);await screen.findByRole('alert');await user.click(screen.getByRole('button',{name:'重新載入'}));await screen.findByText('暫無通知。');const checkbox=screen.getByRole('checkbox');await user.click(checkbox);await screen.findByText('未能儲存電郵設定，請重試。');expect(checkbox).not.toBeChecked();
});
it('account-keyed remount aborts stale lists and preferences without updating the next account badge',async()=>{
 const pending:((value:Response)=>void)[]=[];const signals:AbortSignal[]=[];let old=true;const oldCount=vi.fn(),newCount=vi.fn();vi.stubGlobal('fetch',vi.fn((path:string,options:RequestInit)=>{if(old){signals.push(options.signal as AbortSignal);return new Promise<Response>(resolve=>pending.push(resolve));}return Promise.resolve(response(path.endsWith('/notification-preferences')?{...preference,emailAddress:'other-parent@swim.test'}:{items:[],nextCursor:null,unreadCount:0}));}));
 const {rerender}=render(<Notifications key="parent" csrfToken="one" expired={expired} t={t} onUnreadCount={oldCount} follow={()=>{}}/>);old=false;rerender(<Notifications key="other" csrfToken="two" expired={expired} t={t} onUnreadCount={newCount} follow={()=>{}}/>);await screen.findByText('No notifications yet.');expect(signals.every(signal=>signal.aborted)).toBe(true);await act(async()=>{pending[0](response({items:[note],nextCursor:null,unreadCount:9}));pending[1](response(preference));});expect(screen.queryByText('Amy')).not.toBeInTheDocument();expect(oldCount).not.toHaveBeenCalled();expect(newCount).toHaveBeenLastCalledWith(0);expect(within(screen.getByText(/Your login email:/)).queryByText('parent@swim.test')).not.toBeInTheDocument();
});
it('loads 125 tied-time notifications across five pages without dropping or duplicating records',async()=>{
 const items=Array.from({length:125},(_,i)=>({...note,id:`n-${i}`,studentName:`Swimmer ${i}`}));
 const cursors:string[]=[];
 vi.stubGlobal('fetch',vi.fn(async(path:string)=>{if(path.endsWith('/notification-preferences'))return response(preference);const q=new URL(path,'http://test').searchParams;const cursor=q.get('cursor')||'0';cursors.push(cursor);const offset=Number(cursor);return response({items:items.slice(offset,offset+25),nextCursor:offset+25<items.length?String(offset+25):null,unreadCount:125});}));
 const user=userEvent.setup();render(<Notifications csrfToken="csrf" expired={expired} t={t} onUnreadCount={()=>{}} follow={()=>{}}/>);
 await screen.findByText('Swimmer 24');
 for(let page=1;page<5;page++){await user.click(screen.getByRole('button',{name:'Load more'}));await screen.findByText(`Swimmer ${page*25+24}`);}
 expect(screen.getAllByRole('article')).toHaveLength(125);expect(cursors).toEqual(['0','25','50','75','100']);expect(screen.queryByRole('button',{name:'Load more'})).not.toBeInTheDocument();
 for(let i=0;i<125;i++)expect(screen.getAllByText(`Swimmer ${i}`,{exact:true})).toHaveLength(1);
});

it('keeps notification profile links native for modified clicks',async()=>{
 const onOpenStudent=vi.fn();vi.stubGlobal('fetch',vi.fn(async(path:string)=>response(path.endsWith('/notification-preferences')?preference:{items:[note],nextCursor:null,unreadCount:1})));
 render(<Notifications csrfToken="csrf" expired={expired} t={t} onUnreadCount={()=>{}} follow={notifyNavigation(onOpenStudent)}/>);
 const link=await screen.findByRole('link',{name:'Open current student profile'});expect(link).toHaveAttribute('href','/students/amy');
 for(const modifier of ['ctrlKey','metaKey','shiftKey','altKey']){const event=new MouseEvent('click',{bubbles:true,cancelable:true,[modifier]:true});let intercepted=true;document.addEventListener('click',e=>{intercepted=e.defaultPrevented;e.preventDefault();},{once:true});act(()=>{link.dispatchEvent(event);});expect(intercepted).toBe(false);}
 expect(onOpenStudent).not.toHaveBeenCalled();await userEvent.setup().click(link);expect(onOpenStudent).toHaveBeenCalledWith({ view: 'students', studentId: 'amy' });
});
