import { describe, expect, it } from 'vitest';
import { panelSummaries } from './summaries';
import type { TemplateDoc } from '../../../lib/template';

const doc = (over: Partial<TemplateDoc> = {}): TemplateDoc =>
  ({
    name: 'Tool tag',
    units: 'mm',
    width: 62,
    height: 28.999,
    dpi: 300,
    elements: [
      { type: 'text', name: 'Code text', x: 0, y: 0, width: 10, height: 5, content_binding: '{{asset.code}}' },
      { type: 'rectangle', x: 0, y: 0, width: 62, height: 29 },
    ],
    ...over,
  }) as TemplateDoc;

describe('panelSummaries', () => {
  it('says what each panel holds', () => {
    expect(panelSummaries(doc(), [], 0)).toEqual({
      label: '62 × 29 mm',
      layers: '2 layers',
      properties: '',
      history: 'No changes',
      grid: 'Hidden',
    });
  });

  it('names what is selected, counts changes, and gives the grid when shown', () => {
    const s = panelSummaries(doc({ grid: { x: 1, y: 2.5, visible: true }, elements: [doc().elements[0]!] }), [0], 1);
    expect(s).toMatchObject({ layers: '1 layer', properties: 'Code text', history: '1 change', grid: '1 × 2.5 mm' });
    expect(panelSummaries(doc(), [0, 1], 3)).toMatchObject({ properties: '2 selected', history: '3 changes' });
  });
});
