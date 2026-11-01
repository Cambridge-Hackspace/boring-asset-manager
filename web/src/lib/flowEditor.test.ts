import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import type { Definition, Step } from './flows';
import type { Field } from './models';
import {
  addStep,
  answerProblem,
  asks,
  checkFlow,
  fieldValueProblem,
  moveStep,
  newStep,
  newStepID,
  previewProgress,
  problemsOf,
  setsOn,
  slugFrom,
  stepsUsing,
  suggest,
  unknownNames,
  valuesBefore,
  type ValueField,
  withStepIDs,
  type FlowCatalog,
} from './flowEditor';

const leave: Definition = {
  version: 2,
  category_ids: ['short', 'long'],
  steps: [
    { id: 'kind', kind: 'choose_category' },
    { id: 'about', kind: 'form', questions: [{ key: 'name', label: 'What is it?', required: true }] },
    { id: 'file', kind: 'create', name: '{name}' },
    { id: 'label', kind: 'label', label: { mode: 'show' } },
    { id: 'done', kind: 'done' },
  ],
};

describe('checkFlow', () => {
  it('passes a starter', () => {
    expect(checkFlow(leave)).toEqual([]);
  });
  it('says what each step needs from those before it', () => {
    const d: Definition = {
      version: 2,
      category_ids: ['short', 'long'],
      steps: [
        { id: 'label', kind: 'label', label: { mode: 'print' } },
        { id: 'about', kind: 'create', name: 'x' },
        { id: 'Bad Name', kind: 'change', changes: [] },
        { id: 'about', kind: 'find', find_by: [] },
        { id: 'done', kind: 'done' },
      ],
    };
    expect(problemsOf(checkFlow(d), 'about')).toEqual([
      'It needs a category: add Choose category before it, or give the flow only one.',
      'This step has the same ID as another: remove it and add it again.',
      'Choose at least one way to find: scan, search or list.',
    ]);
    expect(problemsOf(checkFlow(d), 'label')).toEqual(['It needs something found or made in an earlier step.', 'Choose the printer or pool it prints to.']);
    expect(problemsOf(checkFlow(d), 'Bad Name')).toContain('Add at least one change.');
  });
  it('wants a category, and Done last', () => {
    const d: Definition = { version: 2, category_ids: [], steps: [{ id: 'confirm', kind: 'confirm' }] };
    expect(problemsOf(checkFlow(d))).toEqual(['Choose at least one category to file into or take from.', 'A flow ends with a Done step.']);
  });
  it('lets one category stand for choosing', () => {
    expect(checkFlow({ ...leave, category_ids: ['short'], steps: leave.steps.slice(1) })).toEqual([]);
  });
  it("checks a form's questions as the server does", () => {
    const d: Definition = {
      version: 2,
      category_ids: ['short'],
      steps: [
        {
          id: 'about',
          kind: 'form',
          questions: [
            { key: 'name', label: 'What?' },
            { key: 'name', label: 'Again?' },
            { key: 'Bad', label: '' },
            { key: 'wood', label: 'Wood', type: 'choice', options: [{ value: 'oak' }, { value: 'oak' }, { value: ' ' }] },
            { key: 'none', label: 'None', type: 'choice' },
            { key: 'fragile', label: 'Fragile', type: 'yes_no', default: 'maybe' },
            { key: 'boards', label: 'Boards', type: 'number', default: '{user}' },
          ],
        },
        { id: 'done', kind: 'done' },
      ],
    };
    expect(problemsOf(checkFlow(d), 'about')).toEqual([
      'The question name is asked twice.',
      'A question\'s key is lowercase letters, digits and _: “Bad”.',
      'The question Bad needs a label.',
      'Wood offers “oak” twice.',
      'Each choice of Wood needs a value.',
      'None needs choices to choose from.',
      'The default of Fragile is yes or no.',
    ]);
  });
});

// The fields of a wood shop: both kinds of board require an owner; long
// boards, a date too; and a grain to choose.
const shop: FlowCatalog = {
  fields: [
    { id: 'o', key: 'owner', label: 'Owner', validation_type: 'text' },
    { id: 'u', key: 'until', label: 'Until', validation_type: 'date' },
    { id: 'g', key: 'grain', label: 'Grain', validation_type: 'select', options: { choices: [{ label: 'Oak', value: 'oak' }] } },
  ] as Field[],
  required: { short: ['owner'], long: ['owner', 'until'] },
  names: { short: 'Short boards', long: 'Long boards' },
};

describe('checkFlow, on Set fields', () => {
  const steps = (...middle: Step[]): Definition => ({
    version: 2,
    category_ids: ['short', 'long'],
    steps: [
      { id: 'kind', kind: 'choose_category' },
      { id: 'about', kind: 'form', questions: [{ key: 'name', label: 'What?' }, { key: 'photo', label: 'Photo', type: 'photo' }] },
      ...middle,
      { id: 'file', kind: 'create', name: '{name}' },
      { id: 'done', kind: 'done' },
    ],
  });
  const long = { value: 'category_id', op: 'is' as const, to: 'long' };
  it('passes when each category Create may file into has what it requires', () => {
    const d = steps(
      { id: 'set', kind: 'set_fields', fields: [{ field: 'owner', value: '{user}' }], photo: '{photo}' },
      { id: 'set_2', kind: 'set_fields', fields: [{ field: 'until', value: '{today}' }], when: long },
    );
    expect(checkFlow(d, shop)).toEqual([]);
  });
  it('names the fields a category requires that nothing sets before Create', () => {
    expect(problemsOf(checkFlow(steps(), shop), 'file')).toEqual([
      'Short boards requires Owner, which no Set fields step before it sets.',
      'Long boards requires Owner, Until, which no Set fields step before it sets.',
    ]);
    // A row shown on another condition counts for no category.
    const d = steps({ id: 'set', kind: 'set_fields', fields: [{ field: 'owner', value: '{user}' }, { field: 'until', value: '{today}' }], when: { value: 'name', op: 'set' } });
    expect(problemsOf(checkFlow(d, shop), 'file')).toHaveLength(2);
    // Offered only short boards, it needs only theirs.
    const short = steps({ id: 'set', kind: 'set_fields', fields: [{ field: 'owner', value: '{user}' }] });
    short.steps[0] = { id: 'kind', kind: 'choose_category', categories: ['short'] };
    expect(checkFlow(short, shop)).toEqual([]);
    // Without the categories' fields, it cannot tell.
    expect(checkFlow(steps())).toEqual([]);
  });
  it('checks each row: a field, once, values set before it, and fixed values that suit', () => {
    const d = steps({
      id: 'set',
      kind: 'set_fields',
      fields: [
        { field: 'owner', value: '{later}' },
        { field: 'owner', value: '{user}' },
        { field: '', value: 'x' },
        { field: 'colour', value: 'red' },
        { field: 'until', value: 'soon' },
        { field: 'grain', value: 'teak' },
      ],
      photo: '{name}',
    });
    expect(problemsOf(checkFlow(d, shop), 'set')).toEqual([
      'The value of Owner names {later}, which no earlier step sets.',
      'It sets Owner twice.',
      'Choose the field each row sets.',
      'No field the flow\'s categories record has the key “colour”.',
      'Until: “soon” is not a date.',
      'Grain: “teak” is not one of its choices.',
      'Its photo is the answer to a photo question asked before it.',
    ]);
    const nothing: Definition = { version: 2, category_ids: ['short'], steps: [{ id: 'set', kind: 'set_fields' }, { id: 'done', kind: 'done' }] };
    expect(problemsOf(checkFlow(nothing, shop), 'set')).toEqual([
      'It sets fields on nothing: put it before a Create, or after a Find or Create.',
      'Add at least one field to set.',
    ]);
  });
});

describe('setsOn', () => {
  it('sets on the asset the next Create files, or the one found or filed before it', () => {
    const steps: Step[] = [
      { id: 'set', kind: 'set_fields' },
      { id: 'file', kind: 'create' },
      { id: 'set_2', kind: 'set_fields' },
      { id: 'find', kind: 'find' },
      { id: 'set_3', kind: 'set_fields' },
      { id: 'file_2', kind: 'create' },
      { id: 'done', kind: 'done' },
    ];
    expect([0, 2, 4].map((i) => setsOn(steps, i))).toEqual(['new', 'filed', 'new']);
    expect(setsOn([{ id: 'find', kind: 'find' }, { id: 'set', kind: 'set_fields' }], 1)).toBe('found');
    expect(setsOn([{ id: 'set', kind: 'set_fields' }], 0)).toBe('');
  });
});

describe('values that suit', () => {
  it('an answer suits its question', () => {
    expect(answerProblem({ key: 'n', label: 'N', type: 'number' }, 'two')).toBe('is a number');
    expect(answerProblem({ key: 'd', label: 'D', type: 'date' }, '2026-02-30')).toBe('is a date, as 2026-10-30');
    expect(answerProblem({ key: 'd', label: 'D', type: 'date' }, '2026-10-30')).toBe('');
    expect(answerProblem({ key: 'c', label: 'C', type: 'choice', options: [{ value: 'a' }] }, 'a')).toBe('');
    expect(answerProblem({ key: 't', label: 'T' }, 'anything')).toBe('');
  });
  it('a fixed value suits its field', () => {
    expect(fieldValueProblem(shop.fields[1]!, '2026-10-30')).toBe('');
    expect(fieldValueProblem(shop.fields[2]!, 'oak')).toBe('');
    expect(fieldValueProblem({ id: 'n', key: 'n', label: 'N', validation_type: 'number' } as Field, '3.5')).toBe('');
    expect(fieldValueProblem({ id: 'w', key: 'w', label: 'W', validation_type: 'url' } as Field, 'example')).toBe('is not a full address');
  });
});

describe('steps', () => {
  it('adds before Done, with a name of its own', () => {
    const steps = addStep(leave.steps, 'form');
    expect(steps.map((s) => s.id)).toEqual(['kind', 'about', 'file', 'label', 'form', 'done']);
    expect(newStepID('form', steps)).toBe('form_2');
    expect(addStep(addStep(steps, 'form'), 'create').map((s) => s.id)).toEqual(['kind', 'about', 'file', 'label', 'form', 'form_2', 'file_2', 'done']);
    expect(addStep(steps, 'set_fields').at(-2)).toEqual({ id: 'set', kind: 'set_fields', fields: [] });
  });
  it('starts a form asking for what it is, by {name}', () => {
    expect(newStep('form', []).questions).toEqual([{ key: 'name', label: 'What is it?', type: 'text', required: true }]);
  });
  it('keeps the IDs steps have, and gives one to any without its own (#171)', () => {
    const steps: Step[] = [
      { id: 'about', kind: 'form' },
      { id: '', kind: 'form' },
      { id: 'about', kind: 'find', find_by: ['scan'] },
      { id: 'Bad Name', kind: 'create' },
      { id: 'done', kind: 'done' },
    ];
    const out = withStepIDs(steps);
    expect(out.map((s) => s.id)).toEqual(['about', 'form', 'find', 'file', 'done']);
    // A step that keeps its ID is the same step.
    expect(out[0]).toBe(steps[0]);
    expect(out[1]).toEqual({ id: 'form', kind: 'form' });
    expect(withStepIDs(leave.steps)).toEqual(leave.steps);
    expect(checkFlow({ ...leave, steps: withStepIDs([...leave.steps.slice(0, 2), { id: 'about', kind: 'form' }, ...leave.steps.slice(2)]) })).toEqual([]);
  });
  it('finds the steps that name a value', () => {
    const steps: Step[] = [
      { id: 'file', kind: 'create', name: '{owner}: {what}' },
      { id: 'look', kind: 'find', find_by: ['list'], match: { field: 'owner', value: '{what}' } },
      { id: 'note', kind: 'set_fields', fields: [{ field: 'notes', value: 'About {what}' }] },
      { id: 'pic', kind: 'set_fields', photo: '{what}' },
      { id: 'when', kind: 'confirm', when: { value: 'what', op: 'set' } },
      { id: 'other', kind: 'confirm', title: 'Is {whatever} right?', text: '{what_else}' },
      { id: 'done', kind: 'done', text: 'Filed {what}.' },
    ];
    expect(stepsUsing(steps, 'what').map((s) => s.id)).toEqual(['file', 'look', 'note', 'pic', 'when', 'done']);
    expect(stepsUsing(steps, 'owner').map((s) => s.id)).toEqual(['file']);
    expect(stepsUsing(steps, '')).toEqual([]);
  });
  it('moves, but never past Done', () => {
    expect(moveStep(leave.steps, 1, -1).map((s) => s.id)).toEqual(['about', 'kind', 'file', 'label', 'done']);
    expect(moveStep(leave.steps, 3, 1).map((s) => s.id)).toEqual(['kind', 'about', 'file', 'label', 'done']);
    expect(moveStep(leave.steps, 0, -1).map((s) => s.id)).toEqual(['kind', 'about', 'file', 'label', 'done']);
  });
  it('makes an address from a name', () => {
    expect(slugFrom('  Leave Something Here! ')).toBe('leave-something-here');
    expect(slugFrom('Café shelf')).toBe('cafe-shelf');
  });

  it('counts the steps that ask something, as the server does (#166)', () => {
    const steps: Step[] = [
      { id: 'kind', kind: 'choose_category' },
      { id: 'fixed', kind: 'choose_category', fixed: 'short' },
      { id: 'about', kind: 'form' },
      { id: 'long_only', kind: 'confirm', when: { value: 'category_id', op: 'is', to: 'long' } },
      { id: 'set', kind: 'set_fields' },
      { id: 'file', kind: 'create' },
      { id: 'put', kind: 'create', location: { mode: 'scan' } },
      { id: 'label', kind: 'label' },
      { id: 'done', kind: 'done' },
    ];
    expect(steps.filter(asks).map((s) => s.id)).toEqual(['kind', 'about', 'long_only', 'put']);
    // Before any answer, a step with a condition counts as if it held.
    expect(previewProgress(steps, 2)).toEqual({ step: 2, of: 4 });
    expect(previewProgress(steps, 6)).toEqual({ step: 4, of: 4 });
    expect(previewProgress(steps, 4)).toBeNull();
    expect(previewProgress(steps, 5)).toBeNull();
    expect(previewProgress(steps, 8)).toBeNull();
  });
});

// The cases saving is tested against too (internal/flows/values_test.go),
// so what the editor offers and what saving accepts cannot disagree (#169).
interface ValueCase {
  name: string;
  fields: ValueField[];
  definition: Definition;
  available: Record<string, string[]>;
  empty_when_skipped: Record<string, string[]>;
}
const valueCases: ValueCase[] = JSON.parse(readFileSync(resolve(process.cwd(), '../internal/flows/testdata/values.json'), 'utf8')).cases;

describe('valuesBefore', () => {
  it('has cases to agree with saving on', () => expect(valueCases.length).toBeGreaterThan(0));

  for (const c of valueCases) {
    it(`offers each step what saving accepts there: ${c.name}`, () => {
      c.definition.steps.forEach((s, i) => {
        const got = valuesBefore(c.definition, i, c.fields);
        expect(got.map((v) => v.key).sort(), s.id).toEqual([...c.available[s.id]!].sort());
        expect(got.filter((v) => v.note).map((v) => v.key).sort(), `${s.id}, empty when skipped`).toEqual([...(c.empty_when_skipped[s.id] ?? [])].sort());
      });
    });
  }

  it('starts with the values every run has, and says where each value comes from', () => {
    const got = valuesBefore(leave, 3, [{ key: 'owner', label: 'Owner' }], { long: 'Long boards' });
    expect(got.slice(0, 4).map((v) => [v.key, v.from])).toEqual([
      ['today', 'Every run'],
      ['now', 'Every run'],
      ['user', 'Every run'],
      ['flow', 'Every run'],
    ]);
    expect(got.find((v) => v.key === 'name')).toMatchObject({ label: 'What is it?', from: 'Form' });
    expect(got.find((v) => v.key === 'owner')).toMatchObject({ label: 'Owner', from: 'Create' });
    // Its own values are not before it.
    expect(valuesBefore(leave, 1).map((v) => v.key)).not.toContain('name');
  });

  it('says a value a step shown only when something holds sets is empty when it is skipped, and what it compares with', () => {
    const d: Definition = {
      version: 2,
      category_ids: ['short', 'long'],
      steps: [
        { id: 'kind', kind: 'choose_category' },
        {
          id: 'long',
          kind: 'form',
          title: 'Long boards',
          when: { value: 'category_id', op: 'is', to: 'long' },
          questions: [
            { key: 'wood', label: 'Which wood?', type: 'choice', options: [{ value: 'oak', label: 'Oak' }, { value: 'pine' }] },
            { key: 'until', label: 'Until', type: 'date' },
            { key: 'waxed', label: 'Waxed?', type: 'yes_no' },
          ],
        },
        { id: 'done', kind: 'done' },
      ],
    };
    const got = valuesBefore(d, 2, [], { long: 'Long boards' });
    const wood = got.find((v) => v.key === 'wood')!;
    expect(wood.from).toBe('Form: Long boards');
    expect(wood.note).toBe('Empty when Form: Long boards is skipped: it is shown only when {category_id} is “Long boards”.');
    expect(wood.input).toEqual({ kind: 'choices', options: [{ value: 'oak', label: 'Oak' }, { value: 'pine', label: 'pine' }] });
    expect(got.find((v) => v.key === 'until')!.input).toEqual({ kind: 'date' });
    expect(got.find((v) => v.key === 'waxed')!.input).toEqual({ kind: 'choices', options: [{ value: 'yes', label: 'Yes' }, { value: 'no', label: 'No' }] });
    expect(got.find((v) => v.key === 'category_id')!.input).toEqual({ kind: 'category' });
    expect(got.find((v) => v.key === 'category_id')!.note).toBeUndefined();
  });

  it('suggests by key, those starting with what was typed first, and finds names not available', () => {
    const got = valuesBefore(leave, 2);
    expect(suggest(got, 'na').map((v) => v.key)).toEqual(['name']);
    expect(suggest(got, 'o').map((v) => v.key)).toEqual(['today', 'now', 'flow', 'category', 'category_id']);
    expect(suggest(got, 'c').map((v) => v.key)[0]).toBe('category');
    expect(unknownNames('{owner}: {name} {owner}', got)).toEqual(['owner']);
    expect(unknownNames('{name}', got)).toEqual([]);
  });
});
