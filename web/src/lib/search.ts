// Searching from outside the asset list (docs/data-model.md#search): the
// home page's search field hands its words, or a query, to the asset list,
// which may go straight to a search's one result.

import { request } from './api';
import type { AssetWithDetails, SearchSettings } from './models';
import { session } from './session.svelte';

/**
 * Where a search goes: the asset list, with the search in its address as
 * the list reads it. The list goes on to a search's one result, when the
 * person goes straight there (searchDirect).
 */
export function searchHref(text: string): string {
  const q = text.trim();
  return q ? `/assets?search=${encodeURIComponent(q)}` : '/assets';
}

/**
 * All of a search's results, listed: the asset list, told not to go
 * straight to a one result again, so the way back from it is not a loop.
 */
export function allResultsHref(text: string): string {
  const q = text.trim();
  return q ? `/assets?search=${encodeURIComponent(q)}&all=1` : '/assets';
}

/**
 * An asset's configured page: its scan page, laid out by the scan template
 * its categories choose (docs/scanning.md), or null where none applies.
 */
export function configuredPage(a: AssetWithDetails): string | null {
  return a.scan_template?.template_id ? `/scan/${encodeURIComponent(a.asset.id)}` : null;
}

/**
 * Where a search's one result opens: its configured page, or its own page
 * where it has none, with the search, so the page can lead back to all the
 * results.
 */
export function straightTo(a: AssetWithDetails, text: string): string {
  const page = (session.can('assets.scan') && configuredPage(a)) || `/assets/${encodeURIComponent(a.asset.id)}`;
  return `${page}?search=${encodeURIComponent(text.trim())}`;
}

/**
 * Whether a search that finds exactly one asset opens it straight away:
 * the person's own choice, or the instance's setting where they have made
 * none, as signed-out visitors have not. Off when it cannot be read.
 */
export async function searchDirect(): Promise<boolean> {
  const own = session.user?.search_direct;
  if (own === true || own === false) return own;
  try {
    return (await request<SearchSettings>('GET', '/settings/search')).direct;
  } catch {
    return false;
  }
}

/**
 * Whether a field may take focus by itself when a page opens: only with a
 * precise pointer, such as a mouse, so a phone's keyboard doesn't rise
 * unasked.
 */
export function focusOnOpen(win: Window = window): boolean {
  return win.matchMedia?.('(pointer: fine)').matches ?? false;
}

/**
 * Whether a key press means "go to search", as `/` does on many sites: a
 * plain `/`, typed while not already writing somewhere.
 */
export function isSearchKey(e: KeyboardEvent): boolean {
  if (e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return false;
  const t = e.target;
  if (!(t instanceof HTMLElement)) return true;
  return !(t.isContentEditable || t.closest('input, textarea, select, [contenteditable=""], [contenteditable="true"]'));
}
