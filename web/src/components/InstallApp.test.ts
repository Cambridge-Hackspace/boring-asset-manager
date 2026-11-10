import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import InstallApp from './InstallApp.svelte';
import UpdatePrompt from './UpdatePrompt.svelte';
import { installedApp } from '../lib/install/app.svelte';

afterEach(() => {
  cleanup();
  installedApp.installable = installedApp.ios = installedApp.standalone = false;
  installedApp.waiting = null;
});

describe('InstallApp', () => {
  it('shows nothing where the browser cannot install it', () => {
    render(InstallApp);
    expect(screen.queryByRole('heading', { name: 'Install BAM!' })).toBeNull();
  });

  it('offers to install where the browser allows, as Chrome on Android does', async () => {
    installedApp.installable = true;
    const install = vi.spyOn(installedApp, 'install').mockResolvedValue(true);
    render(InstallApp);
    await fireEvent.click(screen.getByRole('button', { name: 'Install' }));
    expect(install).toHaveBeenCalled();
    install.mockRestore();
  });

  it('says how on an iPhone, where no browser offers to', () => {
    installedApp.ios = true;
    render(InstallApp);
    expect(screen.getByText(/tap/).textContent).toContain('Share');
    expect(screen.getByText(/tap/).textContent).toContain('Add to Home Screen');
    expect(screen.queryByRole('button', { name: 'Install' })).toBeNull();
  });

  it('shows nothing once installed', () => {
    installedApp.installable = installedApp.standalone = true;
    render(InstallApp);
    expect(screen.queryByRole('heading', { name: 'Install BAM!' })).toBeNull();
  });
});

describe('UpdatePrompt', () => {
  it('appears when a new release waits, and reloads to it when asked', async () => {
    const { rerender } = render(UpdatePrompt);
    expect(screen.queryByRole('button', { name: 'Reload to update' })).toBeNull();
    installedApp.waiting = {} as ServiceWorker;
    await rerender({});
    const update = vi.spyOn(installedApp, 'update').mockImplementation(() => {});
    await fireEvent.click(await screen.findByRole('button', { name: 'Reload to update' }));
    expect(update).toHaveBeenCalled();
    update.mockRestore();
  });
});
