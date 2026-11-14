import { describe, expect, it } from 'vitest';
import { focusOnOpen, isSearchKey, searchHref } from './search';

describe('search', () => {
  it('sends a search to the asset list', () => {
    expect(searchHref('  drill ')).toBe('/assets?search=drill');
    expect(searchHref('in ~ "shelf a" & more')).toBe('/assets?search=in%20~%20%22shelf%20a%22%20%26%20more');
    expect(searchHref('   ')).toBe('/assets');
  });

  it('focuses by itself only with a precise pointer', () => {
    const win = (fine: boolean) => ({ matchMedia: (q: string) => ({ matches: fine && q === '(pointer: fine)' }) }) as unknown as Window;
    expect(focusOnOpen(win(true))).toBe(true);
    expect(focusOnOpen(win(false))).toBe(false);
    expect(focusOnOpen({} as Window)).toBe(false);
  });

  it('takes a plain / outside fields as the search key', () => {
    const key = (init: KeyboardEventInit, target: EventTarget = document.body) => {
      const e = new KeyboardEvent('keydown', init);
      Object.defineProperty(e, 'target', { value: target });
      return e;
    };
    expect(isSearchKey(key({ key: '/' }))).toBe(true);
    expect(isSearchKey(key({ key: '/', ctrlKey: true }))).toBe(false);
    expect(isSearchKey(key({ key: 'a' }))).toBe(false);
    expect(isSearchKey(key({ key: '/' }, document.createElement('textarea')))).toBe(false);
    const editable = document.createElement('div');
    editable.setAttribute('contenteditable', 'true');
    expect(isSearchKey(key({ key: '/' }, editable))).toBe(false);
  });
});
