import { describe, expect, it } from 'vitest';
import { interceptLinks, matchRoute, navigate, router } from './router.svelte';

describe('matchRoute', () => {
  it('matches literal and parameter segments', () => {
    expect(matchRoute('/', '/')).toEqual({});
    expect(matchRoute('/assets', '/assets/')).toEqual({});
    expect(matchRoute('/assets/:id', '/assets/abc-123')).toEqual({ id: 'abc-123' });
    expect(matchRoute('/templates/:id/edit', '/templates/qr%20small/edit')).toEqual({ id: 'qr small' });
  });

  it('rejects other paths', () => {
    expect(matchRoute('/assets/:id', '/assets')).toBeNull();
    expect(matchRoute('/assets/:id', '/assets/1/2')).toBeNull();
    expect(matchRoute('/assets', '/categories')).toBeNull();
    expect(matchRoute('/assets/:id', '/assets/%E0%A4%A')).toBeNull(); // malformed escape
  });
});

describe('navigate', () => {
  it('updates the location and the router state', () => {
    navigate('/assets?search=drill');
    expect(window.location.pathname).toBe('/assets');
    expect(router.path).toBe('/assets');
    expect(router.query.get('search')).toBe('drill');

    navigate('/categories', true);
    expect(router.path).toBe('/categories');
    history.back();
  });
});

describe('interceptLinks', () => {
  function click(anchor: HTMLAnchorElement, init: MouseEventInit = {}): boolean {
    const event = new MouseEvent('click', { bubbles: true, cancelable: true, button: 0, ...init });
    anchor.dispatchEvent(event);
    return event.defaultPrevented;
  }

  it('handles local links and leaves the rest to the browser', () => {
    document.body.innerHTML = `
      <a id="local" href="/templates">x</a>
      <a id="api" href="/api/v1/assets/1/label?format=pdf">x</a>
      <a id="external" href="https://example.com/">x</a>
      <a id="blank" href="/assets" target="_blank">x</a>
      <a id="download" href="/assets" download>x</a>`;
    const stop = interceptLinks(document.body);
    const el = (id: string) => document.getElementById(id) as HTMLAnchorElement;

    expect(click(el('local'))).toBe(true);
    expect(router.path).toBe('/templates');
    expect(click(el('api'))).toBe(false);
    expect(click(el('external'))).toBe(false);
    expect(click(el('blank'))).toBe(false);
    expect(click(el('download'))).toBe(false);
    expect(click(el('local'), { ctrlKey: true })).toBe(false);

    stop();
    expect(click(el('local'))).toBe(false);
  });
});
