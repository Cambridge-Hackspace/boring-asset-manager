import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import FavoriteStar from './FavoriteStar.svelte';
import Home from '../views/Home.svelte';
import { fakeApi } from '../test/fetch';
import { favorites } from '../lib/favorites.svelte';
import { session } from '../lib/session.svelte';
import type { Favorite, User } from '../lib/models';

const ada = { id: 'u1', email: 'ada@hackspace.org', name: 'Ada', is_active: true, created_at: '', updated_at: '' } as User;
const fav = (over: Partial<Favorite>): Favorite => ({ kind: 'asset', id: 'a1', name: 'Bandsaw', created_at: '2026-11-03T10:00:00Z', ...over });

beforeEach(() => {
  session.user = ada;
  session.permissions = [];
  favorites.reset();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  session.user = null;
});

describe('FavoriteStar', () => {
  it('stars and unstars, pressed while a favorite', async () => {
    const { sent } = fakeApi({
      'GET /auth/me/favorites': () => ({ favorites: [] }),
      'PUT /auth/me/favorites/asset/a1': () => ({ favorite: fav({}) }),
      'DELETE /auth/me/favorites/asset/a1': () => ({ status: 204 }),
    });
    render(FavoriteStar, { kind: 'asset', id: 'a1', name: 'Bandsaw' });
    const star = screen.getByRole('button', { name: 'Favorite Bandsaw' });
    expect(star.getAttribute('aria-pressed')).toBe('false');

    await fireEvent.click(star);
    await vi.waitFor(() => expect(star.getAttribute('aria-pressed')).toBe('true'));
    expect(favorites.has('asset', 'a1')).toBe(true);

    await fireEvent.click(star);
    await vi.waitFor(() => expect(star.getAttribute('aria-pressed')).toBe('false'));
    expect(sent.filter((r) => r.method !== 'GET').map((r) => `${r.method} ${r.path}`)).toEqual([
      'PUT /auth/me/favorites/asset/a1',
      'DELETE /auth/me/favorites/asset/a1',
    ]);
  });

  it('shows a favorite already starred as pressed, and is not there signed out', async () => {
    fakeApi({ 'GET /auth/me/favorites': () => ({ favorites: [fav({ kind: 'report', id: 'r1', name: 'Everything' })] }) });
    render(FavoriteStar, { kind: 'report', id: 'r1', name: 'Everything' });
    const star = screen.getByRole('button', { name: 'Favorite Everything' });
    await vi.waitFor(() => expect(star.getAttribute('aria-pressed')).toBe('true'));
    cleanup();

    session.user = null;
    render(FavoriteStar, { kind: 'report', id: 'r1', name: 'Everything' });
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('says why a star could not be changed', async () => {
    fakeApi({
      'GET /auth/me/favorites': () => ({ favorites: [] }),
      'PUT /auth/me/favorites/asset/a1': () => ({ status: 404, body: { error: 'Nothing to star: it is not there, or you cannot see it' } }),
    });
    render(FavoriteStar, { kind: 'asset', id: 'a1', name: 'Bandsaw' });
    await fireEvent.click(screen.getByRole('button', { name: 'Favorite Bandsaw' }));
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Nothing to star: it is not there, or you cannot see it');
  });
});

describe('Home', () => {
  it('shows the favorites by kind, each by name, without archived assets', async () => {
    fakeApi({
      'GET /auth/me/favorites': () => ({
        favorites: [
          fav({ id: 'a2', name: 'Drill' }),
          fav({ id: 'a1', name: 'Bandsaw' }),
          fav({ id: 'a3', name: 'Old lathe', archived: true }),
          fav({ kind: 'flow', id: 'f1', name: 'Leave wood', slug: 'leave-wood' }),
          fav({ kind: 'category', id: 'c1', name: 'Saws' }),
        ],
      }),
      'GET /loans': () => ({ loans: [] }),
    });
    render(Home, { params: {} });
    const card = await screen.findByRole('region', { name: 'Favorites' });
    expect(within(card).getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual(['Assets', 'Categories', 'Flows']);
    const links = within(card).getAllByRole('link');
    expect(links.map((l) => [l.textContent, l.getAttribute('href')])).toEqual([
      ['Bandsaw', '/assets/a1'],
      ['Drill', '/assets/a2'],
      ['Saws', '/categories/c1'],
      ['Leave wood', '/f/leave-wood'],
    ]);
  });

  it('says how to add favorites when there are none', async () => {
    fakeApi({ 'GET /auth/me/favorites': () => ({ favorites: [] }), 'GET /loans': () => ({ loans: [] }) });
    render(Home, { params: {} });
    const card = await screen.findByRole('region', { name: 'Favorites' });
    expect(card.textContent).toContain('Star an asset, category, report or flow on its page to find it here.');
  });
});
