import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import TermsPage from './TermsPage.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { Term, TermImport, TermLink } from '../lib/models';

const term = (id: string, label: string, extra: Partial<Term> = {}): Term => ({
  id, label, alt_labels: [], note: '', source: 'person', added_by_name: 'Ada', created_at: '', updated_at: '', ...extra,
});

beforeEach(() => {
  session.permissions = ['terms.manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('TermsPage', () => {
  it('adds a term, keeps it open to link it, and links it narrower than another', async () => {
    let terms: Term[] = [term('flex', 'Flexible filament', { via: 'Claude Desktop', source: 'agent' })];
    let links: TermLink[] = [];
    const { sent } = fakeApi({
      'GET /terms': () => ({ terms, links, imports: [] }),
      'POST /terms': (body) => {
        const made = term('tpu', (body as Term).label, { alt_labels: (body as Term).alt_labels });
        terms = [...terms, made];
        return { status: 201, body: { term: made } };
      },
      'PUT /terms/tpu/links/broader/flex': () => {
        links = [{ term_id: 'tpu', other_id: 'flex', relation: 'broader', source: 'person', created_at: '' }];
        return { message: 'Terms linked' };
      },
    });
    render(TermsPage, { params: {} });
    const flex = (await screen.findByRole('heading', { name: 'Flexible filament' })).closest('li')!;
    expect(flex.textContent).toContain('Added by Ada through Claude Desktop.');

    await fireEvent.click(screen.getByRole('button', { name: 'New term' }));
    // One primary action at a time.
    expect(screen.queryByRole('button', { name: 'New term' })).toBeNull();
    await fireEvent.input(screen.getByLabelText('Preferred label'), { target: { value: 'TPU' } });
    await fireEvent.input(screen.getByLabelText('Other names, one per line'), { target: { value: 'thermoplastic polyurethane\nNinjaflex' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add term' }));
    await screen.findByText('Added TPU. Link it to other terms below.');
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ label: 'TPU', alt_labels: ['thermoplastic polyurethane', 'Ninjaflex'], note: '' });

    await fireEvent.change(screen.getByLabelText('Add one of the broader terms'), { target: { value: 'flex' } });
    const broader = screen.getByRole('heading', { name: 'Broader terms' }).parentElement!;
    await fireEvent.click(within(broader).getByRole('button', { name: 'Link' }));
    await screen.findByRole('button', { name: 'Unlink Flexible filament from broader terms' });
    const card = (await screen.findAllByRole('heading', { name: 'Flexible filament' })).map((h) => h.closest('li')).find(Boolean)!;
    expect(card.textContent).toContain('Finds TPU.');
  });

  it('shows what a search would find, and removes an import as one', async () => {
    const imp: TermImport = { id: 'i1', name: 'Filament words', terms: 2, links: 1, added_by_name: 'Ada', created_at: '' };
    let imports = [imp];
    let terms = [term('tpu', 'TPU', { source: 'import', import_id: 'i1' })];
    fakeApi({
      'GET /terms': () => ({ terms, links: [], imports }),
      'GET /terms/expand': () => ({ search: 'flexible filament', expansion: [{ label: 'TPU', why: 'TPU, narrower than Flexible filament' }], related: ['TPE'] }),
      'DELETE /terms/imports/i1': () => {
        imports = [];
        terms = [];
        return { message: 'Import removed' };
      },
    });
    render(TermsPage, { params: {} });
    expect((await screen.findByRole('heading', { name: 'TPU' })).closest('li')!.textContent).toContain('From the import Filament words.');

    await fireEvent.input(screen.getByLabelText('Words'), { target: { value: 'flexible filament' } });
    await fireEvent.click(screen.getByRole('button', { name: 'See what it finds' }));
    expect((await screen.findByTestId('expansion')).textContent).toContain('TPU, narrower than Flexible filament');
    expect(screen.getByText('Suggests: TPE.')).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Remove import' }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Remove import' }));
    await screen.findByText('Removed the import Filament words.');
    expect(await screen.findByText('No terms yet.')).toBeTruthy();
  });
});
