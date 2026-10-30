// Packages (README §2.3, #144): part of the instance, chosen as an
// installer's components are, with what it needs ticked and marked, then
// downloaded by the browser as the server writes it.

import { request } from './api';

/** What a package can be chosen from, each by ID. */
export interface Selection {
  categories?: string[];
  fields?: string[];
  actions?: string[];
  relation_types?: string[];
  label_templates?: string[];
  scan_templates?: string[];
  reports?: string[];
  flows?: string[];
  scopes?: string[];
  icons?: boolean;
  assets?: { all: boolean; photos: boolean; links: boolean; history: boolean };
}

export type ItemKind =
  | 'category'
  | 'field'
  | 'action'
  | 'relation_type'
  | 'label_template'
  | 'scan_template'
  | 'report'
  | 'flow'
  | 'scope'
  | 'icon_library_file'
  | 'image'
  | 'printer'
  | 'printer_pool'
  | 'asset'
  | 'icons'
  | 'assets'
  | 'category_icon'
  | 'palette_token';

export interface Ref {
  kind: ItemKind;
  id: string;
  name: string;
}

export interface PlanItem extends Ref {
  chosen: boolean;
  needed_by: Ref[];
}

export interface Plan {
  items: PlanItem[];
  assets?: { assets: number; photos: number; links: number; events: number; unreadable: number };
  left_out: (Ref & { needed_by: Ref[]; reason: string })[];
}

/** The kinds a selection lists, each with its selection's key, in the order the page shows them. */
export const groups: { kind: ItemKind; key: keyof Selection; label: string }[] = [
  { kind: 'field', key: 'fields', label: 'Fields' },
  { kind: 'action', key: 'actions', label: 'Actions' },
  { kind: 'relation_type', key: 'relation_types', label: 'Link types' },
  { kind: 'label_template', key: 'label_templates', label: 'Label templates' },
  { kind: 'scan_template', key: 'scan_templates', label: 'Scan templates' },
  { kind: 'report', key: 'reports', label: 'Reports' },
  { kind: 'flow', key: 'flows', label: 'Flows' },
  { kind: 'scope', key: 'scopes', label: 'Scopes' },
];

/** Plural names of what a plan lists, for what needs what and what stays behind. */
export const kindNames: Record<ItemKind, string> = {
  category: 'category',
  field: 'field',
  action: 'action',
  relation_type: 'link type',
  label_template: 'label template',
  scan_template: 'scan template',
  report: 'report',
  flow: 'flow',
  scope: 'scope',
  icon_library_file: 'icon library file',
  image: 'image',
  printer: 'printer',
  printer_pool: 'printer pool',
  asset: 'asset',
  icons: 'icons',
  assets: 'assets',
  category_icon: 'category’s icon',
  palette_token: 'palette color',
};

/** A kind's name with its article: "a field", "an icon library file". */
export function aKind(kind: ItemKind): string {
  const name = kindNames[kind];
  return `${/^[aeiou]/.test(name) ? 'an' : 'a'} ${name}`;
}

export interface TreeNode {
  id: string;
  name: string;
  children: TreeNode[];
}

/** The categories as a tree, each level by name. */
export function categoryTree(categories: { id: string; name: string; parent_id?: string | null }[]): TreeNode[] {
  const nodes = new Map(categories.map((c) => [c.id, { id: c.id, name: c.name, children: [] as TreeNode[] }]));
  const roots: TreeNode[] = [];
  for (const c of categories) {
    const node = nodes.get(c.id)!;
    const parent = c.parent_id ? nodes.get(c.parent_id) : undefined;
    (parent ? parent.children : roots).push(node);
  }
  const sort = (list: TreeNode[]) => {
    list.sort((a, b) => a.name.localeCompare(b.name));
    list.forEach((n) => sort(n.children));
  };
  sort(roots);
  return roots;
}

/** A node and every node under it. */
export function subtreeIds(node: TreeNode): string[] {
  return [node.id, ...node.children.flatMap(subtreeIds)];
}

/**
 * Ticks or unticks a category with everything under it, as an installer's
 * component list does, returning the categories chosen.
 */
export function toggleSubtree(chosen: string[], node: TreeNode, on: boolean): string[] {
  const ids = new Set(subtreeIds(node));
  const rest = chosen.filter((id) => !ids.has(id));
  return on ? [...rest, ...ids] : rest;
}

/** Whether a category and those under it are chosen: all, some or none. */
export function treeState(chosen: string[], node: TreeNode): 'all' | 'some' | 'none' {
  const ids = subtreeIds(node);
  const n = ids.filter((id) => chosen.includes(id)).length;
  return n === 0 ? 'none' : n === ids.length ? 'all' : 'some';
}

/** What the plan holds besides what was chosen, by kind:id, with what needs each. */
export function needed(plan: Plan | null): Map<string, Ref[]> {
  const out = new Map<string, Ref[]>();
  for (const it of plan?.items ?? []) {
    if (!it.chosen && it.needed_by.length > 0) out.set(`${it.kind}:${it.id}`, it.needed_by);
  }
  return out;
}

/** Says what needs something: "Needed by Tools and Lend a tool". */
export function neededBySaid(by: Ref[]): string {
  const names = by.map((r) => r.name);
  if (names.length <= 1) return `Needed by ${names[0] ?? 'what you chose'}`;
  return `Needed by ${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
}

function count(n: number, one: string): string {
  return `${n.toLocaleString()} ${one}${n === 1 ? '' : 's'}`;
}

/** Says what a plan holds: "The package holds 4 things and 2 assets with 1 photo." */
export function planSummary(plan: Plan, withPhotos: boolean): string {
  const things = plan.items.filter((i) => i.kind !== 'assets' && i.kind !== 'icons').length;
  let s = `The package holds ${count(things, 'thing')}`;
  if (plan.assets) {
    s += ` and ${count(plan.assets.assets, 'asset')}`;
    if (withPhotos) s += ` with ${count(plan.assets.photos, 'photo')}`;
  }
  s += '.';
  const unseen = plan.assets?.unreadable ?? 0;
  if (unseen > 0) s += ` ${count(unseen, 'asset')} you cannot see ${unseen === 1 ? 'is' : 'are'} left out.`;
  return s;
}

/** Whether anything is chosen. */
export function anyChosen(sel: Selection): boolean {
  return groups.some((g) => ((sel[g.key] as string[] | undefined)?.length ?? 0) > 0) || (sel.categories?.length ?? 0) > 0 || !!sel.icons || !!sel.assets;
}

export function planPackage(selection: Selection): Promise<Plan> {
  return request<Plan>('POST', '/exports/plan', { selection });
}

export interface PackageInfo {
  name: string;
  description: string;
  author: string;
  license: string;
  version: string;
  /** A preview image, base64, or empty. */
  preview: string;
}

export function createExport(selection: Selection, info: PackageInfo): Promise<{ id: string; download: string; plan: Plan }> {
  return request('POST', '/exports', { selection, ...info, preview: info.preview || undefined });
}

/** Reads a file as base64, without its data: URL's prefix. */
export function base64Of(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).replace(/^data:[^,]*,/, ''));
    reader.onerror = () => reject(reader.error ?? new Error('Could not read the file'));
    reader.readAsDataURL(file);
  });
}

// Importing (README §2.3, #145): a package, or kinds of a backup, uploaded
// as backups are, checked to say what it would do, then imported as checked.

/** What an import does with one thing: what clashes with nothing is created. */
export type Choice = 'create' | 'existing' | 'copy' | 'skip' | 'replace';

export const choiceLabels: Record<Choice, string> = {
  create: 'Create',
  existing: 'Use the one here',
  copy: 'Import as a copy',
  skip: 'Skip',
  replace: 'Replace the one here',
};

export interface ImportItem extends Ref {
  action: Choice;
  /** Its key, name, address or path here, when that is not its own. */
  as?: string;
  /** What it clashes with here. */
  existing?: Ref;
  identical?: boolean;
  /** What may be chosen for it; none when nothing clashes. */
  choices?: Choice[];
  note?: string;
}

/** The kinds of a backup an import can merge. */
export const mergeable = ['catalog', 'labels', 'reports', 'icons', 'flows', 'assets'] as const;
export type MergeKind = (typeof mergeable)[number];

export interface ImportPlan {
  header: {
    type: string;
    created_at: string;
    kinds: string[];
    package?: { name: string; description?: string; author?: string; license?: string; version?: string };
  };
  kinds: string[];
  anchor: Ref | null;
  items: ImportItem[];
  assets?: { assets: number; photos: number; links: number; events: number; values_dropped: number; links_dropped: number; codes_taken: number };
  files: number;
  file_bytes: number;
  permissions: string[];
  missing: string[];
  warnings: string[];
  problems: string[];
  fingerprint: string;
}

export interface ImportRequest {
  upload_id: string;
  anchor: string;
  choices: Record<string, Choice>;
  kinds?: MergeKind[];
  fingerprint?: string;
}

/** What a check or an import came to: its plan, even when refused, and why it was. */
export interface ImportOutcome {
  plan: ImportPlan | null;
  error: string;
}

interface ImportJob {
  id: string;
  done: boolean;
  error?: string;
  result?: { plan: ImportPlan | null };
}

/** Checks an upload, or imports it as checked, waiting for the job. */
export async function importJob(
  path: '/import/check' | '/import',
  body: ImportRequest,
  opts: { sleep?: (ms: number) => Promise<void> } = {},
): Promise<ImportOutcome> {
  const sleep = opts.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
  let job = await request<ImportJob>('POST', path, body);
  for (let wait = 250; !job.done; wait = Math.min(wait * 2, 3000)) {
    await sleep(wait);
    job = await request<ImportJob>('GET', `/import/jobs/${encodeURIComponent(job.id)}`);
  }
  return { plan: job.result?.plan ?? null, error: job.error ?? '' };
}

/** The key a choice is made by. */
export function itemKey(it: Ref): string {
  return `${it.kind}:${it.id}`;
}

/** What clashes with something here, to choose for. */
export function clashes(plan: ImportPlan | null): ImportItem[] {
  return (plan?.items ?? []).filter((it) => (it.choices?.length ?? 0) > 0);
}

/** Says what an import does with one thing. */
export function actionSaid(it: ImportItem): string {
  switch (it.action) {
    case 'create':
      return it.as ? `Created as ${it.as}` : 'Created';
    case 'existing':
      return it.existing?.name ? `Uses ${it.existing.name}, here already` : 'Uses the one here already';
    case 'copy':
      return `Imported as a copy, ${it.as}`;
    case 'skip':
      return 'Skipped';
    case 'replace':
      return 'Replaces the one here';
  }
}

/** Says what a plan does: "Creates 4 things, uses 1 here already and copies 1; 2 assets with 1 photo." */
export function importSummary(plan: ImportPlan): string {
  const n = (a: Choice) => plan.items.filter((i) => i.action === a).length;
  const parts = [`creates ${count(n('create') + n('copy'), 'thing')}`];
  if (n('copy') > 0) parts[0] += ` (${n('copy').toLocaleString()} of them copies)`;
  if (n('replace') > 0) parts.push(`replaces ${n('replace').toLocaleString()}`);
  if (n('existing') > 0) parts.push(`uses ${n('existing').toLocaleString()} here already`);
  if (n('skip') > 0) parts.push(`skips ${n('skip').toLocaleString()}`);
  let s = `It ${parts.join(', ')}`;
  if (plan.assets) {
    s += `, and brings ${count(plan.assets.assets, 'asset')} with ${count(plan.assets.photos, 'photo')} and ${count(plan.assets.links, 'link')}`;
  }
  return s + '.';
}
