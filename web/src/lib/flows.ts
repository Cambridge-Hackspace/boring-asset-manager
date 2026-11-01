// Flows (docs/flows.md): guided pages for doing one everyday thing with
// assets, run one step at a time on the server.

import { ApiError, request } from './api';
import { scanTarget } from './scan';

/** The version of the definition this interface reads and writes (#174). */
export const definitionVersion = 2;

export type StepKind = 'choose_category' | 'form' | 'set_fields' | 'find' | 'create' | 'label' | 'change' | 'confirm' | 'done';

export interface Condition {
  value: string;
  op: 'is' | 'is_not' | 'set' | 'unset';
  to?: string;
}

export type QuestionType = 'text' | 'long_text' | 'number' | 'date' | 'yes_no' | 'choice' | 'photo';

/** The types of question, in the order the editor offers them. */
export const questionTypes: [QuestionType, string][] = [
  ['text', 'Text'],
  ['long_text', 'Long text'],
  ['number', 'Number'],
  ['date', 'Date'],
  ['yes_no', 'Yes or no'],
  ['choice', 'A choice'],
  ['photo', 'A photo'],
];

/** One choice of a choice question: its value, the answer, and how it is shown (as the value, when empty). */
export interface Option {
  value: string;
  label?: string;
}

/** One thing a Form asks; its answer is the run's value of its key. */
export interface Question {
  key: string;
  label: string;
  help?: string;
  type?: QuestionType | '';
  options?: Option[];
  default?: string;
  required?: boolean;
}

/** One row of a Set fields step: an asset field, by key, and the value to give it, a template. */
export interface FieldValue {
  field: string;
  value: string;
}

export interface FlowLabel {
  mode: 'print' | 'queue' | 'show';
  destination?: string;
  template_id?: string;
}

export type ChangeKind = 'archive' | 'condition' | 'borrow' | 'return' | 'place' | 'category' | 'hook';

export interface Change {
  kind: ChangeKind;
  condition?: string;
  note?: string;
  asset_id?: string;
  category_id?: string;
  hook?: string;
}

export interface Step {
  id: string;
  kind: StepKind;
  title?: string;
  text?: string;
  when?: Condition | null;
  categories?: string[];
  fixed?: string;
  questions?: Question[];
  /** Set fields: the fields to set, and the asset's photo, a photo question's answer as {photo}. */
  fields?: FieldValue[];
  photo?: string;
  find_by?: ('scan' | 'search' | 'list')[];
  filed_by?: string;
  match?: { field: string; value: string } | null;
  name?: string;
  location?: { mode: 'fixed' | 'scan'; asset_id?: string } | null;
  label?: FlowLabel | null;
  changes?: Change[];
}

export interface Definition {
  version: number;
  description?: string;
  category_ids: string[];
  steps: Step[];
  /** The key of a date field: what the flow files is overdue once it has passed. */
  leave_until?: string;
}

/** Something a flow filed that is past its date to collect by. */
export interface Overdue {
  asset_id: string;
  name: string;
  until: string;
}

export interface Flow {
  id: string;
  slug: string;
  name: string;
  enabled: boolean;
  definition: Definition;
  created_by?: string;
  created_at: string;
  updated_at: string;
}

export interface Starter {
  key: string;
  name: string;
  description: string;
  definition: Definition;
}

export interface FlowProblem {
  step?: string;
  message: string;
}


export interface FlowAsset {
  id: string;
  name: string;
  code?: string;
  categories: string[];
  /** Set, to its date, when it is past a flow's date to collect by. */
  overdue?: string;
}

export interface FlowDone {
  step: string;
  kind: StepKind;
  said: string;
}

export interface Prompt {
  step: string;
  kind: StepKind;
  title?: string;
  text?: string;
  categories?: { id: string; name: string; color_hex: string }[];
  questions?: Question[];
  find_by?: ('scan' | 'search' | 'list')[];
  assets?: FlowAsset[];
  ask_place?: boolean;
  coming?: string[];
  done?: FlowDone[];
  asset?: FlowAsset;
  show_label?: boolean;
  label_template_id?: string;
}

export interface Run {
  id: string;
  flow_id: string;
  step: number;
  values: Record<string, string>;
  done: FlowDone[];
  finished: boolean;
  /** Photos Set fields put on assets: the page holding each adds it, once the asset exists. */
  photos?: FlowPhoto[];
}

/** A photo question's answer a Set fields step put on an asset; no asset_id until it is filed. */
export interface FlowPhoto {
  question: string;
  asset_id?: string;
}

export interface RunView {
  run: Run;
  flow: string;
  prompt: Prompt;
  /** Why the last answer was refused; the run is as it was. */
  error?: string;
  /** How far along it is (#166): left out on Done, and on a step that asks nothing and failed. */
  progress?: Progress;
}

/** "Step 2 of 3": the steps that ask something, answered and this one, of those and the ones ahead. */
export interface Progress {
  step: number;
  of: number;
}

/** An answer to the step a run is on, naming it. */
export interface Answer {
  step: string;
  category_id?: string;
  /** A form's answers, by its questions' keys. */
  answers?: Record<string, string>;
  asset_id?: string;
  code?: string;
  place_code?: string;
}

/**
 * Sends a request about a run. A refused answer still comes back with the
 * run as it is, and why; anything else refused is thrown.
 */
export async function runRequest(method: 'GET' | 'POST', path: string, body?: unknown): Promise<RunView> {
  try {
    return await request<RunView>(method, path, body);
  } catch (err) {
    if (!(err instanceof ApiError)) throw err;
    const view = err.body as RunView | undefined;
    if (view?.run && view.prompt) return { ...view, error: view.error ?? err.message };
    throw err;
  }
}

/** A flow to start, as an asset or scan page offers it. */
export interface FlowLink {
  slug: string;
  name: string;
}

/** The address of a flow's page, started from an asset when one is given. */
export function flowHref(slug: string, assetID?: string): string {
  return `/f/${encodeURIComponent(slug)}${assetID ? `?asset=${encodeURIComponent(assetID)}` : ''}`;
}

/** The address of a flow's runs, or of one of them. */
export function runsPath(slug: string, runID?: string): string {
  const base = `/flows/${encodeURIComponent(slug)}/runs`;
  return runID ? `${base}/${encodeURIComponent(runID)}` : base;
}

/**
 * What a scanned code names, as Find and Create take it: the code on a
 * label, or the code or asset ID in a scan link a QR code holds.
 */
export function scannedCode(text: string): string | null {
  const to = scanTarget(text);
  if (!to) return null;
  const last = to.slice(to.lastIndexOf('/') + 1);
  try {
    return decodeURIComponent(last);
  } catch {
    return last;
  }
}

/** A form's first answers: each question's default, as the server rendered it; a photo has none. */
export function formDefaults(p: Prompt): Record<string, string> {
  const answers: Record<string, string> = {};
  for (const q of p.questions ?? []) answers[q.key] = q.type === 'photo' ? '' : (q.default ?? '');
  return answers;
}

/** The input type a question is asked with, where a box asks it. */
export function inputType(type: string | undefined): 'text' | 'number' | 'date' {
  return type === 'number' || type === 'date' ? type : 'text';
}

/** What is missing from a form before it is sent: the labels of required answers left empty. */
export function missing(p: Prompt, answers: Record<string, string>): string[] {
  return (p.questions ?? []).filter((q) => q.required && !(answers[q.key] ?? '').trim()).map((q) => q.label);
}

/** How a choice is shown. */
export function optionLabel(o: Option): string {
  return o.label || o.value;
}

/**
 * The photos to add now: those the run put on an asset that exists, of
 * the photo questions this page holds a photo for, not added already
 * (named question:asset in added).
 */
export function photosToAdd(run: Run, held: Record<string, File>, added: ReadonlySet<string>): { key: string; assetID: string; file: File }[] {
  const out: { key: string; assetID: string; file: File }[] = [];
  for (const p of run.photos ?? []) {
    const key = `${p.question}:${p.asset_id ?? ''}`;
    const file = held[p.question];
    if (p.asset_id && file && !added.has(key)) out.push({ key, assetID: p.asset_id, file });
  }
  return out;
}
