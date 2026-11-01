import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import HooksPage from './HooksPage.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { AssetHook } from '../lib/models';

beforeEach(() => {
  session.permissions = ['assets.read', 'categories.manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const identify: AssetHook = { id: 'h1', key: 'identify', label: 'Identify', confirm: '', created_at: '', updated_at: '' };

describe('HooksPage', () => {
  it('defines, edits and deletes hooks', async () => {
    let hooks: AssetHook[] = [];
    const { sent } = fakeApi({
      'GET /hooks': () => ({ hooks }),
      'POST /hooks': (body) => {
        hooks = [{ id: 'a1', ...(body as { key: string; label: string; confirm: string }), created_at: '', updated_at: '' }];
        return { status: 201, body: { hook: hooks[0] } };
      },
      'PUT /hooks/a1': (body) => {
        hooks = [{ ...hooks[0]!, ...(body as object) }];
        return { hook: hooks[0] };
      },
      'DELETE /hooks/a1': () => {
        hooks = [];
        return { message: 'Hook deleted' };
      },
    });
    render(HooksPage, { params: {} });
    expect(await screen.findByText('No hooks yet.')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'New hook' }));
    const form = screen.getByRole('form', { name: 'New hook' });
    await fireEvent.input(within(form).getByLabelText('Label'), { target: { value: 'Identify' } });
    // Its key is made from its label.
    expect(within(form).getByText('identify')).toBeTruthy();
    await fireEvent.submit(within(form).getByRole('button', { name: 'Save hook' }));
    expect(await screen.findByText('Saved Identify.')).toBeTruthy();
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ key: 'identify', label: 'Identify', confirm: '' });

    await fireEvent.click(await screen.findByRole('button', { name: 'Edit Identify' }));
    await fireEvent.input(screen.getByLabelText('Ask first (optional)'), { target: { value: 'Light it up?' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save hook' }));
    expect(await screen.findByText('Asks: Light it up?')).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Delete Identify' }));
    expect(await screen.findByText('No hooks yet.')).toBeTruthy();
  });

  it("makes a new hook's key from its label, until it is changed (#171)", async () => {
    const { sent } = fakeApi({
      'GET /hooks': () => ({ hooks: [] }),
      'POST /hooks': (b) => ({ status: 201, body: { hook: { ...identify, ...(b as object) } } }),
    });
    render(HooksPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'New hook' }));
    const form = screen.getByRole('form', { name: 'New hook' });
    await fireEvent.input(within(form).getByLabelText('Label'), { target: { value: 'Light its drawer' } });
    expect(within(form).getByText('light_its_drawer')).toBeTruthy();
    // Nothing listens for a new hook yet: nothing to warn of.
    await fireEvent.click(within(form).getByRole('button', { name: 'Change key devices listen for' }));
    expect(within(form).queryByRole('note')).toBeNull();
    await fireEvent.input(within(form).getByLabelText('Key devices listen for'), { target: { value: 'light' } });
    await fireEvent.input(within(form).getByLabelText('Label'), { target: { value: 'Light up its drawer' } });
    await fireEvent.submit(within(form).getByRole('button', { name: 'Save hook' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'POST')).toBe(true));
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ key: 'light', label: 'Light up its drawer', confirm: '' });
  });

  it("keeps a hook's key as its label changes, and warns that devices listen for it", async () => {
    const { sent } = fakeApi({
      'GET /hooks': () => ({ hooks: [identify] }),
      'PUT /hooks/h1': (b) => ({ hook: { ...identify, ...(b as object) } }),
    });
    render(HooksPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'Edit Identify' }));
    const form = screen.getByRole('form', { name: 'Edit Identify' });
    await fireEvent.input(within(form).getByLabelText('Label'), { target: { value: 'Find it' } });
    expect(within(form).getByText('identify')).toBeTruthy();
    expect(within(form).queryByRole('note')).toBeNull();
    await fireEvent.click(within(form).getByRole('button', { name: 'Change key devices listen for' }));
    expect(within(form).getByRole('note').textContent).toBe('Devices, and flows that trigger it, know this hook as identify: change them too, or they stop hearing it.');
    await fireEvent.submit(within(form).getByRole('button', { name: 'Save hook' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ key: 'identify', label: 'Find it', confirm: '' });
  });
});
