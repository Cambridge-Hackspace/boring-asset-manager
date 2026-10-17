// The main navigation, as the header and the home page show it: everyday
// pages on their own, the rest in groups.

import type { Permission } from './models';

/** The groups pages are listed under, in the order they appear. */
export const navGroups = ['Catalog', 'Labels', 'Admin'] as const;
export type NavGroup = (typeof navGroups)[number];

/** What navigation needs of a route. */
export interface NavRoute {
  pattern: string;
  nav?: string;
  group?: NavGroup;
  permissions?: Permission[];
}

export interface NavLink {
  href: string;
  label: string;
}

/** A run of pages on their own (no label), or a group of them. */
export interface NavSection {
  group?: NavGroup;
  links: NavLink[];
}

/**
 * The sections someone sees: the pages on their own first, then each group
 * that has a page they can open, in route order within each.
 */
export function navSections(routes: NavRoute[], canAny: (...permissions: Permission[]) => boolean): NavSection[] {
  const visible = routes.filter((r) => r.nav && (!r.permissions || canAny(...r.permissions)));
  const link = (r: NavRoute): NavLink => ({ href: r.pattern, label: r.nav! });
  const sections: NavSection[] = [];
  const alone = visible.filter((r) => !r.group).map(link);
  if (alone.length) sections.push({ links: alone });
  for (const group of navGroups) {
    const links = visible.filter((r) => r.group === group).map(link);
    if (links.length) sections.push({ group, links });
  }
  return sections;
}

/** Whether path is the page at href, or a page within it. */
export function isCurrent(href: string, path: string): boolean {
  return href === '/' ? path === '/' : path === href || path.startsWith(href + '/');
}
