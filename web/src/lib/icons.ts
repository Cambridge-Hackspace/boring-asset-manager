// Category icons (README §3.7): which icon a category shows, the names
// and starting sources the category page offers, and where the
// compiler's diagnostics belong.

import type { Category, CategoryIcon, IconDiag } from './models';
import type { Problem } from './isolex/syntax';

/** The file a category's icon is in a build, as diagnostics name it. */
export const iconFile = (name: string) => `categories/${name}.ilx`;

const nameRule = /^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,63}$/u;

/** Whether a name may be an icon's: as the server checks it. */
export function validIconName(name: string): boolean {
  return nameRule.test(name) && !name.startsWith('bam-');
}

/** A name for a category's first icon, from its own name, not taken. */
export function suggestIconName(categoryName: string, taken: Iterable<string>): string {
  let base = categoryName
    .normalize('NFKD')
    .replace(/[̀-ͯ]/gu, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/gu, '-')
    .replace(/^-+|-+$/gu, '')
    .slice(0, 56);
  if (!base || base.startsWith('bam-')) base = 'icon' + (base ? '-' + base : '');
  const used = new Set(taken);
  if (!used.has(base)) return base;
  for (let n = 2; ; n++) if (!used.has(`${base}-${n}`)) return `${base}-${n}`;
}

/** The icon a category shows: its own, published, or else the nearest
    category above it with one; with the category it comes from. */
export function shownIcon(categories: Category[], icons: CategoryIcon[], id: string): { name: string; from: string } | null {
  const own = new Map(icons.filter((i) => i.source !== null).map((i) => [i.category_id, i.icon_name]));
  const parent = new Map(categories.map((c) => [c.id, c.parent_id ?? null]));
  const seen = new Set<string>();
  for (let at: string | null = id; at && !seen.has(at); at = parent.get(at) ?? null) {
    seen.add(at);
    const name = own.get(at);
    if (name) return { name, from: at };
  }
  return null;
}

/** Diagnostics as an editor of one file shows them, and the rest. */
export function splitDiags(file: string, errors: IconDiag[], warnings: IconDiag[]): { here: Problem[]; elsewhere: (IconDiag & { severity: 'error' | 'warning' })[] } {
  const here: Problem[] = [];
  const elsewhere: (IconDiag & { severity: 'error' | 'warning' })[] = [];
  for (const [list, severity] of [[errors, 'error'], [warnings, 'warning']] as const) {
    for (const d of list) {
      if (d.file === file) here.push({ line: d.line, col: d.col, message: d.message, severity });
      else elsewhere.push({ ...d, severity });
    }
  }
  return { here, elsewhere };
}

const quote = (s: string) => '"' + s.replace(/\\/gu, '\\\\').replace(/"/gu, '\\"') + '"';

/** A source that draws an icon extending another, changing nothing yet. */
export function extendingSource(name: string, parent: string): string {
  return `icon ${quote(name)} {\n  extends ${quote(parent)}\n  // replace a part, or add one, to make it this category's own\n}\n`;
}

/** A source to start from: one box. */
export function boxSource(name: string, group: string): string {
  return `icon ${quote(name)} {\n  category ${quote(group)}\n  footprint 1 1 1\n  part "body" {\n    box 0 0 0 1 1 1\n  }\n}\n`;
}

/** Another icon's source, drawing it under this name instead. */
export function copiedSource(source: string, from: string, to: string): string {
  const pattern = new RegExp(`(\\b(?:icon|base)\\s+)"${from.replace(/[.*+?^${}()|[\]\\]/gu, '\\$&')}"`, 'u');
  return source.replace(pattern, `$1${quote(to)}`);
}

/** The group a category's icons are listed under: its top category's name. */
export function iconGroup(categories: Category[], id: string): string {
  const byId = new Map(categories.map((c) => [c.id, c]));
  let c = byId.get(id);
  const seen = new Set<string>();
  while (c?.parent_id && byId.has(c.parent_id) && !seen.has(c.id)) {
    seen.add(c.id);
    c = byId.get(c.parent_id);
  }
  return suggestIconName(c?.name ?? 'icons', []);
}
