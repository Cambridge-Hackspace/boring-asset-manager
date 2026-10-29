import { describe, expect, it } from 'vitest';
import type { Definition } from './flows';
import { addStep, checkFlow, moveStep, newStepID, problemsOf, slugFrom } from './flowEditor';

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
      'Another step is named about.',
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
});
