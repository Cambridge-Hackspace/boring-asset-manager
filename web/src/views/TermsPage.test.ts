import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import TermsPage from './TermsPage.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { Term, TermImport, TermLink, WordNetStatus } from '../lib/models';

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

  describe('seeding from WordNet', () => {
    const status = (extra: Partial<WordNetStatus> = {}): WordNetStatus => ({
      name: 'WordNet 3.1',
      source_url: 'https://wordnetcode.princeton.edu/wn3.1.dict.tar.gz',
      default_source_url: 'https://wordnetcode.princeton.edu/wn3.1.dict.tar.gz',
      sha256: 'abc',
      size: 16358468,
      cached: false,
      notice: 'WordNet 3.1 Copyright 2011 by Princeton University.',
      ...extra,
    });

    it('says it downloads once, seeds the meanings asked for, and shows the license', async () => {
      let kept = false;
      let imports: TermImport[] = [];
      const { sent } = fakeApi({
        'GET /terms': () => ({ terms: [], links: [], imports }),
        'GET /terms/wordnet': () => status({ cached: kept }),
        'POST /terms/wordnet/seed': () => {
          kept = true;
          const imp: TermImport = { id: 'w1', name: 'WordNet 3.1', terms: 12, links: 4, added_by_name: 'Ada', created_at: '' };
          imports = [imp];
          return { status: 201, body: { import: imp, downloaded: true } };
        },
      });
      render(TermsPage, { params: {} });
      const kept1 = await screen.findByTestId('wordnet-kept');
      expect(kept1.textContent).toContain('seeding downloads it once (16 MB) from https://wordnetcode.princeton.edu/wn3.1.dict.tar.gz');
      // Only an admin sets where it comes from.
      expect(screen.queryByLabelText('Download it from')).toBeNull();
      expect(screen.getByText('WordNet 3.1 Copyright 2011 by Princeton University.')).toBeTruthy();

      // The three most used, to start with.
      expect((screen.getByLabelText('Meanings of each word') as HTMLSelectElement).value).toBe('3');
      await fireEvent.change(screen.getByLabelText('Meanings of each word'), { target: { value: '2' } });
      await fireEvent.click(screen.getByRole('button', { name: 'Seed from WordNet' }));
      await screen.findByText('Seeded 12 terms and 4 links from WordNet 3.1.');
      expect(sent.find((r) => r.path === '/terms/wordnet/seed')!.body).toEqual({ senses: 2 });
      await vi.waitFor(() => expect(screen.getByTestId('wordnet-kept').textContent).toContain('Kept on this server.'));
      expect(screen.queryByLabelText('Or upload it')).toBeNull();
      expect(await screen.findByText('WordNet 3.1')).toBeTruthy();
    });

    it('says when there is nothing new', async () => {
      fakeApi({
        'GET /terms': () => ({ terms: [], links: [], imports: [] }),
        'GET /terms/wordnet': () => status({ cached: true }),
        'POST /terms/wordnet/seed': () => ({ import: null, downloaded: false, message: 'Nothing new: every term WordNet has for these names is here already' }),
      });
      render(TermsPage, { params: {} });
      await fireEvent.click(await screen.findByRole('button', { name: 'Seed from WordNet' }));
      expect(await screen.findByText('Nothing new: every term WordNet has for these names is here already.')).toBeTruthy();
    });

    it('lets an admin point it at a mirror, and anyone who manages terms upload it', async () => {
      session.permissions = ['terms.manage', 'settings.manage'];
      let source = 'https://wordnetcode.princeton.edu/wn3.1.dict.tar.gz';
      const { sent } = fakeApi({
        'GET /terms': () => ({ terms: [], links: [], imports: [] }),
        'GET /terms/wordnet': () => status({ source_url: source }),
        'PUT /terms/wordnet': (body) => {
          source = (body as { source_url: string }).source_url || status().default_source_url;
          return status({ source_url: source });
        },
        'PUT /terms/wordnet/file': () => status({ cached: true }),
      });
      render(TermsPage, { params: {} });
      await fireEvent.input(await screen.findByLabelText('Download it from'), { target: { value: ' http://mirror.lan/wn.tar.gz ' } });
      await fireEvent.click(screen.getByRole('button', { name: 'Save address' }));
      await screen.findByText('WordNet is downloaded from http://mirror.lan/wn.tar.gz.');
      expect(sent.find((r) => r.method === 'PUT' && r.path === '/terms/wordnet')!.body).toEqual({ source_url: 'http://mirror.lan/wn.tar.gz' });

      const file = new File([new Uint8Array([0x1f, 0x8b])], 'wn3.1.dict.tar.gz');
      await fireEvent.change(screen.getByLabelText('Or upload it'), { target: { files: [file] } });
      await fireEvent.click(screen.getByRole('button', { name: 'Upload WordNet' }));
      await screen.findByText('WordNet 3.1 is kept on this server.');
    });

    it("says so in its place when its status can't be read", async () => {
      fakeApi({ 'GET /terms': () => ({ terms: [], links: [], imports: [] }) });
      render(TermsPage, { params: {} });
      expect(await screen.findByText(/Seeding from WordNet isn't available/)).toBeTruthy();
      expect(screen.queryByRole('alert')).toBeNull();
    });
  });
});
