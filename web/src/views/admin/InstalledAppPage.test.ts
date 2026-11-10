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
    });
    render(InstalledAppPage, { params: {} });
    const name = (await screen.findByLabelText('Name')) as HTMLInputElement;
    expect(name.placeholder).toBe('Cambridge Hackspace');
    expect(screen.getByText(/Left empty, the instance's name/).textContent).toContain('Cambridge Hackspace');
    expect(screen.getByRole('option', { name: 'logo.png' })).toBeTruthy();

    await fireEvent.input(screen.getByLabelText('Short name'), { target: { value: 'Stores' } });
    await fireEvent.change(screen.getByLabelText('Drawn from'), { target: { value: 'img-1' } });
    await fireEvent.input(screen.getByLabelText('Bar above the app, light'), { target: { value: '#d41920' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByText(/Saved/);
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
    fakeApi({ 'GET /settings/app': () => ({ ...settings, theme_color: '#000000', background_color: '#123456' }), 'GET /images': () => ({ images: [] }) });
    render(InstalledAppPage, { params: {} });
    const light = (await screen.findByLabelText('Bar above the app, light')) as HTMLInputElement;
    expect(light.value).toBe('#000000');
    await fireEvent.click(screen.getByRole('button', { name: "BAM!'s colors" }));
    expect(light.value).toBe('#faf7f0');
    expect((screen.getByLabelText('Behind the icon, and as it opens') as HTMLInputElement).value).toBe('#faf7f0');
  });
});
