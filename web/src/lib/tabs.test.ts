import { describe, expect, it } from 'vitest';
import { currentTab, tabHref } from './tabs';

const keys = ['about', 'fields', 'labels'];

describe('tabs in the address', () => {
  it('reads the tab named, or the first', () => {
    expect(currentTab('?tab=fields', keys)).toBe('fields');
    expect(currentTab('', keys)).toBe('about');
    expect(currentTab('?tab=nope', keys)).toBe('about');
    expect(currentTab('?tab=labels', [])).toBeUndefined();
  });

  it('links each tab, leaving out the first and keeping other parameters', () => {
    expect(tabHref('/categories/c1', '?tab=fields&x=1', keys, 'about')).toBe('/categories/c1?x=1');
    expect(tabHref('/categories/c1', '', keys, 'labels')).toBe('/categories/c1?tab=labels');
    expect(tabHref('/categories/c1', '?tab=about', keys, 'about')).toBe('/categories/c1');
  });
});
