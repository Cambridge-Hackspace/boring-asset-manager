import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import FlowRunPage from './FlowRunPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Prompt, RunView } from '../../lib/flows';

beforeEach(() => {
  session.permissions = ['flows.run'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function view(step: number, prompt: Prompt, extra: Partial<RunView> = {}): RunView {
  return { run: { id: 'r1', flow_id: 'f', step, values: { flow: 'Leave something' }, done: [], finished: prompt.kind === 'done' }, flow: 'leave', prompt, ...extra };
}

const choose: Prompt = {
  step: 'kind',
  kind: 'choose_category',
  title: 'What are you leaving?',
  categories: [
    { id: 'short', name: 'Short boards', color_hex: '#15803d' },
    { id: 'long', name: 'Long boards', color_hex: '#1d4ed8' },
  ],
};
const formFor = (long: boolean): Prompt => ({
  step: 'about',
  kind: 'form',
  title: 'About it',
  questions: [
    { key: 'name', label: 'What is it?', type: 'text', required: true },
    ...(long ? [{ key: 'until', label: 'Leave until', type: 'date' as const, help: "When you'll take it home" }] : []),
    { key: 'owner', label: 'Owner', type: 'text', default: 'Sam', required: true },
  ],
});

describe('FlowRunPage', () => {
  it('asks the form the server gives for the category chosen, and files it', async () => {
    let category = '';
    const { sent } = fakeApi({
      'POST /flows/leave/runs': () => ({ status: 201, body: view(0, choose, { progress: { step: 1, of: 2 } }) }),
      'POST /flows/leave/runs/r1': (body) => {
        const b = body as { step: string; category_id?: string };
        if (b.step === 'kind') {
          category = b.category_id!;
          return view(1, formFor(category === 'long'), { progress: { step: 2, of: 2 } });
        }
        return view(4, {
          step: 'done',
          kind: 'done',
          title: 'Filed',
          done: [{ step: 'file', kind: 'create', said: 'Filed Oak in Long boards.' }],
          asset: { id: 'a1', name: 'Oak', code: 'WOOD-1', categories: ['Long boards'] },
          show_label: true,
          label_template_id: 't1',
        });
      },
    });
    render(FlowRunPage, { params: { slug: 'leave' } });

    expect((await screen.findByRole('heading', { name: 'What are you leaving?' })).ownerDocument.activeElement?.textContent).toContain('What are you leaving?');
    // How far along, as the server counts it (#166).
    expect(screen.getByText(/· Step 1 of 2$/)).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Long boards' }));

    // Long boards ask until, with its help, and the owner, defaulted.
    await screen.findByRole('heading', { name: 'About it' });
    expect(screen.getByText(/· Step 2 of 2$/)).toBeTruthy();
    expect(screen.getByLabelText('Leave until')).toBeTruthy();
    expect(screen.getByText("When you'll take it home")).toBeTruthy();
    expect((screen.getByLabelText(/Owner/) as HTMLInputElement).value).toBe('Sam');

    // The name is required; it says so without asking the server.
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect((await screen.findByRole('alert')).textContent).toContain('What is it? is required.');
    expect(sent.filter((r) => r.method === 'POST')).toHaveLength(2);

    await fireEvent.input(screen.getByLabelText(/What is it?/), { target: { value: 'Oak' } });
    await fireEvent.input(screen.getByLabelText('Leave until'), { target: { value: '2026-11-06' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    await screen.findByRole('heading', { name: 'Filed' });
    expect(sent.at(-1)!.body).toEqual({ step: 'about', answers: { name: 'Oak', until: '2026-11-06', owner: 'Sam' } });
    expect(screen.getByText('Filed Oak in Long boards.')).toBeTruthy();
    expect((screen.getByAltText('The label for Oak') as HTMLImageElement).src).toContain('/api/v1/assets/a1/label?template_id=t1');
    expect(screen.getByText('WOOD-1')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Start again' })).toBeTruthy();
  });

  it('asks each type of question, answers by key, and adds the photo once its asset is filed', async () => {
    const sizes = ['XS', 'S', 'M', 'L', 'XL', '2XL', '3XL', '4XL', '5XL'].map((s) => ({ value: s.toLowerCase(), label: s }));
    const form: Prompt = {
      step: 'about',
      kind: 'form',
      title: 'About it',
      questions: [
        { key: 'note', label: 'Anything else?', type: 'long_text', help: 'Where it came from' },
        { key: 'boards', label: 'How many?', type: 'number', default: '1' },
        { key: 'back', label: 'Back by', type: 'date' },
        { key: 'fragile', label: 'Fragile?', type: 'yes_no', default: 'no' },
        { key: 'wood', label: 'Which wood?', type: 'choice', options: [{ value: 'oak', label: 'Oak' }, { value: 'pine' }] },
        { key: 'size', label: 'Size', type: 'choice', options: sizes },
        { key: 'photo', label: 'A photo', type: 'photo', required: true },
      ],
    };
    const done: RunView = {
      ...view(3, { step: 'done', kind: 'done', title: 'Filed', asset: { id: 'a1', name: 'Oak', categories: [] }, done: [{ step: 'file', kind: 'create', said: 'Filed Oak.' }] }),
    };
    done.run.photos = [{ question: 'photo', asset_id: 'a1' }];
    const { sent } = fakeApi({
      'POST /flows/leave/runs': () => ({ status: 201, body: view(0, form) }),
      'POST /flows/leave/runs/r1': () => done,
      'POST /assets/a1/photos': () => ({ status: 201, body: { photo: { id: 'p1' } } }),
    });
    render(FlowRunPage, { params: { slug: 'leave' } });
    await screen.findByRole('heading', { name: 'About it' });

    expect(screen.getByLabelText('Anything else?').tagName).toBe('TEXTAREA');
    expect(screen.getByText('Where it came from')).toBeTruthy();
    expect((screen.getByLabelText('How many?') as HTMLInputElement).type).toBe('number');
    expect((screen.getByLabelText('How many?') as HTMLInputElement).value).toBe('1');
    expect((screen.getByLabelText('Back by') as HTMLInputElement).type).toBe('date');
    // Yes or no, and a few choices, are buttons to choose; many, a list.
    const fragile = screen.getByRole('group', { name: 'Fragile?' });
    expect((within(fragile).getByRole('radio', { name: 'No' }) as HTMLInputElement).checked).toBe(true);
    expect(within(screen.getByRole('group', { name: 'Which wood?' })).getAllByRole('radio').map((r) => r.parentElement!.textContent!.trim())).toEqual(['Oak', 'pine']);
    expect(screen.getByLabelText('Size').tagName).toBe('SELECT');

    // A required photo is asked for before anything is sent.
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect((await screen.findByRole('alert')).textContent).toContain('A photo is required.');

    await fireEvent.input(screen.getByLabelText('Anything else?'), { target: { value: 'From the skip' } });
    await fireEvent.input(screen.getByLabelText('How many?'), { target: { value: '3' } });
    await fireEvent.input(screen.getByLabelText('Back by'), { target: { value: '2026-11-06' } });
    await fireEvent.click(within(fragile).getByRole('radio', { name: 'Yes' }));
    await fireEvent.click(screen.getByRole('radio', { name: 'Oak' }));
    await fireEvent.change(screen.getByLabelText('Size'), { target: { value: 'xl' } });
    const file = new File(['jpeg'], 'oak.jpg', { type: 'image/jpeg' });
    await fireEvent.change(screen.getByLabelText(/A photo/), { target: { files: [file] } });
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    await screen.findByRole('heading', { name: 'Filed' });
    const answered = sent.find((r) => r.method === 'POST' && r.path.endsWith('/runs/r1'))!.body;
    expect(answered).toEqual({
      step: 'about',
      answers: { note: 'From the skip', boards: '3', back: '2026-11-06', fragile: 'yes', wood: 'oak', size: 'xl', photo: 'oak.jpg' },
    });
    // The photo goes to the asset Set fields put it on, once.
    await vi.waitFor(() => expect(sent.filter((r) => r.path.endsWith('/assets/a1/photos'))).toHaveLength(1));
    const upload = sent.find((r) => r.path.endsWith('/assets/a1/photos'))!.body as FormData;
    expect((upload.get('file') as File).name).toBe('oak.jpg');
  });

  it('finds by a typed code, confirms, and shows a refusal with the run as it is', async () => {
    const find: Prompt = { step: 'find', kind: 'find', title: 'What are you collecting?', find_by: ['scan', 'list'], assets: [{ id: 'a1', name: 'Oak', code: 'WOOD-1', categories: ['Long boards'] }] };
    const confirm: Prompt = { step: 'confirm', kind: 'confirm', asset: { id: 'a1', name: 'Oak', categories: [] }, coming: ['Archive Oak: it is taken off the lists.'] };
    const { sent } = fakeApi({
      'POST /flows/collect/runs': () => ({ status: 201, body: { ...view(0, find), flow: 'collect' } }),
      'POST /flows/collect/runs/r1': (body) => {
        const b = body as { step: string; code?: string };
        if (b.step === 'find' && b.code === 'NOPE') return { status: 400, body: { ...view(0, find), error: 'Nothing has the code NOPE' } };
        if (b.step === 'find') return view(1, confirm);
        return { status: 403, body: { ...view(2, { step: 'finish', kind: 'change', coming: ['Archive Oak: it is taken off the lists.'] }), error: 'Forbidden: insufficient permissions' } };
      },
    });
    render(FlowRunPage, { params: { slug: 'collect' } });
    await screen.findByRole('button', { name: /Oak/ });

    await fireEvent.input(screen.getByLabelText('Its code'), { target: { value: 'NOPE' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Find' }));
    expect((await screen.findByRole('alert')).textContent).toContain('Nothing has the code NOPE');

    // A scanner types a scan link; it is read for its code.
    await fireEvent.input(screen.getByLabelText('Its code'), { target: { value: 'https://bam.example.org/scan/WOOD-1' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Find' }));
    await screen.findByRole('heading', { name: 'Is this right?' });
    expect(sent.at(-1)!.body).toEqual({ step: 'find', code: 'WOOD-1' });
    // What it found, above the question.
    expect(screen.getByText('Oak', { selector: 'span' })).toBeTruthy();
    expect(screen.getByText('Archive Oak: it is taken off the lists.')).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Yes, go ahead' }));
    expect((await screen.findByRole('alert')).textContent).toContain('Forbidden');
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy();
  });

  it('starts from an asset once, when started from its page', async () => {
    history.replaceState(null, '', '/f/collect?asset=a1');
    const confirm: Prompt = { step: 'confirm', kind: 'confirm', asset: { id: 'a1', name: 'Oak', categories: [] }, coming: ['Archive Oak: it is taken off the lists.'] };
    const { sent } = fakeApi({ 'POST /flows/collect/runs': () => ({ status: 201, body: { ...view(1, confirm), flow: 'collect' } }) });
    render(FlowRunPage, { params: { slug: 'collect' } });
    await screen.findByRole('heading', { name: 'Is this right?' });
    expect(sent[0]!.body).toEqual({ asset_id: 'a1' });
    expect(location.search).toBe('');
  });

  it('links to one thing after another, with Add another and Done, or something new (#264)', async () => {
    const link = (linked: { id: string; name: string }[]): Prompt => ({
      step: 'link',
      kind: 'link',
      title: 'Which kit?',
      relation: 'contained in',
      find_by: ['list'],
      repeat: true,
      required: true,
      create_in: 'Kits',
      moves: 'Kit 0',
      linked: linked.map((l) => ({ ...l, categories: [] })),
      assets: [
        { id: 'k1', name: 'Kit 1', categories: ['Kits'] },
        { id: 'k2', name: 'Kit 2', categories: ['Kits'] },
      ].filter((a) => !linked.some((l) => l.id === a.id)),
    });
    const { sent } = fakeApi({
      'POST /flows/kit/runs': () => ({ status: 201, body: view(1, link([])) }),
      'POST /flows/kit/runs/r1': (body) => {
        const b = body as { create?: { name: string }; asset_id?: string; done?: boolean };
        if (b.done) return view(2, { step: 'done', kind: 'done', title: 'Added', done: [{ step: 'link', kind: 'link', said: 'Linked Board: contained in Kit 1.' }] });
        if (b.create) return view(1, link([{ id: 'k1', name: 'Kit 1' }, { id: 'k9', name: b.create.name }]));
        return view(1, link([{ id: 'k1', name: 'Kit 1' }]));
      },
    });
    render(FlowRunPage, { params: { slug: 'kit' } });
    await screen.findByRole('heading', { name: 'Which kit?' });
    expect(screen.getByText('Linking it moves it out of Kit 0.')).toBeTruthy();
    // Required, it offers no skipping; nothing linked, there is no Done.
    expect(screen.queryByRole('button', { name: 'Skip' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Done' })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: /Kit 1/ }));

    // Linked, it says so, and asks whether to add another or be done.
    const linked = await screen.findByRole('list', { name: 'Linked so far' });
    expect(linked.textContent).toContain('contained in Kit 1');
    expect(sent.at(-1)!.body).toEqual({ step: 'link', asset_id: 'k1' });
    expect(screen.queryByRole('button', { name: /Kit 2/ })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Add another' }));

    // Another: something not listed, added by its name.
    await fireEvent.input(screen.getByLabelText('Not listed? Add it to Kits'), { target: { value: 'Kit 9' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add it' }));
    await vi.waitFor(() => expect(within(screen.getByRole('list', { name: 'Linked so far' })).getAllByRole('listitem')).toHaveLength(2));
    expect(sent.at(-1)!.body).toEqual({ step: 'link', create: { name: 'Kit 9' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    await screen.findByRole('heading', { name: 'Added' });
    expect(sent.at(-1)!.body).toEqual({ step: 'link', done: true });
  });

  it('lets a Link that is not required be skipped', async () => {
    const { sent } = fakeApi({
      'POST /flows/kit/runs': () => ({ status: 201, body: view(1, { step: 'link', kind: 'link', title: 'Which kit?', relation: 'needs', find_by: ['scan'] }) }),
      'POST /flows/kit/runs/r1': () => view(2, { step: 'done', kind: 'done', title: 'Added' }),
    });
    render(FlowRunPage, { params: { slug: 'kit' } });
    await screen.findByRole('heading', { name: 'Which kit?' });
    expect(screen.getByLabelText('Its code')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Skip' }));
    await screen.findByRole('heading', { name: 'Added' });
    expect(sent.at(-1)!.body).toEqual({ step: 'link', done: true });
  });
});
