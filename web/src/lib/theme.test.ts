import { afterEach, describe, expect, it, vi } from 'vitest';
import { fakeApi } from '../test/fetch';
import { appearance, applyTheme, asTheme, storedTheme } from './theme.svelte';

afterEach(() => {
  appearance.adopt('system');
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe('theme', () => {
  it('takes light or dark, and the system’s for anything else', () => {
    expect(asTheme('dark')).toBe('dark');
    expect(asTheme('light')).toBe('light');
    expect(asTheme('system')).toBe('system');
    expect(asTheme('sepia')).toBe('system');
    expect(asTheme(null)).toBe('system');
  });

  it('marks the page only for a choice, leaving the system’s to CSS', () => {
    const root = document.createElement('html');
    applyTheme('dark', root);
    expect(root.dataset.theme).toBe('dark');
    applyTheme('system', root);
    expect(root.hasAttribute('data-theme')).toBe(false);
  });

  it('keeps a choice in this browser, and forgets it at the system’s', () => {
    appearance.adopt('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(localStorage.getItem('bam.theme')).toBe('dark');
    expect(storedTheme()).toBe('dark');
    appearance.adopt('system');
    expect(localStorage.getItem('bam.theme')).toBeNull();
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
    localStorage.setItem('bam.theme', 'mauve');
    expect(storedTheme()).toBe('system');
  });

  it('saves a choice to the profile only when signed in', async () => {
    const { sent } = fakeApi({ 'PUT /auth/theme': () => ({ user: {} }) });
    await appearance.choose('light', false);
    expect(sent).toHaveLength(0);
    expect(appearance.theme).toBe('light');
    await appearance.choose('dark', true);
    expect(sent.map((r) => [r.method, r.path, r.body])).toEqual([['PUT', '/auth/theme', { theme: 'dark' }]]);
    expect(document.documentElement.dataset.theme).toBe('dark');
  });
});
