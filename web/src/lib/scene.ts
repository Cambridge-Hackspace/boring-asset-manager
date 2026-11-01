// Scenes (docs/icons.md): a location's contents as the server lays them out
// (internal/scene), drawn as they are given, and the view of one: its
// floor's grid, an item's outline, and panning and zooming.

import type { IrShape } from './isolex/ir';
import type { Condition, LinkedAsset, ResolvedIcon } from './models';

/** An asset in a scene: where it stands, in tiles, and where it is drawn. */
export interface SceneItem {
  id: string;
  name: string;
  code?: string;
  condition: Condition;
  icon?: ResolvedIcon;
  /** With no icon in three dimensions, a plain box its size. */
  shapes?: IrShape[];
  x: number;
  y: number;
  z: number;
  size: [number, number, number];
  tx: number;
  ty: number;
  holds: number;
  /** Where someone placed it, rather than packed. */
  arranged?: boolean;
  /** The IDs of the items it shares space with. */
  overlaps?: string[];
  /** Whether the caller may arrange it. */
  movable?: boolean;
}

export interface Scene {
  location: { id: string; name: string; icon?: ResolvedIcon };
  path: LinkedAsset[];
  floor: [number, number];
  viewbox: [number, number, number, number];
  /** Back to front. */
  items: SceneItem[];
}

export type ViewBox = [number, number, number, number];

// isolex's projection (its SPEC, §7)
export const S = 40;
const COS = 0.8660254037844387;
const SIN = 0.5;

/** A point in tiles on the page, as isolex projects it. */
export function project(x: number, y: number, z: number): [number, number] {
  return [(x - y) * COS * S, (x + y) * SIN * S - z * S];
}

/** The floor's grid: a line along each tile edge, as [x1, y1, x2, y2]. */
export function gridLines(width: number, depth: number): [number, number, number, number][] {
  const lines: [number, number, number, number][] = [];
  for (let i = 0; i <= width; i++) lines.push([...project(i, 0, 0), ...project(i, depth, 0)]);
  for (let j = 0; j <= depth; j++) lines.push([...project(0, j, 0), ...project(width, j, 0)]);
  return lines;
}

/** The outline of an item's box on the page: its six-sided silhouette. */
export function hull(item: Pick<SceneItem, 'x' | 'y' | 'z' | 'size'>): [number, number][] {
  const { x, y, z } = item;
  const [w, d, h] = item.size;
  return [
    project(x, y, z + h),
    project(x + w, y, z + h),
    project(x + w, y, z),
    project(x + w, y + d, z),
    project(x, y + d, z),
    project(x, y + d, z + h),
  ];
}

/** How far in a view zooms, as its width against the scene's: from a tenth to four times as wide. */
const MIN_SCALE = 0.1;
const MAX_SCALE = 4;

/** The view zoomed by factor (above 1 nearer) about a point on the page, kept to the limits. */
export function zoomAt(view: ViewBox, whole: ViewBox, px: number, py: number, factor: number): ViewBox {
  const [x, y, w, h] = view;
  const width = Math.min(Math.max(w / factor, whole[2] * MIN_SCALE), whole[2] * MAX_SCALE);
  const k = width / w;
  return [px - (px - x) * k, py - (py - y) * k, width, h * k];
}

/** The view moved by dx, dy on the page. */
export function panBy(view: ViewBox, dx: number, dy: number): ViewBox {
  return [view[0] + dx, view[1] + dy, view[2], view[3]];
}

/** What an item is, said in a line: its name, code and condition. */
export function describeItem(item: Pick<SceneItem, 'name' | 'code' | 'condition' | 'holds'>): string {
  const parts = [item.name];
  if (item.code) parts.push(item.code);
  if (item.condition !== 'ok') parts.push(item.condition.replaceAll('_', ' '));
  if (item.holds) parts.push(`holds ${item.holds}`);
  return parts.join(', ');
}

/** How far apart heights are lifted to, in tiles. */
export const LIFT_STEP = 0.25;

/** A move across the page, dx, dy, as tiles across a floor: x and y. */
export function floorDelta(dx: number, dy: number): [number, number] {
  const a = dx / (COS * S); // x - y
  const b = dy / (SIN * S); // x + y
  return [(a + b) / 2, (b - a) / 2];
}

/** Where an item dragged from (x, y) by dx, dy tiles comes to rest: on whole tiles, on the floor. */
export function moveTo(item: Pick<SceneItem, 'size'>, from: [number, number], dx: number, dy: number, floor: [number, number]): [number, number] {
  const clamp = (v: number, max: number) => Math.min(Math.max(Math.round(v), 0), Math.max(max, 0));
  return [clamp(from[0] + dx, floor[0] - item.size[0]), clamp(from[1] + dy, floor[1] - item.size[1])];
}

/**
 * The height an item lifted to z comes to rest at: on top of something
 * under it when near enough, and otherwise on a step; never below the floor.
 */
export function liftTo(items: Pick<SceneItem, 'id' | 'x' | 'y' | 'z' | 'size'>[], item: Pick<SceneItem, 'id' | 'x' | 'y' | 'size'>, z: number): number {
  const under = (o: Pick<SceneItem, 'x' | 'y' | 'size'>) =>
    o.x < item.x + item.size[0] && item.x < o.x + o.size[0] && o.y < item.y + item.size[1] && item.y < o.y + o.size[1];
  for (const o of items) {
    if (o.id === item.id || !under(o)) continue;
    const top = o.z + o.size[2];
    if (Math.abs(z - top) < LIFT_STEP / 2) return top;
  }
  return Math.max(0, Math.round(z / LIFT_STEP) * LIFT_STEP);
}

/** Where an item stands, as its history says it: "1, 2, 0.25". */
export function position(item: Pick<SceneItem, 'x' | 'y' | 'z'>): string {
  return [item.x, item.y, item.z].map((v) => String(Math.round(v * 1000) / 1000)).join(', ');
}

/** What an item overlaps, said in a line, or nothing. */
export function overlapNote(item: Pick<SceneItem, 'overlaps'>, items: Pick<SceneItem, 'id' | 'name'>[]): string {
  const names = (item.overlaps ?? []).map((id) => items.find((i) => i.id === id)?.name ?? id);
  return names.length ? `Overlaps ${names.join(', ')}.` : '';
}

/** A location's scene as an image to save: SVG, or PNG drawn 1,024 pixels high; with show outlined. */
export function sceneImage(id: string, format: 'svg' | 'png', show?: string | null): string {
  const q = new URLSearchParams({ format });
  if (format === 'png') q.set('height', '1024');
  if (show) q.set('show', show);
  return `/api/v1/assets/${encodeURIComponent(id)}/scene/image?${q}`;
}
