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

const identify: AssetHook = { id: 'h1', key: 'identify', label: 'Identify', confirm: '', triggers: [{ type: 'button' }], created_at: '', updated_at: '' };
const button = [{ type: 'button' }];

describe('HooksPage', () => {
  it('defines, edits and deletes hooks', async () => {
    let hooks: AssetHook[] = [];
    const { sent } = fakeApi({
      'GET /hooks': () => ({ hooks }),
      'GET /fields': () => ({ fields: [] }),
      'POST /hooks': (body) => {
        hooks = [{ id: 'a1', ...(body as Pick<AssetHook, 'key' | 'label' | 'confirm' | 'triggers'>), created_at: '', updated_at: '' }];
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
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ key: 'identify', label: 'Identify', confirm: '', triggers: button });

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
      'GET /fields': () => ({ fields: [] }),
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
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({ key: 'light', label: 'Light up its drawer', confirm: '', triggers: button });
  });

  it("keeps a hook's key as its label changes, and warns that devices listen for it", async () => {
    const { sent } = fakeApi({
      'GET /hooks': () => ({ hooks: [identify] }),
      'GET /fields': () => ({ fields: [] }),
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
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({ key: 'identify', label: 'Find it', confirm: '', triggers: button });
  });

  it('fires a hook on events and on a schedule as well as, or instead of, a button', async () => {
    const { sent } = fakeApi({
      'GET /hooks': () => ({ hooks: [] }),
      'GET /fields': () => ({ fields: [{ id: 'f1', key: 'led', label: 'LED', validation_type: 'text' }] }),
      'POST /hooks': (b) => ({ status: 201, body: { hook: { ...identify, ...(b as object) } } }),
    });
    render(HooksPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'New hook' }));
    const form = screen.getByRole('form', { name: 'New hook' });
    await fireEvent.input(within(form).getByLabelText('Label'), { target: { value: 'Light the drawer' } });
    // No button: it fires by itself.
    await fireEvent.click(within(form).getByRole('checkbox', { name: 'A button people press on assets' }));
    await fireEvent.click(within(form).getByRole('button', { name: 'Add events' }));
    await fireEvent.click(within(form).getByRole('checkbox', { name: 'Lent' }));
    await fireEvent.click(within(form).getByRole('checkbox', { name: 'Returned' }));
    await within(form).findByRole('option', { name: 'LED' });
    await fireEvent.change(within(form).getByLabelText('Only when'), { target: { value: 'condition' } });
    await fireEvent.change(within(form).getByLabelText('is'), { target: { value: 'ok' } });
    await fireEvent.input(within(form).getByLabelText('At most once every (minutes)'), { target: { value: '5' } });
    await fireEvent.click(within(form).getByRole('button', { name: 'Add a schedule' }));
    await fireEvent.input(within(form).getByLabelText('When'), { target: { value: '30 8 * * 1-5' } });
    await fireEvent.input(within(form).getByLabelText('Time zone'), { target: { value: 'Europe/London' } });
    await fireEvent.change(within(form).getByLabelText('Loans'), { target: { value: 'overdue' } });
    await fireEvent.submit(within(form).getByRole('button', { name: 'Save hook' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'POST')).toBe(true));
    expect((sent.find((r) => r.method === 'POST')!.body as { triggers: unknown }).triggers).toEqual([
      { type: 'event', kinds: ['asset.checked_out', 'asset.checked_in'], condition: { field: 'condition', equals: 'ok' }, interval_seconds: 300 },
      { type: 'schedule', cron: '30 8 * * 1-5', time_zone: 'Europe/London', filter: { loan: 'overdue' } },
    ]);
  });

  it('will not save a hook nothing fires', async () => {
    const { sent } = fakeApi({ 'GET /hooks': () => ({ hooks: [] }), 'GET /fields': () => ({ fields: [] }) });
    render(HooksPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'New hook' }));
    const form = screen.getByRole('form', { name: 'New hook' });
    await fireEvent.input(within(form).getByLabelText('Label'), { target: { value: 'Nothing' } });
    await fireEvent.click(within(form).getByRole('checkbox', { name: 'A button people press on assets' }));
    await fireEvent.submit(within(form).getByRole('button', { name: 'Save hook' }));
    expect((await screen.findByRole('alert')).textContent).toContain('Choose what fires it');
    expect(sent.some((r) => r.method === 'POST')).toBe(false);
  });
});
