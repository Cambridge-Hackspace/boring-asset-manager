import { describe, expect, it } from 'vitest';
import type { Definition, Step } from './flows';
import { addStep, asks, checkFlow, moveStep, newStepID, previewProgress, problemsOf, slugFrom, stepsUsing, withStepIDs } from './flowEditor';

const leave: Definition = {
  version: 1,
  category_ids: ['short', 'long'],
  steps: [
    { id: 'kind', kind: 'choose_category' },
    { id: 'about', kind: 'form', ask_name: true },
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
      version: 1,
      category_ids: ['short', 'long'],
      steps: [
        { id: 'about', kind: 'form' },
        { id: 'label', kind: 'label', label: { mode: 'print' } },
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
    const d: Definition = { version: 1, category_ids: [], steps: [{ id: 'confirm', kind: 'confirm' }] };
    expect(problemsOf(checkFlow(d))).toEqual(['Choose at least one category to file into or take from.', 'A flow ends with a Done step.']);
  });
  it('lets one category stand for choosing', () => {
    expect(checkFlow({ ...leave, category_ids: ['short'], steps: leave.steps.slice(1) })).toEqual([]);
  });
});

describe('steps', () => {
  it('adds before Done, with a name of its own', () => {
    const steps = addStep(leave.steps, 'form');
    expect(steps.map((s) => s.id)).toEqual(['kind', 'about', 'file', 'label', 'form', 'done']);
    expect(newStepID('form', steps)).toBe('form_2');
    expect(addStep(addStep(steps, 'form'), 'create').map((s) => s.id)).toEqual(['kind', 'about', 'file', 'label', 'form', 'form_2', 'file_2', 'done']);
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
      { id: 'note', kind: 'change', changes: [{ kind: 'fields', fields: { notes: 'About {what}' } }] },
      { id: 'when', kind: 'confirm', when: { value: 'what', op: 'set' } },
      { id: 'other', kind: 'confirm', title: 'Is {whatever} right?', text: '{what_else}' },
      { id: 'done', kind: 'done', text: 'Filed {what}.' },
    ];
    expect(stepsUsing(steps, 'what').map((s) => s.id)).toEqual(['file', 'look', 'note', 'when', 'done']);
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
      { id: 'file', kind: 'create' },
      { id: 'put', kind: 'create', location: { mode: 'scan' } },
      { id: 'label', kind: 'label' },
      { id: 'done', kind: 'done' },
    ];
    expect(steps.filter(asks).map((s) => s.id)).toEqual(['kind', 'about', 'long_only', 'put']);
    // Before any answer, a step with a condition counts as if it held.
    expect(previewProgress(steps, 2)).toEqual({ step: 2, of: 4 });
    expect(previewProgress(steps, 5)).toEqual({ step: 4, of: 4 });
    expect(previewProgress(steps, 4)).toBeNull();
    expect(previewProgress(steps, 7)).toBeNull();
  });
});
