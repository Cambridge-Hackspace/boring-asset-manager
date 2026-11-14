import { describe, expect, it } from 'vitest';
import { neighborsOf, parseImport, sourceOf, splitNames, termMatches } from './terms';
import type { Term, TermLink } from './models';

const term = (id: string, label: string, alt_labels: string[] = [], note = ''): Term => ({ id, label, alt_labels, note, source: 'person', created_at: '', updated_at: '' });
const link = (term_id: string, relation: 'broader' | 'related', other_id: string): TermLink => ({ term_id, other_id, relation, source: 'person', created_at: '' });

describe('terms', () => {
  it('says where a term came from', () => {
    expect(sourceOf({ source: 'person', added_by_name: 'Ada' })).toBe('Added by Ada');
    expect(sourceOf({ source: 'agent', added_by_name: 'Ada', via: 'Claude Desktop' })).toBe('Added by Ada through Claude Desktop');
    expect(sourceOf({ source: 'agent', via: 'Claude Desktop' })).toBe('Added through Claude Desktop');
    expect(sourceOf({ source: 'import', import_id: 'i1' }, [{ id: 'i1', name: 'Filament words', terms: 3, links: 2, created_at: '' }])).toBe('From the import Filament words');
    expect(sourceOf({ source: 'import', import_id: 'gone' })).toBe('From an import');
    // Those who don't manage terms aren't told who added them.
    expect(sourceOf({ source: 'person' })).toBe('');
  });

  it('finds the terms a term links to, either way round', () => {
    const links = [link('tpu', 'broader', 'flex'), link('flex', 'broader', 'fil'), link('tpe', 'related', 'tpu')];
    expect(neighborsOf('flex', links)).toEqual({ broader: ['fil'], narrower: ['tpu'], related: [] });
    expect(neighborsOf('tpu', links)).toEqual({ broader: ['flex'], narrower: [], related: ['tpe'] });
  });

  it('reads other names one per line or by commas, and finds a term by any name', () => {
    expect(splitNames('TPU,  thermoplastic polyurethane\n\n Ninjaflex ')).toEqual(['TPU', 'thermoplastic polyurethane', 'Ninjaflex']);
    const tpu = term('tpu', 'TPU', ['thermoplastic  polyurethane'], 'Bends.');
    expect(termMatches(tpu, 'thermoplastic POLY')).toBe(true);
    expect(termMatches(tpu, 'bends')).toBe(true);
    expect(termMatches(tpu, 'nylon')).toBe(false);
    expect(termMatches(tpu, '  ')).toBe(true);
  });

  it('reads a file to import, as a list or with a name', () => {
    expect(parseImport('[{"label": "TPU", "broader": ["Flexible filament"], "related": [1, "TPE"]}]', 'words')).toEqual({
      name: 'words',
      terms: [{ label: 'TPU', alt_labels: [], note: '', broader: ['Flexible filament'], related: ['TPE'] }],
    });
    expect(parseImport('{"name": "Filament words", "terms": [{"label": "PLA", "alt_labels": ["polylactic acid"]}]}', 'file').name).toBe('Filament words');
    expect(() => parseImport('not json', 'x')).toThrow('The file is not JSON.');
    expect(() => parseImport('[]', 'x')).toThrow('holds no terms');
    expect(() => parseImport('[{"label": "PLA"}, {"note": "no label"}]', 'x')).toThrow('Term 2 has no label.');
  });
});
