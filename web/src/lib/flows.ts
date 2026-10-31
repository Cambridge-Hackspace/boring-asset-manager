// Flows (README §6.7): guided pages for doing one everyday thing with
// assets, run one step at a time on the server.

import { ApiError, request } from './api';
import { scanTarget } from './scan';

export type StepKind = 'choose_category' | 'form' | 'find' | 'create' | 'label' | 'change' | 'confirm' | 'done';

export interface Condition {
  value: string;
  op: 'is' | 'is_not' | 'set' | 'unset';
  to?: string;
}

export interface FieldSetting {
  key: string;
  hidden?: boolean;
  label?: string;
  help?: string;
  default?: string;
  required?: boolean;
}

export interface Question {
  key: string;
  label: string;
  help?: string;
  type?: 'text' | 'number' | 'date' | '';
  default?: string;
  required?: boolean;
}

export interface FlowLabel {
  mode: 'print' | 'queue' | 'show';
  destination?: string;
  template_id?: string;
}

export type ChangeKind = 'archive' | 'condition' | 'borrow' | 'return' | 'place' | 'category' | 'fields' | 'action';

export interface Change {
  kind: ChangeKind;
  condition?: string;
  note?: string;
  asset_id?: string;
  category_id?: string;
  fields?: Record<string, string>;
  action?: string;
}

export interface Step {
  id: string;
  kind: StepKind;
  title?: string;
  text?: string;
  when?: Condition | null;
  categories?: string[];
  fixed?: string;
  fields?: FieldSetting[];
  ask_name?: boolean;
  name_label?: string;
  ask_photo?: boolean;
  questions?: Question[];
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

export interface FlowField {
  key: string;
  label: string;
  help?: string;
  type: 'text' | 'number' | 'date' | 'url' | 'select';
  options?: { choices?: { label: string; value: string }[]; default?: string | null } | null;
  default?: string;
  required: boolean;
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
  fields?: FlowField[];
  ask_name?: boolean;
  name_label?: string;
  ask_photo?: boolean;
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
  name?: string;
  fields?: Record<string, string>;
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

/** A form's first values: each field's and question's default, as the server rendered it. */
export function formDefaults(p: Prompt): { fields: Record<string, string>; answers: Record<string, string> } {
  const fields: Record<string, string> = {};
  for (const f of p.fields ?? []) fields[f.key] = f.default ?? f.options?.default ?? '';
  const answers: Record<string, string> = {};
  for (const q of p.questions ?? []) answers[q.key] = q.default ?? '';
  return { fields, answers };
}

/** The input type a field or question is asked with. */
export function inputType(type: string | undefined): 'text' | 'number' | 'date' | 'url' {
  return type === 'number' || type === 'date' || type === 'url' ? type : 'text';
}

/** What is missing from a form before it is sent: the labels of required answers left empty. */
export function missing(p: Prompt, name: string, fields: Record<string, string>, answers: Record<string, string>): string[] {
  const out: string[] = [];
  if (p.ask_name && !name.trim()) out.push(p.name_label || 'Its name');
  for (const f of p.fields ?? []) if (f.required && !(fields[f.key] ?? '').trim()) out.push(f.label);
  for (const q of p.questions ?? []) if (q.required && !(answers[q.key] ?? '').trim()) out.push(q.label);
  return out;
}
