import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import SpoolPage from './SpoolPage.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { Spool, SpoolEntry } from '../lib/models';

const entry = (id: string, name: string, destination: SpoolEntry['destination'], template: string | null, over: Partial<SpoolEntry> = {}): SpoolEntry => ({
  id,
  asset_id: `a-${id}`,
  asset_name: name,
  asset_archived: false,
  template_id: template,
  destination,
  copies: 1,
  added_by: { id: 'me', name: 'Me' },
  added_at: '2026-10-16T10:00:00Z',
  problems: [],
  ready: !over.problems?.length && !over.job,
  ...over,
});

const spoolOf = (entries: SpoolEntry[]): Spool => ({
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

beforeEach(() => {
  session.user = { id: 'me', email: 'me@hackspace.org', name: 'Me', is_active: true, created_at: '', updated_at: '' };
  session.permissions = ['assets.read', 'labels.print'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('SpoolPage', () => {
  it('groups waiting labels by printer and template, with what keeps them from printing', async () => {
    fakeApi({
      'GET /spool/jobs': () => ({ jobs: [] }),
      'GET /spool': () =>
        spoolOf([
          entry('1', 'Bin 1', 'printer:p1', 'shelf'),
          entry('2', 'Loose', '', null, { problems: ['No template chosen', 'No printer chosen'] }),
          entry('3', 'Bin 2', 'printer:p1', 'shelf', { copies: 2 }),
        ]),
    });
    render(SpoolPage, { params: {} });
    const front = (await screen.findByRole('heading', { name: /Front desk · Shelf · 2 labels/ })).closest('section')!;
    expect(within(front).getAllByRole('link').map((l) => l.textContent)).toEqual(['Bin 1', 'Bin 2']);
    expect(within(front).getAllByText('Ready').length).toBe(2);
    const nowhere = screen.getByRole('heading', { name: /No printer chosen · No template chosen · 1 label/ }).closest('section')!;
    expect(within(nowhere).getByText('No template chosen', { selector: 'li' })).toBeTruthy();
  });

  it('sets templates and printers in bulk, offering only what fits', async () => {
    let spool = spoolOf([entry('1', 'Bin 1', 'printer:p1', 'shelf'), entry('2', 'Bin 2', 'printer:p1', 'shelf'), entry('3', 'Tag me', 'printer:p2', 'tag')]);
    const { sent } = fakeApi({
      'GET /spool/jobs': () => ({ jobs: [] }),
      'GET /spool': () => spool,
      'PATCH /spool': (body) => {
        const b = body as { entry_ids: string[]; destination?: SpoolEntry['destination'] };
        spool = { ...spool, entries: spool.entries.map((e) => (b.entry_ids.includes(e.id) && b.destination !== undefined ? { ...e, destination: b.destination } : e)) };
        return { updated: b.entry_ids.length };
      },
      'POST /spool/remove': () => {
        spool = { ...spool, entries: spool.entries.filter((e) => e.id !== '3') };
        return { removed: 1 };
      },
    });
    render(SpoolPage, { params: {} });
    const front = (await screen.findByRole('heading', { name: /Front desk · Shelf/ })).closest('section')!;
    await fireEvent.click(within(front).getByRole('checkbox', { name: 'Select every label for Front desk, Shelf' }));
    const bar = screen.getByRole('region', { name: 'Change the selected labels' });
    expect(within(bar).getByText('2 labels selected')).toBeTruthy();
    // Shelf labels suit only the front desk; downloading suits anything.
    const printers = within(bar).getByLabelText('Print on') as HTMLSelectElement;
    expect([...printers.options].map((o) => o.textContent)).toEqual(['Not chosen', 'Front desk', 'Download (PDF or ZIP)']);
    // Both go to the front desk, which takes either template.
    expect([...(within(bar).getByLabelText('Template') as HTMLSelectElement).options].map((o) => o.textContent)).toEqual(['None', 'Shelf', 'Tag']);
    await fireEvent.change(printers, { target: { value: 'download' } });
    await fireEvent.submit(within(bar).getByRole('button', { name: 'Set printer' }));
    expect(await screen.findByText('Printer set for 2 labels.')).toBeTruthy();
    expect(sent.find((r) => r.method === 'PATCH')!.body).toEqual({ entry_ids: ['1', '2'], destination: 'download' });
    expect(await screen.findByRole('heading', { name: /Download · Shelf · 2 labels/ })).toBeTruthy();

    // Selecting the tag printer's label offers only the templates it takes.
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select every label for Download, Shelf' }));
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select Tag me' }));
    expect([...(screen.getByLabelText('Template') as HTMLSelectElement).options].map((o) => o.textContent)).toEqual(['None', 'Tag']);
    await fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Remove 1 label' }));
    expect(await screen.findByText('Removed 1 label from the spool.')).toBeTruthy();
    expect(sent.find((r) => r.path === '/spool/remove')!.body).toEqual({ entry_ids: ['3'] });
    await vi.waitFor(() => expect(screen.queryByText('Tag me')).toBeNull());
  });

  it('downloads a group, counting what could not be produced', async () => {
    let spool = spoolOf([entry('1', 'Bin 1', 'download', 'shelf'), entry('2', 'Bin 2', 'download', 'shelf'), entry('3', 'Broken', 'download', 'shelf', { problems: ['Its asset is archived'] })]);
    const { sent } = fakeApi({ 'GET /spool/jobs': () => ({ jobs: [] }),
      'GET /spool': () => spool });
    const realFetch = globalThis.fetch;
    vi.stubGlobal('fetch', (url: string, init?: RequestInit) => {
      if (url.endsWith('/spool/download')) {
        // Bin 2 could not be produced, and stayed.
        spool = { ...spool, entries: spool.entries.filter((e) => e.id !== '1') };
        return Promise.resolve(new Response('%PDF', { headers: { 'Content-Disposition': 'attachment; filename="spool-Shelf.pdf"' } }));
      }
      return realFetch(url, init);
    });
    URL.createObjectURL = vi.fn(() => 'blob:x');
    URL.revokeObjectURL = vi.fn();
    render(SpoolPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'Download PDF' }));
    expect(await screen.findByText('Downloaded 1 label. 1 label could not be produced and stayed in the spool.')).toBeTruthy();
    // Only those ready went, and the list was read again.
    expect(sent.filter((r) => r.path === '/spool').length).toBe(2);
  });

  it('lists what was printed, to print again, and only one’s own when asked', async () => {
    const printed = [
      entry('9', 'Bin 1', 'download', 'shelf', { printed_at: '2026-10-15T10:00:00Z', printed_by: { id: 'me', name: 'Me' }, printed_template: 'Shelf', printed_on: 'Download', copies: 2 }),
    ];
    const { fetchMock, sent } = fakeApi({
      'GET /spool/jobs': () => ({ jobs: [] }),
      'GET /spool': () => spoolOf(fetchMock.mock.calls.at(-1)![0].includes('printed=true') ? printed : []),
      'POST /spool/requeue': () => ({ status: 201, body: { added: [{ id: 'new' }], already_waiting: [], skipped: [] } }),
    });
    render(SpoolPage, { params: {} });
    expect(await screen.findByText(/The spool is empty/)).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Printed' }));
    const row = (await screen.findByRole('link', { name: 'Bin 1' })).closest('tr')!;
    expect(row.textContent).toContain('Download');
    expect(row.textContent).toContain('Me');
    await fireEvent.click(within(row).getByRole('checkbox'));
    await fireEvent.click(screen.getByRole('button', { name: 'Print again' }));
    expect(await screen.findByText('Added 1 label to the spool.')).toBeTruthy();
    expect(sent.find((r) => r.path === '/spool/requeue')!.body).toEqual({ entry_ids: ['9'] });

    await fireEvent.click(screen.getByRole('checkbox', { name: /Include those over 30 days old/ }));
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Only labels I added' }));
    await vi.waitFor(() => expect(fetchMock.mock.calls.map((c) => c[0] as string).filter((u) => u.includes('/spool?')).at(-1)).toContain('printed=true&older=true&mine=true'));
  });

  it('prints a group in the background, showing its labels as they go', async () => {
    let spool = spoolOf([entry('1', 'Bin 1', 'printer:p1', 'shelf', { copies: 2 }), entry('2', 'Bin 2', 'printer:p1', 'shelf')]);
    const job = { id: 'j1', template_id: 'shelf', template_name: 'Shelf', printer_id: 'p1', target: 'Front desk', download: false, state: 'waiting' as const, labels: 3, printed: 0, entries: 2, created_by: { id: 'me', name: 'Me' }, created_at: '2026-10-16T10:00:00Z' };
    const { sent } = fakeApi({
      'GET /spool/jobs': () => ({ jobs: [] }),
      'GET /spool': () => spool,
      'POST /spool/print': () => {
        spool = { ...spool, entries: spool.entries.map((e) => ({ ...e, ready: false, job: { id: 'j1', state: 'sending' as const, target: 'Front desk' } })) };
        return { status: 201, body: { jobs: [job], skipped: [] } };
      },
    });
    render(SpoolPage, { params: {} });
    expect(await screen.findByRole('button', { name: 'Print everything ready (3 labels)' })).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Print 3 labels' }));
    expect(await screen.findByText('Sent 3 labels to print in 1 job.')).toBeTruthy();
    expect(sent.find((r) => r.path === '/spool/print')!.body).toEqual({ entry_ids: ['1', '2'] });
    // Being printed, they cannot be chosen or changed.
    expect((await screen.findAllByText('Sending to Front desk…')).length).toBe(2);
    expect((screen.getByRole('checkbox', { name: 'Select Bin 1' }) as HTMLInputElement).disabled).toBe(true);
    expect(screen.queryByRole('button', { name: /Print everything ready/ })).toBeNull();
  });

  it('keeps checking on jobs while they print', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let reads = 0;
      fakeApi({
        'GET /spool/jobs': () => ({ jobs: [] }),
        'GET /spool': () => {
          reads++;
          return spoolOf([entry('1', 'Bin 1', 'printer:p1', 'shelf', reads < 3 ? { job: { id: 'j', state: 'sending', target: 'Front desk' } } : {})]);
        },
      });
      render(SpoolPage, { params: {} });
      await screen.findByText('Sending to Front desk…');
      await vi.advanceTimersByTimeAsync(4500);
      expect(await screen.findByText('Ready')).toBeTruthy();
      const settled = reads;
      await vi.advanceTimersByTimeAsync(6000);
      expect(reads).toBe(settled);
    } finally {
      vi.useRealTimers();
    }
  });

  it('lists jobs, cancels one waiting, and sends one again elsewhere', async () => {
    const base = { template_id: 'tag', template_name: 'Tag', printer_id: 'p2', target: 'Back room', download: false, entries: 1, created_by: { id: 'me', name: 'Me' }, created_at: '2026-10-16T10:00:00Z' };
    let jobs = [
      { ...base, id: 'j2', state: 'waiting' as const, labels: 2, printed: 0 },
      { ...base, id: 'j1', state: 'done' as const, labels: 1, printed: 1 },
      { ...base, id: 'j0', state: 'failed' as const, labels: 1, printed: 0, error: 'Could not deliver these to Back room: refused' },
    ];
    const { sent } = fakeApi({
      'GET /spool/jobs': () => ({ jobs }),
      'GET /spool': () => spoolOf([]),
      'POST /spool/jobs/j2/cancel': () => {
        jobs = jobs.map((j) => (j.id === 'j2' ? { ...j, state: 'canceled' as const } : j)) as typeof jobs;
        return { canceled: 'j2' };
      },
      'POST /spool/jobs/j1/resend': () => ({ status: 201, body: { added: [{ id: 'e' }], already_waiting: [], skipped: [], jobs: [{ id: 'j3' }] } }),
    });
    render(SpoolPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'Jobs' }));
    const rows = await screen.findAllByRole('row');
    expect(rows[3]!.textContent).toContain('Could not deliver these to Back room');
    // Only what printed can be sent again; a failed job's labels wait in the spool.
    expect(within(rows[3]!).queryByRole('button', { name: 'Send again' })).toBeNull();
    await fireEvent.click(within(rows[1]!).getByRole('button', { name: 'Cancel the job printing 2 labels on Back room' }));
    expect(await screen.findByText('Canceled; its 2 labels wait in the spool again.')).toBeTruthy();
    expect(await screen.findByText('Canceled')).toBeTruthy();

    // Tag labels suit both printers; the job went to the back room.
    const to = within(rows[2]!).getByRole('combobox') as HTMLSelectElement;
    expect([...to.options].map((o) => o.textContent)).toEqual(['Back room', 'Front desk', 'Bench (pool)', 'Download']);
    await fireEvent.change(to, { target: { value: 'printer:p1' } });
    await fireEvent.click(within(rows[2]!).getByRole('button', { name: 'Send again' }));
    expect(await screen.findByText('Added 1 label to the spool, and sent to print.')).toBeTruthy();
    expect(sent.find((r) => r.path === '/spool/jobs/j1/resend')!.body).toEqual({ destination: 'printer:p1' });
  });

  it('offers pools the selection suits, prints to them, and shows each printer’s part', async () => {
    let spool = spoolOf([entry('1', 'Tag me', 'printer:p2', 'tag'), entry('2', 'Shelf me', 'printer:p1', 'shelf')]);
    const job = {
      id: 'j', template_id: 'tag', template_name: 'Tag', printer_id: null, pool_id: 'bench', target: 'Bench', download: false, state: 'sending' as const,
      labels: 4, printed: 2, entries: 4, created_by: { id: 'me', name: 'Me' }, created_at: '2026-10-16T10:00:00Z',
      members: [
        { printer_id: 'p1', printer: 'Front desk', printed: 2, sending: true, dropped: false },
        { printer_id: 'p2', printer: 'Back room', printed: 0, sending: false, dropped: true },
      ],
    };
    const { sent } = fakeApi({
      'GET /spool/jobs': () => ({ jobs: [job] }),
      'GET /spool': () => spool,
      'PATCH /spool': () => {
        spool = { ...spool, entries: spool.entries.map((e) => (e.id === '1' ? { ...e, destination: 'pool:bench' as const } : e)) };
        return { updated: 1 };
      },
      'POST /spool/print': () => ({ status: 201, body: { jobs: [job], skipped: [], warnings: ['Back room sits out Bench’s Tag labels: no'] } }),
    });
    render(SpoolPage, { params: {} });
    // Shelf labels suit no pool; tag labels suit the bench.
    await fireEvent.click(await screen.findByRole('checkbox', { name: 'Select Shelf me' }));
    expect(screen.queryByRole('group', { name: 'Pools' })).toBeNull();
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select Shelf me' }));
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select Tag me' }));
    const pools = screen.getByRole('group', { name: 'Pools' });
    expect(within(pools).getAllByRole('option').map((o) => o.textContent)).toEqual(['Bench']);
    await fireEvent.change(screen.getByLabelText('Print on'), { target: { value: 'pool:bench' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Set printer' }));
    const bench = (await screen.findByRole('heading', { name: /Bench · Tag · 1 label/ })).closest('section')!;
    await fireEvent.click(within(bench).getByRole('button', { name: 'Print 1 label' }));
    expect(await screen.findByText(/Sent 4 labels to print in 1 job\. Back room sits out Bench’s Tag labels: no\./)).toBeTruthy();
    expect(sent.find((r) => r.path === '/spool/print')!.body).toEqual({ entry_ids: ['1'] });

    await fireEvent.click(screen.getByRole('button', { name: 'Jobs' }));
    const parts = await screen.findByRole('list', { name: 'Its printers' });
    expect(within(parts).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['Front desk: sending, 2 printed', 'Back room: dropped out']);
  });
});
