import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, expect, it, vi } from 'vitest';
import AvailabilityClasses from './AvailabilityClasses';
const slot = { id: 'slot', coachId: 'coach', coachName: 'Alex', venueId: 'pool', venueName: 'Pool', startsAt: '2030-01-07T05:00:00Z', endsAt: '2030-01-07T07:00:00Z', capacity: 6, status: 'submitted' as const, version: 1, editable: true };
const t = (en: string) => en;
function response(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status }); }
function setup(failure = 0) {
  const posts: string[] = [];
  let fail = failure;
  const fetch = vi.fn(async (path: string, options?: RequestInit) => {
    if (options?.method === 'POST') {
      posts.push(options.body as string);
      if (fail) { const code = fail; fail = 0; return response({error:{code:'failure',message:'Changed'}}, code); }
      return response({ id: 'class' }, 201);
    }
    if (path.endsWith('/class-types')) return response({items:[{id:'kind',name:'Beginner',active:true}]});
    if (path.endsWith('/classes')) return response({items:[]});
    return response(slot);
  });
  vi.stubGlobal('fetch', fetch);
  return { posts, fetch };
}
beforeEach(() => vi.unstubAllGlobals());
it('preserves the same submission after an uncertain response and blocks edits until retry', async () => {
  const {posts} = setup(500); const user = userEvent.setup();
  render(<AvailabilityClasses slot={slot} admin close={() => {}} csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('option', {name:'Beginner'});
  await user.selectOptions(screen.getByLabelText('Lesson type'), 'kind');
  await user.click(screen.getByRole('button', {name:'Schedule lesson'}));
  expect(await screen.findByRole('alert')).toHaveTextContent('result is uncertain');
  expect(screen.getByLabelText('Lesson capacity')).toBeDisabled();
  expect(screen.getByRole('button',{name:'Close lessons'})).toBeDisabled();
  await user.click(screen.getByRole('button',{name:'Retry same submission'}));
  await screen.findByText(/Lesson scheduled/);
  expect(posts).toHaveLength(2); expect(posts[0]).toBe(posts[1]);
  expect(JSON.parse(posts[0])).toMatchObject({availabilityVersion:1,startsAt:'2030-01-07T13:00:00+08:00',endsAt:'2030-01-07T15:00:00+08:00',capacity:6});
  expect(screen.getByLabelText('Lesson capacity')).toBeEnabled();
});
it('preserves fields on a definitive conflict and refreshes the window for correction', async () => {
  setup(409); const user=userEvent.setup();
  render(<AvailabilityClasses slot={slot} admin close={() => {}} csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByRole('option',{name:'Beginner'});
  await user.selectOptions(screen.getByLabelText('Lesson type'),'kind');
  await user.clear(screen.getByLabelText('Lesson capacity')); await user.type(screen.getByLabelText('Lesson capacity'),'8');
  await user.click(screen.getByRole('button',{name:'Schedule lesson'}));
  expect(await screen.findByRole('alert')).toHaveTextContent('entries are preserved');
  expect(screen.getByLabelText('Lesson capacity')).toHaveValue(8);
  await user.click(screen.getByRole('button',{name:'Refresh window'}));
  expect(screen.getByLabelText('Lesson capacity')).toHaveValue(8);
});
it('lets coaches read source history without scheduling controls', async () => {
  const {fetch}=setup();
  render(<AvailabilityClasses slot={slot} admin={false} close={() => {}} csrfToken="csrf" expired={() => false} t={t}/>);
  await screen.findByText('No scheduled lessons to show.');
  expect(screen.getByRole('heading', {name:'Lessons from this window'})).toHaveFocus();
  expect(screen.queryByRole('button',{name:'Schedule lesson'})).not.toBeInTheDocument();
  expect(fetch.mock.calls.some(([path])=>path.endsWith('/class-types'))).toBe(false);
});
