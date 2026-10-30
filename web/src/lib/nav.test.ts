import { describe, expect, it } from 'vitest';
import type { Permission } from './models';
import { isCurrent, navSections } from './nav';
import { routes } from '../routes';

const holding =
  (...held: Permission[]) =>
  (...p: Permission[]) =>
    p.some((x) => held.includes(x));

const labels = (held: Permission[]) =>
  navSections(routes, holding(...held)).map((s) => [s.group ?? '', s.links.map((l) => l.label)]);

describe('navSections', () => {
  it('lists everyday pages on their own, then each group', () => {
    const all: Permission[] = [
      'assets.read', 'assets.scan', 'assets.track', 'labels.print', 'categories.manage', 'templates.manage', 'settings.manage', 'users.manage',
    ];
    expect(labels(all)).toEqual([
      ['', ['Assets', 'Scan', 'Loans', 'Print queue', 'Reports']],
      ['Catalog', ['Categories', 'Fields', 'Link types', 'Actions', 'Icons']],
      ['Labels', ['Templates', 'Images', 'Printers']],
      ['Admin', ['Users', 'Roles', 'API tokens', 'Notifications', 'Backups', 'Import & export']],
    ]);
  });

  it('shows only the pages someone can open, and drops empty groups', () => {
    expect(labels(['assets.read'])).toEqual([
      ['', ['Assets', 'Reports']],
      ['Catalog', ['Categories']],
    ]);
    expect(labels(['labels.print'])).toEqual([
      ['', ['Print queue']],
      ['Labels', ['Printers']],
    ]);
    expect(labels([])).toEqual([]);
  });

  it('lists every navigation page in a known group or on its own', () => {
    for (const r of routes.filter((r) => r.nav)) expect(['Catalog', 'Labels', 'Admin', undefined]).toContain(r.group);
  });
});

describe('isCurrent', () => {
  it('matches a page and the pages within it', () => {
    expect(isCurrent('/assets', '/assets')).toBe(true);
    expect(isCurrent('/assets', '/assets/a1/edit')).toBe(true);
    expect(isCurrent('/assets', '/assetsx')).toBe(false);
    expect(isCurrent('/', '/assets')).toBe(false);
    expect(isCurrent('/', '/')).toBe(true);
  });
});
