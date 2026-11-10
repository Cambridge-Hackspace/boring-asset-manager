import { describe, expect, it } from 'vitest';
import { helpFor, helpTopic, helpTopics } from './help';
import { routes } from '../routes';

describe('help', () => {
  it('has every topic, titled, in order', () => {
    expect(helpTopics.map((t) => t.slug)).toEqual(['start', 'assets', 'stock', 'loans', 'categories', 'vendors', 'contracts', 'orders', 'review', 'comments', 'icons', 'links', 'import', 'labels', 'spool', 'scanning', 'install', 'flows', 'hooks', 'reports', 'people', 'notifications', 'relays', 'account', 'assistants']);
    for (const t of helpTopics) expect(t.title, t.slug).not.toBe('');
  });

  it('gives every page a topic that exists', () => {
    for (const r of routes) {
      const path = r.pattern.replace(/:[a-z]+/g, 'x');
      expect(helpTopic(helpFor(path)), path).toBeTruthy();
    }
    expect(helpFor('/assets/import')).toBe('import');
    expect(helpFor('/assets/graph')).toBe('links');
    expect(helpFor('/oauth/consent')).toBe('assistants');
    expect(helpFor('/assets/x/edit')).toBe('assets');
    expect(helpFor('/f/leave')).toBe('flows');
    expect(helpFor('/flows/leave/edit')).toBe('flows');
    expect(helpFor('/scan/BIN-A3')).toBe('scanning');
    expect(helpFor('/roles')).toBe('people');
    expect(helpFor('/spool')).toBe('spool');
    expect(helpFor('/hooks')).toBe('hooks');
    expect(helpFor('/vendors/x')).toBe('vendors');
    expect(helpFor('/contracts/x')).toBe('contracts');
    expect(helpFor('/orders/x/receive')).toBe('orders');
    expect(helpFor('/relays')).toBe('relays');
    expect(helpFor('/installed-app')).toBe('install');
    expect(helpFor('/shopping')).toBe('stock');
    expect(helpFor('/')).toBe('start');
  });

  it('links only to topics that exist', () => {
    for (const t of helpTopics) {
      for (const [, target] of t.body.matchAll(/\]\(\/help\/([^)]+)\)/g)) {
        expect(helpTopic(target!), `${t.slug} links to ${target}`).toBeTruthy();
      }
    }
  });

  it('has no question marks: it says, rather than asks', () => {
    for (const t of helpTopics) expect(t.body, t.slug).not.toContain('?');
  });
});
