import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import SavedSearches from './SavedSearches.svelte';
import SavedSearchesCard from './SavedSearchesCard.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import { savedSearchHref, savedSearchNote, type SavedSearch } from '../lib/savedSearches';

const saved = (id: string, name: string, search: string, more: Partial<SavedSearch> = {}): SavedSearch => ({
  id,
  name,
  search,
  shared: false,
  mine: true,
  owner: { id: 'ada', name: 'Ada' },
  created_at: '',
  updated_at: '',
  ...more,
});

const ada = { id: 'ada', email: 'ada@hackspace.org', name: 'Ada', is_active: true, created_at: '', updated_at: '' };

beforeEach(() => {
  session.permissions = ['assets.read'];
  session.user = ada;
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  session.user = null;
});

describe('saved searches', () => {
  it('says whose each is, and which no longer runs, and opens them in the asset list', () => {
    expect(savedSearchNote(saved('s1', 'Mine', 'drill'))).toBe('');
    expect(savedSearchNote(saved('s1', 'Mine', 'drill', { shared: true }))).toBe('shared');
    expect(savedSearchNote(saved('s2', 'Theirs', 'drill', { mine: false, shared: true, owner: { id: 'bob', name: 'Bob' } }))).toBe('shared by Bob');
    expect(savedSearchNote(saved('s3', 'Broken', 'field.x = 1', { problem: { message: 'no field has the key "x"', start: 0, end: 7 } }))).toBe('needs fixing');
    expect(savedSearchHref(saved('s1', 'Thin', 'field.diameter < 2'))).toBe('/assets?search=field.diameter%20%3C%202');
  });

  it('saves the search shown under a name, and lists it', async () => {
    let list: SavedSearch[] = [];
    const { sent } = fakeApi({
      'GET /searches': () => ({ saved_searches: list }),
      'POST /searches': (body) => {
        const b = body as { name: string; search: string; shared: boolean };
        const s = saved('s1', b.name, b.search, { shared: b.shared });
        list = [s];
        return { status: 201, body: { saved_search: s } };
      },
    });
    render(SavedSearches, { search: 'field.diameter < 2', onopen: () => {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'Save as…' }));
    // Without searches.share, it is saved for them alone.
    expect(screen.queryByLabelText('Share with everyone who can read assets')).toBeNull();
    const name = screen.getByLabelText('Title');
    expect(document.activeElement).toBe(name);
    await fireEvent.input(name, { target: { value: 'Thin filament' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Saved searches (1)')).toBeTruthy();
    expect(sent.find((r) => r.method === 'POST')?.body).toEqual({ name: 'Thin filament', search: 'field.diameter < 2', shared: false });
    expect(screen.getByRole('link', { name: 'Thin filament' }).getAttribute('href')).toBe('/assets?search=field.diameter%20%3C%202');
  });

  it('shares one for those who may, and says why one was refused', async () => {
    session.permissions = ['assets.read', 'searches.share'];
    const { sent } = fakeApi({
      'GET /searches': () => ({ saved_searches: [] }),
      'POST /searches': () => ({ status: 409, body: { error: 'You already have a saved search of that name' } }),
    });
    render(SavedSearches, { search: 'drill', onopen: () => {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'Save as…' }));
    await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Drills' } });
    await fireEvent.click(screen.getByLabelText('Share with everyone who can read assets'));
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect((await screen.findByRole('alert')).textContent).toContain('already have a saved search of that name');
    expect(sent.find((r) => r.method === 'POST')?.body).toEqual({ name: 'Drills', search: 'drill', shared: true });
  });

  it('opens a saved one in place, and deletes only their own', async () => {
    let list = [saved('s1', 'Thin', 'field.diameter < 2'), saved('s2', 'All filament', 'category = filament', { mine: false, shared: true, owner: { id: 'bob', name: 'Bob' } })];
    fakeApi({
      'GET /searches': () => ({ saved_searches: list }),
      'DELETE /searches/s1': () => {
        list = list.slice(1);
        return { status: 204 };
      },
    });
    const opened: string[] = [];
    render(SavedSearches, { search: '', onopen: (s: string) => opened.push(s) });
    await screen.findByText('Saved searches (2)');
    // Nothing searched for, so nothing to save.
    expect(screen.queryByRole('button', { name: 'Save as…' })).toBeNull();
    await fireEvent.click(screen.getByRole('link', { name: 'Thin' }));
    expect(opened).toEqual(['field.diameter < 2']);
    expect(screen.getByText('shared by Bob')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'Delete' })).toHaveLength(1);
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Delete Thin' }));
    expect(await screen.findByText('Saved searches (1)')).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Thin' })).toBeNull();
  });

  it('shows nothing, and no error, when there are none or they cannot be read', async () => {
    session.user = null;
    fakeApi({});
    const { container } = render(SavedSearches, { search: 'drill', onopen: () => {} });
    await new Promise((r) => setTimeout(r, 20));
    expect(container.textContent?.trim()).toBe('');
  });

  it('lists them on the home page, marking one that needs fixing', async () => {
    fakeApi({
      'GET /searches': () => ({
        saved_searches: [saved('s1', 'Thin', 'field.diameter < 2', { problem: { message: 'no field has the key "diameter"', start: 0, end: 14 } })],
      }),
    });
    render(SavedSearchesCard);
    expect(await screen.findByRole('heading', { name: 'Saved searches' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Thin' }).getAttribute('href')).toBe('/assets?search=field.diameter%20%3C%202');
    expect(screen.getByText('needs fixing').getAttribute('title')).toBe('no field has the key "diameter"');
  });
});
