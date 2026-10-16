import { describe, expect, it } from 'vitest';
import { cleanedScanSpec, newScanSpec, scanActionLabel } from './scan';

describe('scan templates', () => {
  it('starts with details, contents and a way to the page', () => {
    expect(newScanSpec().sections.map((s) => s.type)).toEqual(['details', 'table', 'actions']);
  });

  it('sends only what the API takes', () => {
    const spec = newScanSpec();
    spec.sections[0]!.title = '  ';
    spec.sections[1]!.sort_by = 0;
    spec.sections[1]!.descending = true;
    spec.sections[2]!.actions = ['open', 'use'];
    expect(cleanedScanSpec(spec)).toEqual({
      filter: {},
      sections: [
        { type: 'details', columns: [{ heading: 'Name', binding: '{{asset.name}}' }, { heading: 'Where', binding: '{{asset.location}}' }] },
        { type: 'table', title: 'In it', source: 'contents', descending: true, columns: [{ heading: 'What', binding: '{{asset.name}}' }] },
        { type: 'actions', actions: ['open', 'use'] },
      ],
    });
  });

  it('names actions', () => {
    expect(scanActionLabel('return')).toBe('Give it back');
  });
});
