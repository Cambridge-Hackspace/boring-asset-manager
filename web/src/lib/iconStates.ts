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
  if (!icon.state || !icon.base) return '';
  return `Drawn as ${icon.name}: ${iconStateNames[icon.state].toLowerCase()}.`;
}
