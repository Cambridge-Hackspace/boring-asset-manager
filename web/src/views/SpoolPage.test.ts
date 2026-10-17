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
  fits: { shelf: ['p1'], tag: ['p1', 'p2'] },
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
    const { sent } = fakeApi({ 'GET /spool': () => spool });
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
    // Only those ready went.
    expect(sent.length).toBe(2);
  });

  it('lists what was printed, to print again, and only one’s own when asked', async () => {
    const printed = [
      entry('9', 'Bin 1', 'download', 'shelf', { printed_at: '2026-10-15T10:00:00Z', printed_by: { id: 'me', name: 'Me' }, printed_template: 'Shelf', printed_on: 'Download', copies: 2 }),
    ];
    const { fetchMock, sent } = fakeApi({
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

    await fireEvent.click(screen.getByRole('checkbox', { name: /Include those printed over 30 days ago/ }));
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Only labels I added' }));
    await vi.waitFor(() => expect(fetchMock.mock.calls.at(-1)![0]).toContain('printed=true&older=true&mine=true'));
  });
});
