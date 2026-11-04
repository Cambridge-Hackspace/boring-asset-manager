// What the signed-in person has starred (#225, docs/favorites.md), read once
// and shared, so every star on a page agrees.
import { request } from './api';
import type { Favorite, FavoriteKind } from './models';
import { session } from './session.svelte';

function path(kind: FavoriteKind, id: string): string {
  return `/auth/me/favorites/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`;
}

class Favorites {
  list = $state<Favorite[]>([]);
  // Whose favorites list holds, so someone else signing in reads their own.
  #user: string | null = null;
  #loading: Promise<void> | null = null;

  has(kind: FavoriteKind, id: string): boolean {
    return this.list.some((f) => f.kind === kind && f.id === id);
  }

  /** Reads them once for each person signed in, or again when asked. */
  load(again = false): Promise<void> {
    const user = session.user?.id ?? null;
    if (!user) {
      this.list = [];
      this.#user = null;
      this.#loading = null;
      return Promise.resolve();
    }
    if (!again && this.#user === user && this.#loading) return this.#loading;
    this.#user = user;
    const loading = request<{ favorites: Favorite[] }>('GET', '/auth/me/favorites')
      .then((r) => {
        if (this.#user === user) this.list = r.favorites ?? [];
      })
      .catch((err) => {
        if (this.#loading === loading) this.#loading = null;
        throw err;
      });
    this.#loading = loading;
    return loading;
  }

  /** Forgets them, so the next load reads them afresh. */
  reset(): void {
    this.list = [];
    this.#user = null;
    this.#loading = null;
  }

  /** Stars something, or unstars it. */
  async set(kind: FavoriteKind, id: string, starred: boolean): Promise<void> {
    if (starred) {
      const { favorite } = await request<{ favorite: Favorite }>('PUT', path(kind, id));
      if (!this.has(kind, id)) this.list = [...this.list, favorite];
    } else {
      await request('DELETE', path(kind, id));
      this.list = this.list.filter((f) => !(f.kind === kind && f.id === id));
    }
  }
}

export const favorites = new Favorites();
