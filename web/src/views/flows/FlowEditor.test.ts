import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import FlowEditor from './FlowEditor.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Flow } from '../../lib/flows';

beforeEach(() => {
  session.permissions = ['flows.manage', 'flows.run', 'assets.read'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const flow: Flow = {
  id: 'f1',
  slug: 'leave',
  name: 'Leave something',
  enabled: true,
  created_at: '',
  updated_at: '',
  definition: {
    version: 1,
    category_ids: ['short', 'long'],
    steps: [
      { id: 'kind', kind: 'choose_category', title: 'What are you leaving?' },
      { id: 'about', kind: 'form', title: 'About it', ask_name: true, name_label: 'What is it?' },
      { id: 'file', kind: 'create', name: '{name}' },
      { id: 'done', kind: 'done', title: 'Filed' },
    ],
  },
};

function api(save: (body: unknown) => unknown, f: Flow = flow) {
  return fakeApi({
    'GET /categories': () => ({
      categories: [
        { id: 'short', name: 'Short boards', color_hex: '#15803d' },
        { id: 'long', name: 'Long boards', color_hex: '#1d4ed8' },
      ],
    }),
    'GET /flows': () => ({ flows: [f] }),
    'GET /flows/leave': () => ({ flow: f }),
    'GET /categories/short/fields': () => ({ fields: [{ field: { id: 'o', key: 'owner', label: 'Owner', validation_type: 'text' }, is_required: true }] }),
    'GET /categories/long/fields': () => ({
      fields: [
        { field: { id: 'o', key: 'owner', label: 'Owner', validation_type: 'text' }, is_required: true },
        { field: { id: 'u', key: 'until', label: 'Until', validation_type: 'date' }, is_required: false },
      ],
    }),
    'PUT /flows/leave': save,
  });
}

describe('FlowEditor', () => {
  it('opens one step at a time, previews it, and says what stops saving beside its step', async () => {
    const { sent } = api(() => ({ status: 400, body: { error: 'The flow cannot be saved', problems: [{ step: 'file', message: 'its steps use assets.write, which you do not hold' }] } }));
    render(FlowEditor, { params: { slug: 'leave' } });
    const steps = await screen.findByRole('list', { name: 'Steps' });
    expect(within(steps).getAllByRole('listitem')).toHaveLength(4);

    // The first step starts open, and only it shows its settings.
    expect(screen.getByRole('button', { name: '1. Choose category: What are you leaving?' }).getAttribute('aria-expanded')).toBe('true');
    expect(within(steps).getAllByLabelText('Heading')).toHaveLength(1);
    // A step's ID is the API's, not something to type (#171).
    expect(within(steps).queryByLabelText('Name')).toBeNull();

    // The form lists the union of the categories' fields; its preview
    // shows them as a phone would.
    await fireEvent.click(screen.getByRole('button', { name: '2. Form: About it' }));
    expect(screen.getByRole('button', { name: '1. Choose category: What are you leaving?' }).getAttribute('aria-expanded')).toBe('false');
    // Counted as a run counts them: Create and Done ask nothing.
    expect(screen.getByText('Preview · step 2 of 2')).toBeTruthy();
    const preview = screen.getByRole('figure', { name: 'Preview, as a phone shows it' });
    await vi.waitFor(() => expect(preview.textContent).toContain('Until'));
    expect(preview.textContent).toContain('What is it?');
    await fireEvent.input(screen.getByLabelText('Label for Until'), { target: { value: 'Leave until' } });
    expect(preview.textContent).toContain('Leave until');

    // The server's problems show beside the step they name, by its ID.
    await fireEvent.click(screen.getByRole('button', { name: '3. Create' }));
    expect(screen.getByText('Preview · asks nothing, so a run does it and moves on')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Save flow' }));
    const problem = await screen.findByText('its steps use assets.write, which you do not hold');
    expect(problem.closest('li')!.textContent).toContain('3. Create');
    const body = sent.find((r) => r.method === 'PUT')!.body as { definition: Flow['definition'] };
    expect(body.definition.steps[1]!.fields).toEqual([{ key: 'until', label: 'Leave until' }]);
    // The steps keep their IDs, so runs under way still match.
    expect(body.definition.steps.map((s) => s.id)).toEqual(['kind', 'about', 'file', 'done']);
  });

  it('adds a step before Done, and moves steps but not Done', async () => {
    api(() => ({ flow }));
    render(FlowEditor, { params: { slug: 'leave' } });
    const steps = await screen.findByRole('list', { name: 'Steps' });
    await fireEvent.change(screen.getByLabelText('Add a step'), { target: { value: 'confirm' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    const headings = () => within(steps).getAllByRole('heading').map((h) => h.textContent?.trim());
    expect(headings().at(-2)).toBe('4. Confirm: Is this right?');
    expect(headings().at(-1)).toBe('5. Done: Filed');
    expect((screen.getByRole('button', { name: 'Move step 4 down' }) as HTMLButtonElement).disabled).toBe(true);
    await fireEvent.click(screen.getByRole('button', { name: 'Move step 4 up' }));
    expect(headings()[2]).toBe('3. Confirm: Is this right?');
    // The step added opens, and stays open as it moves.
    expect(screen.getByRole('button', { name: '3. Confirm: Is this right?' }).getAttribute('aria-expanded')).toBe('true');
    // Moving another step past it keeps it open too.
    await fireEvent.click(screen.getByRole('button', { name: 'Move step 2 down' }));
    expect(screen.getByRole('button', { name: '2. Confirm: Is this right?' }).getAttribute('aria-expanded')).toBe('true');
  });

  it("makes a question's key from its text, and warns before renaming one a later step uses (#171)", async () => {
    const asked: Flow = {
      ...flow,
      definition: {
        ...flow.definition,
        steps: [
          flow.definition.steps[0]!,
          { id: 'about', kind: 'form', title: 'About it', questions: [{ key: 'what', label: 'What is it?' }] },
          { id: 'file', kind: 'create', name: '{owner}: {what}' },
          { id: 'done', kind: 'done', title: 'Filed', text: 'Filed {what}.' },
        ],
      },
    };
    const { sent } = api(() => ({ flow: asked }), asked);
    render(FlowEditor, { params: { slug: 'leave' } });
    await fireEvent.click(await screen.findByRole('button', { name: '2. Form: About it' }));
    const first = screen.getByRole('group', { name: 'Question 1' });
    // The question comes first, its key under it; a key it came with stays
    // as it is when the question is reworded.
    expect(within(first).getByText('{what}')).toBeTruthy();
    await fireEvent.input(within(first).getByRole('textbox', { name: 'Question 1' }), { target: { value: 'What are you leaving?' } });
    expect(within(first).getByText('{what}')).toBeTruthy();
    expect(within(first).queryByRole('note')).toBeNull();
    // Warned on pressing Change, before anything is typed.
    await fireEvent.click(within(first).getByRole('button', { name: 'Change key of question 1' }));
    expect(within(first).getByRole('note').textContent).toBe('Step 3 (Create) and step 4 (Done: Filed) use {what}: change it there too, or they will name nothing.');
    await fireEvent.input(within(first).getByLabelText('Key of question 1'), { target: { value: 'thing' } });

    // A new question's key follows its text, until it is changed.
    await fireEvent.click(screen.getByRole('button', { name: 'Add a question' }));
    const second = screen.getByRole('group', { name: 'Question 2' });
    await fireEvent.input(within(second).getByRole('textbox', { name: 'Question 2' }), { target: { value: 'Anything else?' } });
    expect(within(second).getByText('{anything_else}')).toBeTruthy();
    expect(within(second).queryByRole('note')).toBeNull();
    // Removing the first leaves the second as it was: still following.
    await fireEvent.click(within(first).getByRole('button', { name: 'Remove question 1' }));
    const left = screen.getByRole('group', { name: 'Question 1' });
    await fireEvent.input(within(left).getByRole('textbox', { name: 'Question 1' }), { target: { value: 'Anything more?' } });
    expect(within(left).getByText('{anything_more}')).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Save flow' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    const body = sent.find((r) => r.method === 'PUT')!.body as { definition: Flow['definition'] };
    expect(body.definition.steps[1]!.questions).toEqual([{ key: 'anything_more', label: 'Anything more?' }]);
  });

  it('chooses its categories with the picker, and saves them', async () => {
    const { sent } = api(() => ({ flow }));
    render(FlowEditor, { params: { slug: 'leave' } });
    await screen.findByRole('list', { name: 'Steps' });
    await fireEvent.click(screen.getByRole('button', { name: 'Remove Long boards' }));
    expect(screen.queryByRole('button', { name: 'Remove Long boards' })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Save flow' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    const body = sent.find((r) => r.method === 'PUT')!.body as { definition: Flow['definition'] };
    expect(body.definition.category_ids).toEqual(['short']);
  });

  it('says it saved a new flow, once it opens at its address', async () => {
    history.replaceState(null, '', '/flows/leave/edit?saved=1');
    api(() => ({ flow }));
    render(FlowEditor, { params: { slug: 'leave' } });
    expect((await screen.findByText('Saved Leave something.')).getAttribute('role')).toBe('status');
    expect(location.search).toBe('');
  });
});
