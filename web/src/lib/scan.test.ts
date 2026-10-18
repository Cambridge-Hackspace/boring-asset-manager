import { describe, expect, it } from 'vitest';
import { cleanedScanSpec, newScanSpec, scanActionLabel, scanTarget } from './scan';

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
    expect(scanActionLabel('return')).toBe('Return');
  });
});

describe('scanTarget', () => {
  it('opens a scan link or an asset address here, wherever it was printed from', () => {
    expect(scanTarget('https://bam.example.org/scan/HS-100')).toBe('/scan/HS-100');
    expect(scanTarget(' http://10.0.0.5:8080/assets/0b5e2c1a-1111-4222-8333-444455556666 ')).toBe('/assets/0b5e2c1a-1111-4222-8333-444455556666');
    expect(scanTarget('https://bam.example.org/scan/a%2Fb')).toBe('/scan/a%2Fb');
  });
  it('looks anything else up as a code', () => {
    expect(scanTarget('HS-100')).toBe('/scan/HS-100');
    expect(scanTarget('BIN 4/A')).toBe('/scan/BIN%204%2FA');
    expect(scanTarget('https://example.org/elsewhere')).toBe('/scan/https%3A%2F%2Fexample.org%2Felsewhere');
    expect(scanTarget('   ')).toBeNull();
  });
});
