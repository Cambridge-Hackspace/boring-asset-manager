import { afterEach, describe, expect, it, vi } from 'vitest';
import { addedMessage, destinationName, groupEntries, jobStateName, poolsFor, printedMessage, printersFor, queueNewLabels, rememberQueueNewLabels, shared, templatesFor } from './spool';
import type { Spool, SpoolEntry } from './models';

const entry = (id: string, destination: SpoolEntry['destination'], template: string | null): SpoolEntry => ({
  id,
  asset_id: `a-${id}`,
  asset_name: `Asset ${id}`,
  asset_archived: false,
  template_id: template,
  destination,
  copies: 1,
  added_by: { id: 'me', name: 'Me' },
  added_at: '2026-10-16T10:00:00Z',
  problems: [],
});

const spool = (entries: SpoolEntry[]): Spool => ({
  entries,
  templates: [
    { id: 'shelf', name: 'Shelf' },
    { id: 'tag', name: 'Tag' },
  ],
  printers: [
    { id: 'p1', name: 'Front desk' },
    { id: 'p2', name: 'Back room' },
  ],
  pools: [{ id: 'bench', name: 'Bench' }],
  fits: { shelf: ['p1'], tag: ['p1', 'p2'] },
  pool_fits: { shelf: [], tag: ['bench'] },
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe('the spool', () => {
  it('says what adding did', () => {
    expect(addedMessage({ added: [entry('1', '', null)], already_waiting: [], skipped: [] })).toBe('Added 1 label to the spool.');
    expect(
      addedMessage({
        added: [],
        already_waiting: ['a', 'b'],
        skipped: [
          { asset_id: 'c', reason: 'archived' },
          { asset_id: 'd', reason: 'not found' },
        ],
      }),
    ).toBe('Nothing new added to the spool; 2 labels already waiting; 1 archived, never labeled; 1 not found.');
  });

  it('groups entries by printer and template, printers first and nowhere last', () => {
    const s = spool([entry('1', '', null), entry('2', 'download', 'tag'), entry('3', 'printer:p2', 'tag'), entry('4', 'printer:p1', 'shelf'), entry('5', 'printer:p1', 'shelf')]);
    expect(groupEntries(s).map((g) => [g.destination, g.templateId, g.entries.map((e) => e.id)])).toEqual([
      ['printer:p2', 'tag', ['3']],
      ['printer:p1', 'shelf', ['4', '5']],
      ['download', 'tag', ['2']],
      ['', null, ['1']],
    ]);
  });

  it('names destinations, gone ones too', () => {
    const s = spool([]);
    expect(destinationName(s, 'printer:p1')).toBe('Front desk');
    expect(destinationName(s, 'printer:gone')).toBe('A deleted printer');
    expect(destinationName(s, 'download')).toBe('Download');
    expect(destinationName(s, '')).toBe('No printer chosen');
  });

  it('offers only the printers a template suits, and the reverse', () => {
    const s = spool([]);
    expect(printersFor(s, 'shelf').map((p) => p.name)).toEqual(['Front desk']);
    expect(printersFor(s, null).length).toBe(2);
    expect(templatesFor(s, 'printer:p2').map((t) => t.name)).toEqual(['Tag']);
    expect(templatesFor(s, 'download').length).toBe(2);
  });

  it('finds what a selection shares', () => {
    expect(shared([entry('1', 'download', 'tag'), entry('2', 'download', 'shelf')], (e) => e.destination)).toBe('download');
    expect(shared([entry('1', 'download', 'tag'), entry('2', 'download', 'shelf')], (e) => e.template_id)).toBeUndefined();
    expect(shared([], (e: SpoolEntry) => e.id)).toBeUndefined();
  });

  it('remembers whether each person queues new labels, ticked at first', () => {
    expect(queueNewLabels('ada')).toBe(true);
    rememberQueueNewLabels('ada', false);
    expect(queueNewLabels('ada')).toBe(false);
    expect(queueNewLabels('bo')).toBe(true);
    // Storage that refuses falls back to ticked, and forgets quietly.
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    });
    expect(queueNewLabels('ada')).toBe(true);
    expect(() => rememberQueueNewLabels('ada', false)).not.toThrow();
  });

  it('says what printing did, and how jobs stand', () => {
    const job = (labels: number) => ({ id: 'j', template_id: 't', template_name: 'Tag', printer_id: 'p', target: 'Front', download: false, state: 'waiting' as const, labels, printed: 0, entries: 1, created_by: { id: 'me' }, created_at: '' });
    expect(printedMessage({ jobs: [job(2), job(3)], skipped: [] })).toBe('Sent 5 labels to print in 2 jobs.');
    expect(printedMessage({ jobs: [], skipped: [{ entry_id: 'e', asset_name: 'Saw', reason: 'No template chosen' }] })).toBe(
      'Nothing was ready to print; 1 label left out: Saw (No template chosen).',
    );
    expect(jobStateName({ state: 'done', download: true })).toBe('Downloaded');
    expect(jobStateName({ state: 'done', download: false })).toBe('Printed');
    expect(jobStateName({ state: 'failed', download: false })).toBe('Failed');
  });

  it('knows pools: their names, the templates they take, and their warnings', () => {
    const s = spool([entry('1', 'pool:bench', 'tag'), entry('2', 'printer:p1', 'shelf')]);
    expect(destinationName(s, 'pool:bench')).toBe('Bench');
    expect(destinationName(s, 'pool:gone')).toBe('A deleted pool');
    expect(poolsFor(s, 'shelf')).toEqual([]);
    expect(poolsFor(s, 'tag').map((p) => p.name)).toEqual(['Bench']);
    expect(templatesFor(s, 'pool:bench').map((t) => t.name)).toEqual(['Tag']);
    // Pools print, so they come with the printers.
    expect(groupEntries(s).map((g) => g.destination)).toEqual(['pool:bench', 'printer:p1']);
    expect(printedMessage({ jobs: [], skipped: [], warnings: ['Wide sits out Bench'] })).toBe('Nothing was ready to print. Wide sits out Bench.');
  });
});
