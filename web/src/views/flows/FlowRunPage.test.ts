import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
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
  ask_name: true,
  name_label: 'What is it?',
  fields: [
    ...(long ? [{ key: 'until', label: 'Leave until', type: 'date' as const, required: false, help: "When you'll take it home" }] : []),
    { key: 'owner', label: 'Owner', type: 'text', default: 'Sam', required: true },
  ],
});

describe('FlowRunPage', () => {
  it('rebuilds the form for the category chosen, and files it', async () => {
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

    await fireEvent.input(screen.getByLabelText('What is it?'), { target: { value: 'Oak' } });
    await fireEvent.input(screen.getByLabelText('Leave until'), { target: { value: '2026-11-06' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    await screen.findByRole('heading', { name: 'Filed' });
    expect(sent.at(-1)!.body).toEqual({ step: 'about', name: 'Oak', fields: { until: '2026-11-06', owner: 'Sam' }, answers: {} });
    expect(screen.getByText('Filed Oak in Long boards.')).toBeTruthy();
    expect((screen.getByAltText('The label for Oak') as HTMLImageElement).src).toContain('/api/v1/assets/a1/label?template_id=t1');
    expect(screen.getByText('WOOD-1')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Start again' })).toBeTruthy();
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
});
