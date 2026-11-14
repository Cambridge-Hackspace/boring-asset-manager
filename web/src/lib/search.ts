// Searching from outside the asset list (docs/data-model.md#search): the
// home page's search field hands its words, or a query, to the asset list.

/**
 * Where a search goes: the asset list, with the search in its address as
 * the list reads it. Going straight to a single result (#389) belongs here.
 */
export function searchHref(text: string): string {
  const q = text.trim();
  return q ? `/assets?search=${encodeURIComponent(q)}` : '/assets';
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
