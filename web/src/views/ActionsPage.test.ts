import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import ActionsPage from './ActionsPage.svelte';
import { fakeApi } from '../test/fetch';
import { session } from '../lib/session.svelte';
import type { AssetAction } from '../lib/models';

beforeEach(() => {
  session.permissions = ['assets.read', 'categories.manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('ActionsPage', () => {
  it('defines, edits and deletes actions', async () => {
    let actions: AssetAction[] = [];
    const { sent } = fakeApi({
      'GET /actions': () => ({ actions }),
      'POST /actions': (body) => {
        actions = [{ id: 'a1', ...(body as { key: string; label: string; confirm: string }), created_at: '', updated_at: '' }];
        return { status: 201, body: { action: actions[0] } };
      },
      'PUT /actions/a1': (body) => {
        actions = [{ ...actions[0]!, ...(body as object) }];
        return { action: actions[0] };
      },
      'DELETE /actions/a1': () => {
        actions = [];
        return { message: 'Action deleted' };
      },
    });
    render(ActionsPage, { params: {} });
    expect(await screen.findByText('No actions yet.')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'New action' }));
    const form = screen.getByRole('form', { name: 'New action' });
    await fireEvent.input(within(form).getByLabelText('Label'), { target: { value: 'Identify' } });
    await fireEvent.input(within(form).getByLabelText('Key'), { target: { value: ' identify ' } });
    await fireEvent.submit(within(form).getByRole('button', { name: 'Save action' }));
    expect(await screen.findByText('Saved Identify.')).toBeTruthy();
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ key: 'identify', label: 'Identify', confirm: '' });

    await fireEvent.click(await screen.findByRole('button', { name: 'Edit Identify' }));
    await fireEvent.input(screen.getByLabelText('Ask first (optional)'), { target: { value: 'Light it up?' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Save action' }));
    expect(await screen.findByText('Asks: Light it up?')).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Delete Identify' }));
    expect(await screen.findByText('No actions yet.')).toBeTruthy();
  });
});
