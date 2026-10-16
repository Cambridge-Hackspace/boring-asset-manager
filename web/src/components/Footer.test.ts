import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/svelte';
import Footer from './Footer.svelte';
import { fakeApi } from '../test/fetch';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('Footer', () => {
  it('names the release, linked to its page', async () => {
    fakeApi({ 'GET /info': () => ({ version: 'v0.3.0', commit: 'abc', modified: false }) });
    render(Footer);
    const link = await screen.findByRole('link', { name: 'BAM! v0.3.0' });
    expect(link.getAttribute('href')).toBe('https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/tag/v0.3.0');
    expect(screen.getByRole('contentinfo')).toBeTruthy();
  });

  it('shows nothing when the version cannot be read', async () => {
    const { fetchMock } = fakeApi({});
    render(Footer);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(screen.queryByRole('contentinfo')).toBeNull();
  });
});
