// Tabs kept in the address as ?tab= (components/TabLinks.svelte), so a
// section of a long page can be linked to and survives a reload.

/** The tab the address names, or the first when it names none of them. */
export function currentTab(search: string, keys: string[]): string | undefined {
  const named = new URLSearchParams(search).get('tab');
  return named !== null && keys.includes(named) ? named : keys[0];
}

/** The address of a tab: the first has no ?tab=, and other parameters stay. */
export function tabHref(path: string, search: string, keys: string[], key: string): string {
  const q = new URLSearchParams(search);
  if (key === keys[0]) q.delete('tab');
  else q.set('tab', key);
  const s = q.toString();
  return path + (s ? `?${s}` : '');
}
