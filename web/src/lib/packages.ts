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
  | 'assets';

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
};

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
