// Saved searches (#390, docs/data-model.md#saved-searches): a search,
// plain words or a query, kept under a name to run again. Each is its
// person's own; shared ones are listed for everyone who can read assets.
import { request } from './api';
import type { QueryProblem } from './query';
import { searchHref } from './search';

export interface SavedSearch {
  id: string;
  name: string;
  /** Plain words or a query, as the asset list reads it. */
  search: string;
  shared: boolean;
  /** The person's own, which they may change and delete. */
  mine: boolean;
  owner: { id: string; name: string };
  /** Where a query that no longer runs is wrong, such as a field it names that was deleted. */
  problem?: QueryProblem;
  created_at: string;
  updated_at: string;
}

/** The person's saved searches, their own first, then those shared. */
export async function listSavedSearches(): Promise<SavedSearch[]> {
  return (await request<{ saved_searches: SavedSearch[] | null }>('GET', '/searches')).saved_searches ?? [];
}

/** Saves a search under a name; a query with a mistake is refused, saying where. */
export async function saveSearch(name: string, search: string, shared: boolean): Promise<SavedSearch> {
  return (await request<{ saved_search: SavedSearch }>('POST', '/searches', { name, search, shared })).saved_search;
}

export async function deleteSavedSearch(id: string): Promise<void> {
  await request('DELETE', `/searches/${encodeURIComponent(id)}`);
}

/** Where opening a saved search goes: the asset list, searching as it was saved. */
export function savedSearchHref(s: SavedSearch): string {
  return searchHref(s.search);
}

/** How a saved search is described beside its name: whose, and whether it still runs. */
export function savedSearchNote(s: SavedSearch): string {
  const notes: string[] = [];
  if (!s.mine) notes.push(`shared by ${s.owner.name}`);
  else if (s.shared) notes.push('shared');
  if (s.problem) notes.push('needs fixing');
  return notes.join(', ');
}
