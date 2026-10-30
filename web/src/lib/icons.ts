// Category icons (README §3.7): which icon a category shows, the names
// and starting sources the category page offers, and where the
// compiler's diagnostics belong.

import type { Category, CategoryIcon, IconDiag, Rendition } from './models';
import type { Definition, Problem } from './isolex/syntax';

/** Whether a name is a wrapper's, drawing an icon with params set to assets' fields: the build's, not anyone's to choose. */
export const isWrapper = (name: string) => name.startsWith('bam--');

/** The params an icon has, its own and those it inherits, root first, from what the sources define. */
export function iconParams(definitions: Definition[], name: string): string[] {
  const chain: Definition[] = [];
  const seen = new Set<string>();
  for (let at: string | undefined = name; at && !seen.has(at); ) {
    seen.add(at);
    const def = definitions.find((d) => d.name === at && (d.kind === 'icon' || d.kind === 'base'));
    if (!def) break;
    chain.unshift(def);
    at = def.extends;
  }
  const params: string[] = [];
  for (const d of chain) for (const p of d.params) if (!params.includes(p)) params.push(p);
  return params;
}

/** Those params of an icon, its own or inherited, that are text. */
export function iconTextParams(definitions: Definition[], name: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (let at: string | undefined = name; at && !seen.has(at); ) {
    seen.add(at);
    const def = definitions.find((d) => d.name === at && (d.kind === 'icon' || d.kind === 'base'));
    if (!def) break;
    for (const p of def.textParams ?? []) if (!out.includes(p)) out.push(p);
    at = def.extends;
  }
  return out;
}

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

/** The icon a category shows for a rendition: its own, published, or else
    the nearest category above it with one; with the category it comes
    from. With none for labels, labels show the screen icon. */
export function shownIcon(categories: Category[], icons: CategoryIcon[], id: string, rendition: Rendition = 'screen'): { name: string; from: string; rendition: Rendition } | null {
  const shown = shownFor(categories, icons, id, rendition);
  return shown || rendition === 'screen' ? shown : shownFor(categories, icons, id, 'screen');
}

function shownFor(categories: Category[], icons: CategoryIcon[], id: string, rendition: Rendition): { name: string; from: string; rendition: Rendition } | null {
  const own = new Map(icons.filter((i) => i.source !== null && (i.rendition ?? 'screen') === rendition).map((i) => [i.category_id, i.icon_name]));
  const parent = new Map(categories.map((c) => [c.id, c.parent_id ?? null]));
  const seen = new Set<string>();
  for (let at: string | null = id; at && !seen.has(at); at = parent.get(at) ?? null) {
    seen.add(at);
    const name = own.get(at);
    if (name) return { name, from: at, rendition };
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

/** Stroke widths as typed, separated by commas or spaces: the numbers, or
    why they are not. */
export function parseWidths(text: string): { widths: number[] } | { error: string } {
  const parts = text.split(/[\s,]+/u).filter((p) => p !== '');
  const widths: number[] = [];
  for (const p of parts) {
    const n = Number(p);
    if (!Number.isFinite(n) || n <= 0) return { error: `${p} is not a positive number` };
    widths.push(n);
  }
  return { widths };
}

/** The compiler's limits a lexicon may set (isolex SPEC §9), with its defaults. */
export const iconLimits = [
  { key: 'shape-limit', label: 'Shapes in one icon', default: 100_000 },
  { key: 'repeat-limit', label: 'Repetitions in one icon', default: 1_000_000 },
  { key: 'insert-depth', label: 'Fragments inserted within each other', default: 32 },
  { key: 'icon-limit', label: 'Icons in the lexicon', default: 100_000 },
  { key: 'expression-depth', label: 'Nesting of one expression', default: 256 },
] as const;

/** Who owns an icon of the build: the category whose icon it is, or a
    variant of. */
export function iconOwner(icons: CategoryIcon[], name: string): CategoryIcon | undefined {
  return (
    icons.find((i) => i.icon_name === name) ??
    icons.filter((i) => name.startsWith(i.icon_name + '-')).sort((a, b) => b.icon_name.length - a.icon_name.length)[0]
  );
}
