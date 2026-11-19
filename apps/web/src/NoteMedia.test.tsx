import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { MediaPicker, NoteMediaList, uploadDrafts, type MediaDraft } from './NoteMedia';
import ProgressNotes from './ProgressNotes';

const t = (en: string) => en;
afterEach(() => vi.unstubAllGlobals());
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });

function Picker() {
  const [drafts, setDrafts] = useState<MediaDraft[]>([]);
  return <MediaPicker id="media" drafts={drafts} onChange={setDrafts} disabled={false} t={t}/>;
}

it('adds photos and videos, refuses other files and removes attachments', async () => {
  render(<Picker/>);
  const user = userEvent.setup({ applyAccept: false });
  await user.upload(screen.getByLabelText('Add photos or videos'), [new File(['p'], 'kick.jpg', { type: 'image/jpeg' }), new File(['v'], 'lap.mov', { type: 'video/quicktime' }), new File(['x'], 'notes.pdf', { type: 'application/pdf' })]);
  expect(screen.getByRole('alert')).toHaveTextContent('notes.pdf is not a photo, video or recording.');
  expect(screen.getByRole('list', { name: 'Attachments' })).toHaveTextContent('Photo · kick.jpg');
  expect(screen.getByRole('list', { name: 'Attachments' })).toHaveTextContent('Video · lap.mov');
  await user.click(screen.getByRole('button', { name: 'Remove kick.jpg' }));
  expect(screen.queryByText(/kick\.jpg/)).not.toBeInTheDocument();
  await user.upload(screen.getByLabelText('Add photos or videos'), Array.from({ length: 6 }, (_, i) => new File(['p'], `p${i}.png`, { type: 'image/png' })));
  expect(screen.getByRole('alert')).toHaveTextContent('at most 6 attachments');
  expect(screen.getAllByRole('button', { name: /^Remove / })).toHaveLength(6);
});

it('uploads each draft once with its own type', async () => {
  const upload = vi.fn(async () => ({ id: 'new', kind: 'audio' as const, contentType: 'audio/webm' as const, sizeBytes: 1 }));
  const drafts: MediaDraft[] = [{ key: 'a', blob: new Blob(['x'], { type: 'image/jpeg' }), kind: 'image', label: 'a', uploadedId: 'done' }, { key: 'b', blob: new Blob(['y'], { type: 'audio/webm;codecs=opus' }), kind: 'audio', label: 'b' }];
  const saved = vi.fn();
  expect(await uploadDrafts(upload, 'amy', 'csrf', drafts, saved)).toEqual(['done', 'new']);
  expect(upload).toHaveBeenCalledOnce();
  expect(upload).toHaveBeenCalledWith('/students/amy/note-media', expect.objectContaining({ headers: { 'X-CSRF-Token': 'csrf', 'Content-Type': 'audio/webm;codecs=opus' } }));
  expect(saved).toHaveBeenCalledWith('b', 'new');
});

it('shows photos, recordings and videos to readers', () => {
  const { container } = render(<NoteMediaList t={t} media={[
    { id: 'i', kind: 'image', contentType: 'image/jpeg', sizeBytes: 1, url: '/api/v1/i' },
    { id: 'a', kind: 'audio', contentType: 'audio/mp4', sizeBytes: 1, url: '/api/v1/a' },
    { id: 'v', kind: 'video', contentType: 'video/mp4', sizeBytes: 1, url: '/api/v1/v' }]}/>);
  expect(screen.getByRole('img', { name: 'Photo from the lesson' })).toHaveAttribute('src', '/api/v1/i');
  expect(container.querySelector('audio')).toHaveAttribute('src', '/api/v1/a');
  expect(container.querySelector('video')).toHaveAttribute('src', '/api/v1/v');
});

it('shares a note that is only a photo', async () => {
  const fetch = vi.fn(async (path: string, options?: RequestInit) => path.endsWith('/note-media') ? json({ id: 'm1', kind: 'image', contentType: 'image/jpeg', sizeBytes: 1 }, 201)
    : options?.method === 'POST' ? json({ id: 'n', studentId: 'amy', authorId: 'coach', authorName: 'Coach', body: '', createdAt: '2030-01-01T00:00:00Z', updatedAt: '2030-01-01T00:00:00Z', updatedById: 'coach', updatedByName: 'Coach', version: 1, editable: true, media: [{ id: 'm1', kind: 'image', contentType: 'image/jpeg', sizeBytes: 1, url: '/api/v1/m1' }] }, 201)
    : json({ items: [] }));
  vi.stubGlobal('fetch', fetch);
  render(<ProgressNotes studentId="amy" role="coach" csrfToken="csrf" expired={() => false} t={t}/>);
  const user = userEvent.setup();
  await screen.findByText('No progress notes yet.');
  await user.upload(screen.getByLabelText('Add photos or videos'), new File(['p'], 'kick.jpg', { type: 'image/jpeg' }));
  await user.click(screen.getByRole('button', { name: 'Share progress note' }));
  expect(await screen.findByText('Progress note shared.')).toBeVisible();
  const post = fetch.mock.calls.find(([path, options]) => path === '/api/v1/students/amy/progress-notes' && options?.method === 'POST')!;
  expect(JSON.parse(post[1]!.body as string)).toMatchObject({ body: '', mediaIds: ['m1'] });
  expect(screen.getByRole('img', { name: 'Photo from the lesson' })).toBeVisible();
});
