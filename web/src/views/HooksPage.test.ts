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
    await fireEvent.input(within(form).getByLabelText('Key'), { target: { value: ' identify ' } });
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
});
