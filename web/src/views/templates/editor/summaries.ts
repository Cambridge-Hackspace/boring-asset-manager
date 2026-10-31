// What each panel holds, in a line on its title (#163), so a collapsed
// panel still says what is in it.

import { layerName, type TemplateDoc } from '../../../lib/template';
import { gridOf } from './snapping';

const tidy = (n: number) => String(+n.toFixed(2));
const count = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`;

/** The summaries of the panels that have one, by panel. */
export function panelSummaries(doc: TemplateDoc, selected: readonly number[], changes: number): Record<string, string> {
  const grid = gridOf(doc);
  const only = selected.length === 1 ? doc.elements[selected[0]!] : undefined;
  return {
    label: `${tidy(doc.width)} × ${tidy(doc.height)} ${doc.units}`,
    layers: count(doc.elements.length, 'layer'),
    // Nothing selected, its panel says so itself.
    properties: only ? layerName(only) : selected.length ? `${selected.length} selected` : '',
    history: changes ? count(changes, 'change') : 'No changes',
    grid: doc.grid?.visible ? `${tidy(grid.x)} × ${tidy(grid.y)} ${doc.units}` : 'Hidden',
  };
}
