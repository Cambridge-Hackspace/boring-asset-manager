import { afterEach, describe, expect, it, vi } from 'vitest';
import { allResultsHref, configuredPage, focusOnOpen, isSearchKey, searchDirect, searchHref, straightTo } from './search';
import { fakeApi } from '../test/fetch';
import { session } from './session.svelte';
import type { AssetWithDetails, User } from './models';

const found = (template_id: string) => ({ asset: { id: 'a 1', name: 'Drill' }, scan_template: { template_id } }) as unknown as AssetWithDetails;
const ada = (search_direct?: boolean | null) => ({ id: 'ada', email: 'ada@hackspace.org', name: 'Ada', is_active: true, created_at: '', updated_at: '', search_direct }) as User;

afterEach(() => {
  vi.unstubAllGlobals();
  session.user = null;
  session.permissions = [];
});

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

  it('lists all the results without going straight to one again', () => {
    expect(allResultsHref(' drill ')).toBe('/assets?search=drill&all=1');
    expect(allResultsHref('')).toBe('/assets');
  });

  it('opens a one result on its scan page where a scan template applies and the viewer scans', () => {
    expect(configuredPage(found('t'))).toBe('/scan/a%201');
    expect(configuredPage(found(''))).toBeNull();
    session.permissions = ['assets.read', 'assets.scan'];
    expect(straightTo(found('t'), ' dri ')).toBe('/scan/a%201?search=dri');
    expect(straightTo(found(''), 'dri')).toBe('/assets/a%201?search=dri');
    session.permissions = ['assets.read'];
    expect(straightTo(found('t'), 'dri')).toBe('/assets/a%201?search=dri');
  });

  it('goes straight there as the person chose, or else as the instance does', async () => {
    const { sent } = fakeApi({ 'GET /settings/search': () => ({ direct: true }) });
    session.user = ada(false);
    expect(await searchDirect()).toBe(false);
    session.user = ada(true);
    expect(await searchDirect()).toBe(true);
    expect(sent).toHaveLength(0);
    session.user = ada(null);
    expect(await searchDirect()).toBe(true);
    session.user = null;
    expect(await searchDirect()).toBe(true);
    fakeApi({});
    expect(await searchDirect()).toBe(false);
  });
});
