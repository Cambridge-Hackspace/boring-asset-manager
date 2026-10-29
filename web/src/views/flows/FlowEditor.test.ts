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

function api(save: (body: unknown) => unknown) {
  return fakeApi({
    'GET /categories': () => ({
      categories: [
        { id: 'short', name: 'Short boards', color_hex: '#15803d' },
        { id: 'long', name: 'Long boards', color_hex: '#1d4ed8' },
      ],
    }),
    'GET /flows': () => ({ flows: [flow] }),
    'GET /flows/leave': () => ({ flow }),
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
  it('edits steps as cards, previews the one chosen, and says what stops saving beside its step', async () => {
    const { sent } = api(() => ({ status: 400, body: { error: 'The flow cannot be saved', problems: [{ step: 'file', message: 'its steps use assets.write, which you do not hold' }] } }));
    render(FlowEditor, { params: { slug: 'leave' } });
    const steps = await screen.findByRole('list', { name: 'Steps' });
    expect(within(steps).getAllByRole('listitem')).toHaveLength(4);

    // The form's card lists the union of the categories' fields; its
    // preview shows them as a phone would.
    await fireEvent.click(screen.getByRole('button', { name: 'Preview step 2' }));
    const preview = screen.getByRole('figure', { name: 'Preview, as a phone shows it' });
    await vi.waitFor(() => expect(preview.textContent).toContain('Until'));
    expect(preview.textContent).toContain('What is it?');
    await fireEvent.input(screen.getByLabelText('Label for Until'), { target: { value: 'Leave until' } });
    expect(preview.textContent).toContain('Leave until');

    // Checked here first: a step whose name is taken.
    await fireEvent.input(within(steps).getAllByLabelText('Name')[2]!, { target: { value: 'about' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save flow' }));
    expect(await screen.findAllByText('Another step is named about.')).toHaveLength(2);
    expect(sent.some((r) => r.method === 'PUT')).toBe(false);

    // Then by the server, beside the step it names.
    await fireEvent.input(within(steps).getAllByLabelText('Name')[2]!, { target: { value: 'file' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save flow' }));
    expect(await screen.findByText('its steps use assets.write, which you do not hold')).toBeTruthy();
    const body = sent.find((r) => r.method === 'PUT')!.body as { definition: Flow['definition'] };
    expect(body.definition.steps[1]!.fields).toEqual([{ key: 'until', label: 'Leave until' }]);
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
  });

  it('says it saved a new flow, once it opens at its address', async () => {
    history.replaceState(null, '', '/flows/leave/edit?saved=1');
    api(() => ({ flow }));
    render(FlowEditor, { params: { slug: 'leave' } });
    expect((await screen.findByText('Saved Leave something.')).getAttribute('role')).toBe('status');
    expect(location.search).toBe('');
  });
});
