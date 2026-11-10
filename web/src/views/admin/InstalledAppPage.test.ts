import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import InstalledAppPage from './InstalledAppPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { AppSettings } from '../../lib/install/app.svelte';

const settings: AppSettings = {
  name: '',
  short_name: '',
  theme_color: '#faf7f0',
  theme_color_dark: '#0b0b0a',
  background_color: '#faf7f0',
  icon_image_id: '',
  instance_name: 'Cambridge Hackspace',
};

const offlineDefaults = { enabled: true, refresh_minutes: 60, thumbnails: true };

beforeEach(() => {
  session.permissions = ['settings.manage', 'assets.read'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('InstalledAppPage', () => {
  it("names the app after the instance until it is given a name, and saves what is chosen", async () => {
    const { sent } = fakeApi({
      'GET /settings/app': () => settings,
      'GET /images': () => ({ images: [{ id: 'img-1', filename: 'logo.png', content_type: 'image/png', width_px: 512, height_px: 512 }] }),
      'PUT /settings/app': (body) => ({ ...settings, ...(body as object) }),
      'GET /settings/offline': () => offlineDefaults,
      'PUT /settings/offline': (body) => body,
    });
    render(InstalledAppPage, { params: {} });
    const name = (await screen.findByLabelText('Name')) as HTMLInputElement;
    expect(name.placeholder).toBe('Cambridge Hackspace');
    expect(screen.getByText(/Left empty, the instance's name/).textContent).toContain('Cambridge Hackspace');
    expect(screen.getByRole('option', { name: 'logo.png' })).toBeTruthy();

    await fireEvent.input(screen.getByLabelText('Short name'), { target: { value: 'Stores' } });
    await fireEvent.change(screen.getByLabelText('Drawn from'), { target: { value: 'img-1' } });
    await fireEvent.input(screen.getByLabelText('Bar above the app, light'), { target: { value: '#d41920' } });
    // How often copies refresh, and whether thumbnails are kept, are the instance's to choose.
    expect((screen.getByLabelText('Keep copies for use offline') as HTMLInputElement).checked).toBe(true);
    await fireEvent.input(screen.getByLabelText('Refresh each copy every (minutes)'), { target: { value: '15' } });
    await fireEvent.click(screen.getByLabelText('Let people keep thumbnails of photos in their copy'));
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByText(/Saved/);
    expect(sent.find((r) => r.method === 'PUT' && r.path === '/settings/offline')?.body).toEqual({ enabled: true, refresh_minutes: 15, thumbnails: false });
    expect(sent.find((r) => r.method === 'PUT')?.body).toEqual({
      name: '',
      short_name: 'Stores',
      theme_color: '#d41920',
      theme_color_dark: '#0b0b0a',
      background_color: '#faf7f0',
      icon_image_id: 'img-1',
    });
  });

  it("puts BAM!'s colors back", async () => {
    fakeApi({
      'GET /settings/app': () => ({ ...settings, theme_color: '#000000', background_color: '#123456' }),
      'GET /settings/offline': () => offlineDefaults,
      'GET /images': () => ({ images: [] }),
    });
    render(InstalledAppPage, { params: {} });
    const light = (await screen.findByLabelText('Bar above the app, light')) as HTMLInputElement;
    expect(light.value).toBe('#000000');
    await fireEvent.click(screen.getByRole('button', { name: "BAM!'s colors" }));
    expect(light.value).toBe('#faf7f0');
    expect((screen.getByLabelText('Behind the icon, and as it opens') as HTMLInputElement).value).toBe('#faf7f0');
  });
});
