// Graphs of followed links (docs/data-model.md#following-links): where the
// drawing puts each asset and link, and the same graph as a nested list, by
// hops, for the keyboard and screen readers. The server lays the graph out
// (each asset's layer and row); this places it in pixels.

import type { AssetGraph, GraphAsset } from './models';

/** Sizes in the drawing, in CSS pixels: boxes stay at least 44 high, as targets. */
export const BOX_W = 208;
export const BOX_H = 52;
export const COL_GAP = 72;
export const ROW_GAP = 16;
/** Room around the drawing, for links that bend out past a column. */
export const MARGIN = 40;

export interface Placed {
  width: number;
  height: number;
  /** Each asset's box's top left corner, by ID. */
  at: Map<string, { x: number; y: number }>;
}

/** Where each asset's box goes: its column, and its row with each column centered on the tallest. */
export function place(g: AssetGraph): Placed {
  const rows = new Map<number, number>();
  let cols = 0;
  for (const a of g.assets) {
    cols = Math.max(cols, a.layer + 1);
    rows.set(a.layer, Math.max(rows.get(a.layer) ?? 0, a.row + 1));
  }
  const tallest = Math.max(0, ...rows.values());
  const at = new Map<string, { x: number; y: number }>();
  for (const a of g.assets) {
    const offset = (tallest - (rows.get(a.layer) ?? 0)) / 2;
    at.set(a.id, { x: MARGIN + a.layer * (BOX_W + COL_GAP), y: MARGIN + (a.row + offset) * (BOX_H + ROW_GAP) });
  }
  return {
    width: cols ? 2 * MARGIN + cols * BOX_W + (cols - 1) * COL_GAP : 0,
    height: tallest ? 2 * MARGIN + tallest * BOX_H + (tallest - 1) * ROW_GAP : 0,
    at,
  };
}

/** The path of a link between two boxes: across between columns, or bending out to the right within one. */
export function linkPath(from: { x: number; y: number }, to: { x: number; y: number }): string {
  const fy = from.y + BOX_H / 2;
  const ty = to.y + BOX_H / 2;
  if (to.x > from.x) {
    const a = from.x + BOX_W;
    const mid = (a + to.x) / 2;
    return `M ${a} ${fy} C ${mid} ${fy}, ${mid} ${ty}, ${to.x} ${ty}`;
  }
  if (to.x < from.x) {
    const b = to.x + BOX_W;
    const mid = (from.x + b) / 2;
    return `M ${from.x} ${fy} C ${mid} ${fy}, ${mid} ${ty}, ${b} ${ty}`;
  }
  const r = from.x + BOX_W;
  const bend = Math.min(MARGIN, 16 + Math.abs(ty - fy) / 4);
  return `M ${r} ${fy} C ${r + bend} ${fy}, ${r + bend} ${ty}, ${r} ${ty}`;
}

/** What an asset's condition, or its being archived, says beside its name; empty when neither. */
export function assetNote(a: GraphAsset): string {
  const notes: string[] = [];
  if (a.condition === 'out_of_service') notes.push('Out of service');
  else if (a.condition === 'needs_maintenance') notes.push('Needs maintenance');
  if (a.archived) notes.push('Archived');
  if (a.cut) notes.push('More not shown');
  return notes.join(', ');
}

/** What the graph shows and what it leaves out, a line each, as the server's drawings say it. */
export function summary(g: AssetGraph): string[] {
  const out: string[] = [];
  const n = (k: number, one: string, many: string) => (k === 1 ? `1 ${one}` : `${k} ${many}`);
  if (g.root ? g.assets.length <= 1 : g.links.length === 0) out.push('No followed links.');
  if (g.hidden > 0) out.push(`${n(g.hidden, 'asset you may not see is', 'assets you may not see are')} left out, and not followed through.`);
  if (g.loops.length > 0) {
    const names = new Map(g.assets.map((a) => [a.id, a.name]));
    for (const loop of g.loops) out.push(`A loop: ${loop.map((id) => names.get(id) ?? '').join(' → ')}, and round again. Following stops where it comes back.`);
  }
  if (g.truncated) {
    out.push(g.truncated_at ? `Cut short at ${g.truncated_at} hops, at ${g.assets.length} assets.` : `Cut short at ${g.assets.length} assets.`);
  }
  return out;
}

/** An asset in the nested list, with those under it. */
export interface ListItem {
  asset: GraphAsset;
  /** Those it leads to, a hop further out, each listed once. */
  children: ListItem[];
  /** Others it is linked to that are listed elsewhere, by name. */
  also: string[];
}

/** One part of the nested list: its heading, and its assets. */
export interface ListPart {
  heading: string;
  items: ListItem[];
}

/**
 * The graph as a nested list. One asset's graph lists what it needs, then
 * what needs it, each by hops: under each asset, those one hop further it
 * links to. A map lists each asset, from the left of the drawing, with what
 * it needs under it. Every asset in the drawing is in the list.
 */
export function nested(g: AssetGraph): ListPart[] {
  const byId = new Map(g.assets.map((a) => [a.id, a]));
  const symmetric = new Set(g.types.filter((t) => t.is_symmetric).map((t) => t.id));
  const byName = (a: GraphAsset, b: GraphAsset) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id);
  if (!g.root) {
    const needs = new Map<string, GraphAsset[]>();
    for (const l of g.links) {
      const add = (from: string, to: string) => {
        const t = byId.get(to);
        const had = needs.get(from) ?? [];
        // two links of different types between them are listed once
        if (t && !had.includes(t)) needs.set(from, [...had, t]);
      };
      add(l.from, l.to);
      if (symmetric.has(l.type_id)) add(l.to, l.from);
    }
    const items = [...g.assets]
      .sort((a, b) => a.layer - b.layer || a.row - b.row)
      .map((a) => ({ asset: a, children: [...(needs.get(a.id) ?? [])].sort(byName).map((c) => ({ asset: c, children: [], also: [] })), also: [] }));
    return [{ heading: 'Every asset, with what it needs', items }];
  }
  const parts: ListPart[] = [];
  const root = byId.get(g.root);
  for (const side of ['out', 'in'] as const) {
    const onSide = g.assets.filter((a) => a.side === side);
    if (!onSide.length) continue;
    // Each asset under the first, by name, of those a hop nearer it links to.
    const items = new Map<string, ListItem>();
    const item = (a: GraphAsset) => {
      let it = items.get(a.id);
      if (!it) items.set(a.id, (it = { asset: a, children: [], also: [] }));
      return it;
    };
    const near = (a: GraphAsset, b: GraphAsset) =>
      g.links.some((l) => {
        const way = side === 'out' ? l.from === a.id && l.to === b.id : l.to === a.id && l.from === b.id;
        const any = (l.from === a.id && l.to === b.id) || (l.to === a.id && l.from === b.id);
        return way || (symmetric.has(l.type_id) && any);
      });
    const top: ListItem[] = [];
    for (const a of [...onSide].sort((x, y) => x.hops - y.hops || byName(x, y))) {
      const nearer = a.hops === 1 && root ? [root] : onSide.filter((p) => p.hops === a.hops - 1 && near(p, a)).sort(byName);
      const parent = nearer[0];
      if (a.hops === 1 || !parent) top.push(item(a));
      else item(parent).children.push(item(a));
      item(a).also = nearer.slice(1).map((p) => p.name);
    }
    parts.push({ heading: side === 'out' ? `What ${root?.name ?? 'it'} needs` : `What needs ${root?.name ?? 'it'}`, items: top });
  }
  return parts;
}

/** Every asset a nested list names, by ID, with the root it is of. */
export function listed(parts: ListPart[], root?: string): Set<string> {
  const out = new Set<string>(root ? [root] : []);
  const walk = (items: ListItem[]) => {
    for (const it of items) {
      out.add(it.asset.id);
      walk(it.children);
    }
  };
  for (const p of parts) walk(p.items);
  return out;
}

/** The Graph page of the asset list's search and category. */
export function graphUrl(search: string, categoryId: string): string {
  const q = new URLSearchParams();
  if (categoryId) q.set('category', categoryId);
  if (search.trim()) q.set('search', search.trim());
  const query = q.toString();
  return '/assets/graph' + (query ? `?${query}` : '');
}
