// The states an asset's icon shows (README §3.7): an asset in a state is
// drawn as the variant of its icon the state is drawn with, when the build
// draws one.

import type { IconState, ResolvedIcon } from './models';

/** The states, the variant each is drawn with, bam's defaults, and a library file to start drawing them from. */
export interface IconStates {
  states: IconState[];
  variants: Record<string, string>;
  defaults: Record<string, string>;
  starter: string;
}

/** What each state is called, worst first. */
export const iconStateNames: Record<IconState, string> = {
  out_of_service: 'Out of service',
  needs_maintenance: 'Needs maintenance',
  overdue: 'Overdue',
  on_loan: 'On loan',
  out_of_stock: 'Out of stock',
  low_stock: 'Running low',
};

/** The icons the build draws in a variant: those named <icon>-<variant> beside an <icon>. */
export function drawnIn(names: Iterable<string>, variant: string): string[] {
  if (!variant) return [];
  const all = new Set(names);
  const suffix = `-${variant}`;
  return [...all].filter((n) => n.endsWith(suffix) && all.has(n.slice(0, -suffix.length))).sort();
}

/** The icon an asset has, without its state. */
export const plainIcon = (icon: ResolvedIcon) => icon.base || icon.name;

/** Says which state an asset's icon shows, if any. */
export function stateNote(icon: ResolvedIcon): string {
  if (!icon.state || !icon.variant) return '';
  return `Drawn as its ${icon.variant} variant: ${iconStateNames[icon.state].toLowerCase()}.`;
}

/** Says which params an asset's icon is drawn with, from its fields, if any. */
/** What texts of its own an asset's icon is written with, if any. */
export function textsNote(icon: ResolvedIcon): string {
  const texts = Object.entries(icon.texts ?? {}).sort(([a], [b]) => a.localeCompare(b));
  if (!texts.length) return '';
  return `Written with ${texts.map(([p, v]) => `${p} “${v}”`).join(', ')}.`;
}

export function paramsNote(icon: ResolvedIcon): string {
  const params = Object.entries(icon.params ?? {}).sort(([a], [b]) => a.localeCompare(b));
  if (!params.length) return '';
  return `Drawn with ${params.map(([p, v]) => `${p} ${v}`).join(', ')}, from its fields.`;
}
