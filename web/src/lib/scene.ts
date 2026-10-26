// Scenes (README §3.7): a location's contents as the server lays them out
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
const S = 40;
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
