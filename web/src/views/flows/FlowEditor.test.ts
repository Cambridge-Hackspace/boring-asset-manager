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
    version: 2,
    category_ids: ['short', 'long'],
    steps: [
      { id: 'kind', kind: 'choose_category', title: 'What are you leaving?' },
      {
        id: 'about',
        kind: 'form',
        title: 'About it',
        questions: [
          { key: 'name', label: 'What is it?', type: 'text', required: true },
          { key: 'until', label: 'Until', type: 'date' },
        ],
      },
      {
        id: 'set',
        kind: 'set_fields',
        fields: [
          { field: 'owner', value: '{user}' },
          { field: 'until', value: '{until}' },
        ],
      },
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
        { field: { id: 'u', key: 'until', label: 'Leave by', validation_type: 'date' }, is_required: false },
        {
          field: { id: 'g', key: 'grain', label: 'Grain', validation_type: 'select', options: { choices: [{ label: 'Oak', value: 'oak' }, { label: 'Pine', value: 'pine' }] } },
          is_required: false,
        },
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
    expect(within(steps).getAllByRole('listitem')).toHaveLength(5);

    // The first step starts open, and only it shows its settings.
    expect(screen.getByRole('button', { name: '1. Choose category: What are you leaving?' }).getAttribute('aria-expanded')).toBe('true');
    expect(within(steps).getAllByLabelText('Heading')).toHaveLength(1);
    // A step's ID is the API's, not something to type (#171).
    expect(within(steps).queryByLabelText('Name')).toBeNull();

    // The form asks its own questions, of any type; its preview shows them
    // as a phone would.
    await fireEvent.click(screen.getByRole('button', { name: '2. Form: About it' }));
    expect(screen.getByRole('button', { name: '1. Choose category: What are you leaving?' }).getAttribute('aria-expanded')).toBe('false');
    // Counted as a run counts them: Set fields, Create and Done ask nothing.
    expect(screen.getByText('Preview · step 2 of 2')).toBeTruthy();
    const preview = screen.getByRole('figure', { name: 'Preview, as a phone shows it' });
    expect(preview.textContent).toContain('What is it? *');
    expect(preview.textContent).toContain('A date');
    // The form no longer lists the categories' fields.
    expect(screen.queryByText('Ask it')).toBeNull();
    const second = screen.getByRole('group', { name: 'Question 2' });
    await fireEvent.input(within(second).getByRole('textbox', { name: 'Question 2' }), { target: { value: 'Leave until' } });
    expect(preview.textContent).toContain('Leave until');
    await fireEvent.input(within(second).getByLabelText('Help for question 2'), { target: { value: "When you'll take it home" } });
    expect(preview.textContent).toContain("When you'll take it home");
    expect(within(second).getAllByRole('option').map((o) => o.textContent)).toEqual(['Text', 'Long text', 'Number', 'Date', 'Yes or no', 'A choice', 'A photo']);

    // A yes/no question's default is yes or no.
    await fireEvent.click(screen.getByRole('button', { name: 'Add a question' }));
    const third = screen.getByRole('group', { name: 'Question 3' });
    await fireEvent.input(within(third).getByRole('textbox', { name: 'Question 3' }), { target: { value: 'Fragile?' } });
    await fireEvent.change(within(third).getByLabelText('Type of question 3'), { target: { value: 'yes_no' } });
    await fireEvent.change(within(third).getByLabelText('Default for question 3'), { target: { value: 'no' } });
    expect(preview.textContent).toContain('YesNo');

    // A choice lists its choices, each answered as a value made from it.
    await fireEvent.click(screen.getByRole('button', { name: 'Add a question' }));
    const fourth = screen.getByRole('group', { name: 'Question 4' });
    await fireEvent.input(within(fourth).getByRole('textbox', { name: 'Question 4' }), { target: { value: 'Which wood?' } });
    await fireEvent.change(within(fourth).getByLabelText('Type of question 4'), { target: { value: 'choice' } });
    await fireEvent.input(within(fourth).getByLabelText('Choice 1 of question 4'), { target: { value: 'Red oak' } });
    expect(within(fourth).getByText(/Answered as/).textContent).toContain('red_oak');
    await fireEvent.click(within(fourth).getByRole('button', { name: 'Add a choice' }));
    await fireEvent.input(within(fourth).getByLabelText('Choice 2 of question 4'), { target: { value: 'Pine' } });
    expect(preview.textContent).toContain('Red oak');
    expect(preview.textContent).toContain('Pine');
    // Moved up, it is asked third.
    await fireEvent.click(within(fourth).getByRole('button', { name: 'Move question 4 up' }));
    expect(within(screen.getByRole('group', { name: 'Question 3' })).getByRole('textbox', { name: 'Question 3' })).toHaveProperty('value', 'Which wood?');

    // The server's problems show beside the step they name, by its ID.
    await fireEvent.click(screen.getByRole('button', { name: '4. Create' }));
    expect(screen.getByText('Preview · asks nothing, so a run does it and moves on')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Save flow' }));
    const problem = await screen.findByText('its steps use assets.write, which you do not hold');
    expect(problem.closest('li')!.textContent).toContain('4. Create');
    const body = sent.find((r) => r.method === 'PUT')!.body as { definition: Flow['definition'] };
    expect(body.definition.version).toBe(2);
    expect(body.definition.steps[1]!.questions).toEqual([
      { key: 'name', label: 'What is it?', type: 'text', required: true },
      { key: 'until', label: 'Leave until', type: 'date', help: "When you'll take it home" },
      { key: 'which_wood', label: 'Which wood?', type: 'choice', default: '', options: [{ value: 'red_oak', label: 'Red oak' }, { value: 'pine', label: 'Pine' }] },
      { key: 'fragile', label: 'Fragile?', type: 'yes_no', default: 'no' },
    ]);
    // The steps keep their IDs, so runs under way still match.
    expect(body.definition.steps.map((s) => s.id)).toEqual(['kind', 'about', 'set', 'file', 'done']);
  });

  it("sets fields chosen by category, says where it sets them, and checks them as the server does", async () => {
    const { sent } = api(() => ({ flow }));
    render(FlowEditor, { params: { slug: 'leave' } });
    await fireEvent.click(await screen.findByRole('button', { name: '3. Set fields' }));
    expect(screen.getByText('On the new asset, as it is filed')).toBeTruthy();
    const first = screen.getByRole('group', { name: 'Field 1' });
    // Chosen from the fields each category records, by their own labels.
    await vi.waitFor(() => expect(within(first).getAllByRole('group').map((g) => g.getAttribute('label'))).toEqual(['Short boards', 'Long boards']));
    const [short, long] = within(first).getAllByRole('group');
    await vi.waitFor(() => expect([...short!.querySelectorAll('option')].map((o) => o.textContent)).toEqual(['Owner']));
    expect([...long!.querySelectorAll('option')].map((o) => o.textContent)).toEqual(['Owner', 'Leave by', 'Grain']);
    expect((within(first).getByLabelText('Value of field 1') as HTMLInputElement).value).toBe('{user}');

    // A choice field offers its choices.
    await fireEvent.click(screen.getByRole('button', { name: 'Add a field' }));
    const third = screen.getByRole('group', { name: 'Field 3' });
    await fireEvent.change(within(third).getByLabelText('Field 3'), { target: { value: 'grain' } });
    expect(within(third).getByText('Its choices: oak, pine')).toBeTruthy();
    const offered = third.querySelectorAll('datalist option');
    expect([...offered].map((o) => (o as HTMLOptionElement).value)).toEqual(['oak', 'pine']);
    // One it does not offer stops saving, said beside the step.
    await fireEvent.input(within(third).getByLabelText('Value of field 3'), { target: { value: 'teak' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save flow' }));
    expect((await screen.findByText('Grain: “teak” is not one of its choices.')).closest('li')!.textContent).toContain('3. Set fields');
    expect(sent.some((r) => r.method === 'PUT')).toBe(false);

    // So does a field a category requires and nothing sets.
    await fireEvent.click(within(screen.getByRole('group', { name: 'Field 1' })).getByRole('button', { name: 'Remove field 1' }));
    await fireEvent.input(within(screen.getByRole('group', { name: 'Field 2' })).getByLabelText('Value of field 2'), { target: { value: 'oak' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save flow' }));
    expect((await screen.findByText('Short boards requires Owner, which no Set fields step before it sets.')).closest('li')!.textContent).toContain('4. Create');

    // Moved after Create, it sets the asset filed.
    await fireEvent.click(screen.getByRole('button', { name: 'Move step 3 down' }));
    expect(screen.getByText('On the asset filed')).toBeTruthy();

    // Change sets no fields: Set fields does, and is a step to add.
    expect(within(screen.getByLabelText('Add a step')).getAllByRole('option').map((o) => o.textContent)).toContain('Set fields');
    await fireEvent.change(screen.getByLabelText('Add a step'), { target: { value: 'change' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    const change = screen.getByRole('group', { name: 'Change 1' });
    expect(within(change).getAllByRole('option').map((o) => o.textContent)).not.toContain('Set fields');
  });

  it('adds a step before Done, and moves steps but not Done', async () => {
    api(() => ({ flow }));
    render(FlowEditor, { params: { slug: 'leave' } });
    const steps = await screen.findByRole('list', { name: 'Steps' });
    await fireEvent.change(screen.getByLabelText('Add a step'), { target: { value: 'confirm' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    const headings = () => within(steps).getAllByRole('heading').map((h) => h.textContent?.trim());
    expect(headings().at(-2)).toBe('5. Confirm: Is this right?');
    expect(headings().at(-1)).toBe('6. Done: Filed');
    expect((screen.getByRole('button', { name: 'Move step 5 down' }) as HTMLButtonElement).disabled).toBe(true);
    await fireEvent.click(screen.getByRole('button', { name: 'Move step 5 up' }));
    expect(headings()[3]).toBe('4. Confirm: Is this right?');
    // The step added opens, and stays open as it moves.
    expect(screen.getByRole('button', { name: '4. Confirm: Is this right?' }).getAttribute('aria-expanded')).toBe('true');
    // Moving another step past it keeps it open too.
    await fireEvent.click(screen.getByRole('button', { name: 'Move step 3 down' }));
    expect(screen.getByRole('button', { name: '3. Confirm: Is this right?' }).getAttribute('aria-expanded')).toBe('true');
  });

  it("makes a question's key from its text, and warns before renaming one a later step uses (#171)", async () => {
    const asked: Flow = {
      ...flow,
      definition: {
        ...flow.definition,
        steps: [
          flow.definition.steps[0]!,
          { id: 'about', kind: 'form', title: 'About it', questions: [{ key: 'what', label: 'What is it?' }] },
          { id: 'set', kind: 'set_fields', fields: [{ field: 'owner', value: '{user}' }] },
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
    expect(within(first).getByRole('note').textContent).toBe('Step 4 (Create) and step 5 (Done: Filed) use {what}: change it there too, or they will name nothing.');
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
    expect(body.definition.steps[1]!.questions).toEqual([{ key: 'anything_more', label: 'Anything more?', type: 'text' }]);
  });

  it('offers, in Only when, only the values set before the step, and compares a choice question with its choices', async () => {
    const withWood: Flow = {
      ...flow,
      definition: {
        ...flow.definition,
        steps: flow.definition.steps.map((s) =>
          s.id === 'about'
            ? { ...s, questions: [...s.questions!, { key: 'wood', label: 'Which wood?', type: 'choice', options: [{ value: 'oak', label: 'Oak' }, { value: 'pine', label: 'Pine' }] }] }
            : s,
        ),
      },
    };
    const { sent } = api(() => ({ flow: withWood }), withWood);
    render(FlowEditor, { params: { slug: 'leave' } });
    await fireEvent.click(await screen.findByRole('button', { name: '4. Create' }));
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Only when' }));
    const depends = screen.getByLabelText('The value it depends on') as HTMLSelectElement;
    // The category, chosen before it, to start with, compared with the flow's categories.
    expect(depends.value).toBe('category_id');
    expect(within(screen.getByLabelText('The category')).getAllByRole('option').map((o) => o.textContent)).toEqual(['Short boards', 'Long boards']);
    // What runs have and earlier steps set; not what it sets itself, nor what comes after.
    const keys = [...depends.options].map((o) => o.value);
    expect(keys).toEqual(expect.arrayContaining(['today', 'now', 'user', 'flow', 'category', 'category_id', 'name', 'until', 'wood', 'owner']));
    expect(keys).not.toContain('asset');
    expect(keys).not.toContain('code');

    // A choice question is compared with its choices; a date, as a date.
    await fireEvent.change(depends, { target: { value: 'wood' } });
    const choice = screen.getByLabelText('The choice') as HTMLSelectElement;
    expect([...choice.options].map((o) => [o.value, o.textContent])).toEqual([
      ['oak', 'Oak'],
      ['pine', 'Pine'],
    ]);
    await fireEvent.change(choice, { target: { value: 'pine' } });
    await fireEvent.change(depends, { target: { value: 'until' } });
    expect((screen.getByLabelText('The date') as HTMLInputElement).type).toBe('date');
    await fireEvent.change(depends, { target: { value: 'wood' } });
    await fireEvent.change(screen.getByLabelText('The choice'), { target: { value: 'pine' } });

    // Its name suggests what is set before it, and marks what is not.
    const name = screen.getByRole('combobox', { name: 'Its name' }) as HTMLInputElement;
    name.value = '{name} {w';
    name.setSelectionRange(name.value.length, name.value.length);
    await fireEvent.input(name);
    const suggested = within(screen.getByRole('listbox', { name: 'Values Its name can use' })).getAllByRole('option');
    expect(suggested.map((o) => o.textContent?.trim().split(' ')[0])).toEqual(['{wood}', '{now}', '{flow}', '{owner}']);
    await fireEvent.keyDown(name, { key: 'Enter' });
    expect(name.value).toBe('{name} {wood}');
    name.value = '{name} {asset}';
    await fireEvent.input(name);
    expect(screen.getByText('{asset} is not set before this step.')).toBeTruthy();
    name.value = '{name}: {wood}';
    await fireEvent.input(name);

    await fireEvent.click(screen.getByRole('button', { name: 'Save flow' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    const body = sent.find((r) => r.method === 'PUT')!.body as { definition: Flow['definition'] };
    expect(body.definition.steps[3]).toMatchObject({ name: '{name}: {wood}', when: { value: 'wood', op: 'is', to: 'pine' } });
  });

  it('chooses its categories with the picker, and saves them', async () => {
    const { sent } = api(() => ({ flow }));
    render(FlowEditor, { params: { slug: 'leave' } });
    await screen.findByRole('list', { name: 'Steps' });
    await fireEvent.click(screen.getByRole('button', { name: 'Remove Long boards' }));
    expect(screen.queryByRole('button', { name: 'Remove Long boards' })).toBeNull();
    // Short boards do not record until, so it is set no more.
    await fireEvent.click(screen.getByRole('button', { name: '3. Set fields' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Remove field 2' }));
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
