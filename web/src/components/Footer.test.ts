import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import Footer from './Footer.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import { appearance } from '../lib/theme.svelte';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  appearance.adopt('system');
  session.user = null;
});

const ada = { id: 'u1', email: 'ada@hackspace.org', name: 'Ada', is_active: true, created_at: '', updated_at: '' };

describe('Footer', () => {
  it('names the release, linked to its page', async () => {
    fakeApi({ 'GET /info': () => ({ version: 'v0.3.0', commit: 'abc', modified: false }) });
    render(Footer);
    const link = await screen.findByRole('link', { name: 'BAM! v0.3.0' });
    expect(link.getAttribute('href')).toBe('https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/tag/v0.3.0');
    expect(screen.getByRole('contentinfo')).toBeTruthy();
  });

  it('leaves the release out when the version cannot be read', async () => {
    const { fetchMock } = fakeApi({});
    render(Footer);
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(screen.queryByRole('link')).toBeNull();
    expect(screen.getByRole('group', { name: 'Appearance' })).toBeTruthy();
  });

  it('chooses light or dark over the system’s, saving it to the profile when signed in', async () => {
    session.user = ada;
    const { sent } = fakeApi({ 'GET /info': () => ({ version: 'v0.3.0', commit: 'abc', modified: false }), 'PUT /auth/theme': () => ({ user: { ...ada, theme: 'dark' } }) });
    render(Footer);
    const group = screen.getByRole('group', { name: 'Appearance' });
    expect(within(group).getAllByRole('button').map((b) => [b.textContent?.trim(), b.getAttribute('aria-pressed')])).toEqual([
      ['Light', 'false'],
      ['System', 'true'],
      ['Dark', 'false'],
    ]);
    await fireEvent.click(within(group).getByRole('button', { name: 'Dark' }));
    await vi.waitFor(() => expect(sent.find((r) => r.method === 'PUT')?.body).toEqual({ theme: 'dark' }));
    expect(within(group).getByRole('button', { name: 'Dark' }).getAttribute('aria-pressed')).toBe('true');
    expect(document.documentElement.dataset.theme).toBe('dark');
  });

  it('keeps a choice it could not save in this browser, and says so', async () => {
    session.user = ada;
    fakeApi({ 'PUT /auth/theme': () => ({ status: 500, body: { error: 'Down' } }) });
    render(Footer);
    await fireEvent.click(screen.getByRole('button', { name: 'Light' }));
    expect((await screen.findByRole('status')).textContent).toBe('Not saved to your profile; it holds in this browser.');
    expect(document.documentElement.dataset.theme).toBe('light');
  });
});
