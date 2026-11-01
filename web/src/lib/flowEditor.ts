// Editing flows (README §6.7): what each kind of step is called and starts
// as, and the checks the editor makes as you go. The server checks a flow
// again, fully, when it is saved.

import type { Definition, FlowProblem, Progress, Question, Step, StepKind } from './flows';
import type { Field } from './models';

export const kindLabels: Record<StepKind, string> = {
  choose_category: 'Choose category',
  form: 'Form',
  set_fields: 'Set fields',
  find: 'Find',
  create: 'Create',
  label: 'Label',
  change: 'Change',
  confirm: 'Confirm',
  done: 'Done',
};

export const kindHelp: Record<StepKind, string> = {
  choose_category: 'Offers the flow’s categories as buttons, or fixes one.',
  form: 'Asks questions of its own; later steps use the answers.',
  set_fields: 'Puts answers, or other values, into the asset’s fields.',
  find: 'Finds something already here: by its code, by search, or from a list.',
  create: 'Files a new asset in the chosen category, with the fields set before it.',
  label: 'Prints its label, queues it, or shows it on screen.',
  change: 'Archives it, moves it, lends it, or triggers a hook.',
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
    case 'set_fields':
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
export const addableKinds: StepKind[] = ['choose_category', 'form', 'set_fields', 'find', 'create', 'label', 'change', 'confirm'];

const stepID = /^[a-z][a-z0-9_]{0,31}$/;
const questionKey = /^[a-z][a-z0-9_]{0,63}$/;

/** What a new step's ID is made from, by kind, as the server makes them (flows.NewStepID). */
const stepBases: Record<StepKind, string> = {
  choose_category: 'kind',
  form: 'form',
  set_fields: 'set',
  find: 'find',
  create: 'file',
  label: 'label',
  change: 'change',
  confirm: 'confirm',
  done: 'done',
};

/** An ID for a new step of a kind, unique among these. */
export function newStepID(kind: StepKind, steps: readonly Step[]): string {
  const base = stepBases[kind];
  const taken = new Set(steps.map((s) => s.id));
  if (!taken.has(base)) return base;
  for (let n = 2; ; n++) if (!taken.has(`${base}_${n}`)) return `${base}_${n}`;
}

/**
 * Steps each with an ID of its own (#171): those they have are kept, so
 * runs under way still match their answers to their steps, and any step
 * without one, or with one another step took first, is given a new one
 * from its kind. People never see or type them.
 */
export function withStepIDs(steps: readonly Step[]): Step[] {
  const kept = new Set<string>();
  const fine = steps.map((s) => {
    const ok = stepID.test(s.id) && !kept.has(s.id);
    if (ok) kept.add(s.id);
    return ok;
  });
  const out: Step[] = [];
  steps.forEach((s, i) => {
    if (fine[i]) return out.push(s);
    const id = newStepID(s.kind, [...steps.filter((_, j) => fine[j]), ...out]);
    kept.add(id);
    out.push({ ...s, id });
  });
  return out;
}

/** A new step of a kind, as it starts. */
export function newStep(kind: StepKind, steps: readonly Step[]): Step {
  const id = newStepID(kind, steps);
  switch (kind) {
    case 'form':
      return { id, kind, title: 'About it', questions: [{ key: 'name', label: 'What is it?', type: 'text', required: true }] };
    case 'set_fields':
      return { id, kind, fields: [] };
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

/** What a Set fields step sets, by where it is: the new asset, the asset found or filed, or nothing. */
export type SetsOn = 'new' | 'found' | 'filed' | '';

/**
 * What the Set fields step at i sets, as the server decides: the new asset
 * the next Create files, when a Create comes after it before any Find; or
 * else the asset the nearest Find or Create before it found or filed.
 */
export function setsOn(steps: readonly Step[], i: number): SetsOn {
  for (const s of steps.slice(i + 1)) {
    if (s.kind === 'create') return 'new';
    if (s.kind === 'find') break;
  }
  for (let j = i - 1; j >= 0; j--) {
    if (steps[j]!.kind === 'find') return 'found';
    if (steps[j]!.kind === 'create') return 'filed';
  }
  return '';
}

/** What a Set fields step says it sets, by where it is. */
export const setsOnText: Record<SetsOn, string> = {
  new: 'On the new asset, as it is filed',
  found: 'On the asset found',
  filed: 'On the asset filed',
  '': 'On nothing yet: put it before a Create, or after a Find or Create',
};

/** Why an answer does not suit a question, as the server says it, or empty when it does. */
export function answerProblem(q: Question, answer: string): string {
  if (!answer) return '';
  switch (q.type) {
    case 'number':
      return answer.trim() === '' || Number.isNaN(Number(answer)) ? 'is a number' : '';
    case 'date':
      return isDate(answer) ? '' : 'is a date, as 2026-10-30';
    case 'yes_no':
      return answer === 'yes' || answer === 'no' ? '' : 'is yes or no';
    case 'choice':
      return (q.options ?? []).some((o) => o.value === answer) ? '' : 'is one of its choices';
  }
  return '';
}

function isDate(s: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(s)) return false;
  const d = new Date(`${s}T00:00:00Z`);
  return !Number.isNaN(d.getTime()) && d.toISOString().startsWith(s);
}

/** Why a fixed value does not suit a field, or empty when it does, as asset fields check them. */
export function fieldValueProblem(f: Field, value: string): string {
  if (!value) return '';
  switch (f.validation_type) {
    case 'number':
      return value.trim() === '' || Number.isNaN(Number(value)) ? 'is not a number' : '';
    case 'date':
      return /^\d{4}-\d{2}-\d{2}/.test(value) && !Number.isNaN(new Date(value).getTime()) ? '' : 'is not a date';
    case 'url':
      return /^[a-z][a-z0-9+.-]*:\/\/[^/\s]+/i.test(value) ? '' : 'is not a full address';
    case 'select':
      return (f.options?.choices ?? []).some((c) => c.value === value) ? '' : 'is not one of its choices';
  }
  return '';
}

/** The values a template names, in order. */
export function references(template: string | undefined): string[] {
  return [...(template ?? '').matchAll(/\{([a-z][a-z0-9_]*)\}/g)].map((m) => m[1]!);
}

/**
 * What the editor knows of the flow's categories, for checking Set fields
 * as the server does: the fields any of them records, and, by category,
 * its name and the keys of the fields it requires.
 */
export interface FlowCatalog {
  fields: Field[];
  required: Record<string, string[]>;
  names: Record<string, string>;
}

/**
 * What stops a flow being saved that the editor can tell without the
 * server: the shape of its steps, that each has what it needs from those
 * before it, and, given its categories' fields, what Set fields sets.
 */
export function checkFlow(d: Definition, catalog?: FlowCatalog): FlowProblem[] {
  const out: FlowProblem[] = [];
  if (!d.category_ids.length) out.push({ message: 'Choose at least one category to file into or take from.' });
  if (!d.steps.length || d.steps[d.steps.length - 1]!.kind !== 'done') out.push({ message: 'A flow ends with a Done step.' });
  let category = d.category_ids.length === 1;
  let asset = false;
  let filing = d.category_ids;
  // What earlier steps set, for the values Set fields names; the photo
  // questions asked; and what Set fields gives the next Create.
  const set = new Set(['today', 'now', 'user', 'flow', ...(category ? ['category', 'category_id'] : [])]);
  let fieldsKnown = false;
  const photos = new Set<string>();
  let pending: { when: Step['when']; keys: string[] }[] = [];
  const byKey = new Map((catalog?.fields ?? []).map((f) => [f.key, f]));
  const label = (key: string) => byKey.get(key)?.label ?? key;
  const ids = new Set<string>();
  d.steps.forEach((s, i) => {
    const bad = (message: string) => out.push({ step: s.id, message });
    // The editor gives each step its ID (withStepIDs), so these are for a
    // flow made some other way.
    if (!stepID.test(s.id)) bad('This step has no usable ID: remove it and add it again.');
    else if (ids.has(s.id)) bad('This step has the same ID as another: remove it and add it again.');
    ids.add(s.id);
    switch (s.kind) {
      case 'choose_category':
        if (s.fixed && !d.category_ids.includes(s.fixed)) bad('Its fixed category is not one the flow uses.');
        filing = s.fixed ? [s.fixed] : s.categories?.length ? s.categories : d.category_ids;
        category = true;
        set.add('category').add('category_id');
        break;
      case 'form': {
        const seen = new Set<string>();
        for (const q of s.questions ?? []) {
          if (!questionKey.test(q.key)) bad(`A question's key is lowercase letters, digits and _: “${q.key}”.`);
          else if (seen.has(q.key)) bad(`The question ${q.key} is asked twice.`);
          seen.add(q.key);
          if (!q.label.trim()) bad(`The question ${q.key} needs a label.`);
          if (q.type === 'choice') {
            if (!q.options?.length) bad(`${q.label || q.key} needs choices to choose from.`);
            const values = new Set<string>();
            for (const o of q.options ?? []) {
              if (!o.value.trim()) bad(`Each choice of ${q.label || q.key} needs a value.`);
              else if (values.has(o.value)) bad(`${q.label || q.key} offers “${o.value}” twice.`);
              values.add(o.value);
            }
          }
          if (!references(q.default).length) {
            const p = answerProblem(q, q.default ?? '');
            if (p) bad(`The default of ${q.label || q.key} ${p}.`);
          }
        }
        for (const q of s.questions ?? []) {
          set.add(q.key);
          if (q.type === 'photo') photos.add(q.key);
        }
        break;
      }
      case 'set_fields': {
        const on = setsOn(d.steps, i);
        if (!on) bad('It sets fields on nothing: put it before a Create, or after a Find or Create.');
        if (!s.fields?.length && !s.photo) bad('Add at least one field to set.');
        const seen = new Set<string>();
        const keys: string[] = [];
        for (const row of s.fields ?? []) {
          if (!row.field) {
            bad('Choose the field each row sets.');
            continue;
          }
          if (catalog && !byKey.has(row.field)) bad(`No field the flow's categories record has the key “${row.field}”.`);
          if (seen.has(row.field)) bad(`It sets ${label(row.field)} twice.`);
          seen.add(row.field);
          keys.push(row.field);
          for (const ref of references(row.value)) {
            if (!set.has(ref) && !(fieldsKnown && byKey.has(ref))) bad(`The value of ${label(row.field)} names {${ref}}, which no earlier step sets.`);
          }
          const f = byKey.get(row.field);
          if (f && !references(row.value).length) {
            const p = fieldValueProblem(f, row.value);
            if (p) bad(`${f.label}: “${row.value}” ${p}.`);
          }
        }
        if (s.photo) {
          const m = /^\{([a-z][a-z0-9_]*)\}$/.exec(s.photo);
          if (!m || !photos.has(m[1]!)) bad('Its photo is the answer to a photo question asked before it.');
        }
        for (const k of keys) set.add(k);
        if (on === 'new') pending.push({ when: s.when, keys });
        break;
      }
      case 'find':
        if (!s.find_by?.length) bad('Choose at least one way to find: scan, search or list.');
        asset = fieldsKnown = true;
        set.add('asset').add('asset_id').add('code');
        break;
      case 'create': {
        if (!category) bad('It needs a category: add Choose category before it, or give the flow only one.');
        if (!s.name?.trim()) bad('It needs a name for the new asset, such as {name}.');
        if (s.location?.mode === 'fixed' && !s.location.asset_id) bad('Choose the place it is put.');
        if (catalog) {
          // Every field a category it may file into requires is set before
          // it, by a step with no condition or one only for that category.
          const w = s.when;
          const into = w && w.value === 'category_id' && w.op === 'is' && w.to ? [w.to] : filing;
          for (const cat of into) {
            const filled = new Set(
              pending.filter((p) => !p.when || (p.when.value === 'category_id' && p.when.op === 'is' && p.when.to === cat)).flatMap((p) => p.keys),
            );
            const left = (catalog.required[cat] ?? []).filter((k) => !filled.has(k)).map(label);
            if (left.length) bad(`${catalog.names[cat] ?? cat} requires ${left.join(', ')}, which no Set fields step before it sets.`);
          }
        }
        pending = [];
        asset = fieldsKnown = true;
        set.add('asset').add('asset_id').add('code');
        break;
      }
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
          if (c.kind === 'hook' && !c.hook) bad('Choose the hook to trigger.');
        }
        break;
      case 'done':
        if (i !== d.steps.length - 1) bad('Done is the last step.');
        break;
    }
  });
  return out;
}

/** Whether a template, such as {owner}: {what}, names a value. */
function names(template: string | undefined, key: string): boolean {
  return (template ?? '').includes(`{${key}}`);
}

/**
 * Which of these steps name a value, by its key: in a template (a heading,
 * text, name, default, field's value, photo or match) or in the condition
 * they are shown on. Renaming a question's key leaves them naming what no
 * step sets.
 */
export function stepsUsing(steps: readonly Step[], key: string): Step[] {
  if (!key) return [];
  return steps.filter(
    (s) =>
      s.when?.value === key ||
      [s.title, s.text, s.name, s.photo, s.match?.value].some((t) => names(t, key)) ||
      (s.fields ?? []).some((f) => names(f.value, key)) ||
      (s.questions ?? []).some((q) => names(q.default, key)) ||
      (s.changes ?? []).some((c) => names(c.note, key)),
  );
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
