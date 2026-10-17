import { describe, expect, it } from 'vitest';
import { helpFor, helpTopic, helpTopics } from './help';
import { routes } from '../routes';

describe('help', () => {
  it('has every topic, titled, in order', () => {
    expect(helpTopics.map((t) => t.slug)).toEqual(['start', 'assets', 'stock', 'loans', 'categories', 'links', 'import', 'labels', 'spool', 'scanning', 'reports', 'people', 'notifications', 'account']);
    for (const t of helpTopics) expect(t.title, t.slug).not.toBe('');
  });

  it('gives every page a topic that exists', () => {
    for (const r of routes) {
      const path = r.pattern.replace(/:[a-z]+/g, 'x');
      expect(helpTopic(helpFor(path)), path).toBeTruthy();
    }
    expect(helpFor('/assets/import')).toBe('import');
    expect(helpFor('/assets/x/edit')).toBe('assets');
    expect(helpFor('/scan/BIN-A3')).toBe('scanning');
    expect(helpFor('/roles')).toBe('people');
    expect(helpFor('/spool')).toBe('spool');
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
