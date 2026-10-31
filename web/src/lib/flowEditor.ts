// Editing flows (README §6.7): what each kind of step is called and starts
// as, and the checks the editor makes as you go. The server checks a flow
// again, fully, when it is saved.

import type { Definition, FlowProblem, Progress, Step, StepKind } from './flows';

export const kindLabels: Record<StepKind, string> = {
  choose_category: 'Choose category',
  form: 'Form',
  find: 'Find',
  create: 'Create',
  label: 'Label',
  change: 'Change',
  confirm: 'Confirm',
  done: 'Done',
};

export const kindHelp: Record<StepKind, string> = {
  choose_category: 'Offers the flow’s categories as buttons, or fixes one.',
  form: 'Asks for the fields the chosen category records, and questions of its own.',
  find: 'Finds something already here: by its code, by search, or from a list.',
  create: 'Files a new asset in the chosen category, with the form’s answers.',
  label: 'Prints its label, queues it, or shows it on screen.',
  change: 'Archives it, moves it, lends it, or changes its fields.',
  confirm: 'Shows what will happen, and asks to go ahead.',
  done: 'Says what was done, with Start again for the next person.',
};

/**
 * Whether a step is a screen that asks the person something, as the server
 * counts them for "step 2 of 3" (#166): not Done, and not one that does its
 * work as soon as it is reached.
 */
export function asks(step: Step): boolean {
  switch (step.kind) {
    case 'done':
    case 'label':
    case 'change':
      return false;
    case 'choose_category':
      return !step.fixed;
    case 'create':
      return step.location?.mode === 'scan';
    default:
      return true;
  }
}

/**
 * Where a step comes among the steps that ask something, as the editor
 * previews it: before any answer, so a step with a condition counts as if
 * it held. Null for a step that asks nothing.
 */
export function previewProgress(steps: readonly Step[], i: number): Progress | null {
  if (!steps[i] || !asks(steps[i])) return null;
  const asking = steps.filter(asks);
  return { step: asking.indexOf(steps[i]) + 1, of: asking.length };
}

/** The kinds an editor offers to add, in order: Done is always last, and comes with the flow. */
export const addableKinds: StepKind[] = ['choose_category', 'form', 'find', 'create', 'label', 'change', 'confirm'];

const stepID = /^[a-z][a-z0-9_]{0,31}$/;

/** An ID for a new step of a kind, unique among these. */
export function newStepID(kind: StepKind, steps: readonly Step[]): string {
  const base = { choose_category: 'kind', form: 'form', find: 'find', create: 'file', label: 'label', change: 'change', confirm: 'confirm', done: 'done' }[kind];
  const taken = new Set(steps.map((s) => s.id));
  if (!taken.has(base)) return base;
  for (let n = 2; ; n++) if (!taken.has(`${base}_${n}`)) return `${base}_${n}`;
}

/** A new step of a kind, as it starts. */
export function newStep(kind: StepKind, steps: readonly Step[]): Step {
  const id = newStepID(kind, steps);
  switch (kind) {
    case 'form':
      return { id, kind, title: 'About it', ask_name: true, name_label: 'What is it?' };
    case 'find':
      return { id, kind, title: 'Which is it?', find_by: ['scan', 'search', 'list'] };
    case 'create':
      return { id, kind, name: '{name}' };
    case 'label':
      return { id, kind, label: { mode: 'show' } };
    case 'change':
      return { id, kind, changes: [{ kind: 'archive' }] };
    case 'confirm':
      return { id, kind, title: 'Is this right?' };
    default:
      return { id, kind };
  }
}

/** Steps with a new one added before Done, which stays last. */
export function addStep(steps: readonly Step[], kind: StepKind): Step[] {
  const at = steps.length && steps[steps.length - 1]!.kind === 'done' ? steps.length - 1 : steps.length;
  const out = [...steps];
  out.splice(at, 0, newStep(kind, steps));
  return out;
}

/** Steps with one moved up (-1) or down (+1); Done does not move, nor does anything pass it. */
export function moveStep(steps: readonly Step[], i: number, by: -1 | 1): Step[] {
  const j = i + by;
  if (j < 0 || j >= steps.length || steps[i]!.kind === 'done' || steps[j]!.kind === 'done') return [...steps];
  const out = [...steps];
  [out[i], out[j]] = [out[j]!, out[i]!];
  return out;
}

/**
 * What stops a flow being saved that the editor can tell without the
 * server: the shape of its steps, and that each has what it needs from
 * those before it.
 */
export function checkFlow(d: Definition): FlowProblem[] {
  const out: FlowProblem[] = [];
  if (!d.category_ids.length) out.push({ message: 'Choose at least one category to file into or take from.' });
  if (!d.steps.length || d.steps[d.steps.length - 1]!.kind !== 'done') out.push({ message: 'A flow ends with a Done step.' });
  let category = d.category_ids.length === 1;
  let asset = false;
  const ids = new Set<string>();
  d.steps.forEach((s, i) => {
    const bad = (message: string) => out.push({ step: s.id, message });
    if (!stepID.test(s.id)) bad('Its name is 1 to 32 lowercase letters, digits and _, starting with a letter.');
    else if (ids.has(s.id)) bad(`Another step is named ${s.id}.`);
    ids.add(s.id);
    switch (s.kind) {
      case 'choose_category':
        if (s.fixed && !d.category_ids.includes(s.fixed)) bad('Its fixed category is not one the flow uses.');
        category = true;
        break;
      case 'form':
        if (!category) bad('It needs a category: add Choose category before it, or give the flow only one.');
        for (const q of s.questions ?? []) {
          if (!/^[a-z][a-z0-9_]{0,63}$/.test(q.key)) bad(`A question's key is lowercase letters, digits and _: “${q.key}”.`);
          if (!q.label.trim()) bad(`The question ${q.key} needs a label.`);
        }
        break;
      case 'find':
        if (!s.find_by?.length) bad('Choose at least one way to find: scan, search or list.');
        asset = true;
        break;
      case 'create':
        if (!category) bad('It needs a category: add Choose category before it, or give the flow only one.');
        if (!s.name?.trim()) bad('It needs a name for the new asset, such as {name}.');
        if (s.location?.mode === 'fixed' && !s.location.asset_id) bad('Choose the place it is put.');
        asset = true;
        break;
      case 'label':
        if (!asset) bad('It needs something found or made in an earlier step.');
        if (s.label?.mode === 'print' && !s.label.destination) bad('Choose the printer or pool it prints to.');
        break;
      case 'change':
        if (!asset) bad('It needs something found or made in an earlier step.');
        if (!s.changes?.length) bad('Add at least one change.');
        for (const c of s.changes ?? []) {
          if (c.kind === 'place' && !c.asset_id) bad('Choose the place it moves to.');
          if (c.kind === 'category' && !c.category_id) bad('Choose the category it moves to.');
          if (c.kind === 'action' && !c.action) bad('Choose the action to press.');
          if (c.kind === 'fields' && !Object.keys(c.fields ?? {}).length) bad('Choose at least one field to set.');
        }
        break;
      case 'done':
        if (i !== d.steps.length - 1) bad('Done is the last step.');
        break;
    }
  });
  return out;
}

/** The problems that belong to a step, or to the flow as a whole (no step). */
export function problemsOf(problems: readonly FlowProblem[], step?: string): string[] {
  return problems.filter((p) => (p.step ?? '') === (step ?? '')).map((p) => p.message);
}

/** An address made from a name: lowercase letters, digits and -. */
export function slugFrom(name: string): string {
  return name
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64);
}
