import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import IncomingHooksPage from './IncomingHooksPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { IncomingHook } from '../../lib/models';

const roles = [{ id: 'counters', name: 'Counters', description: '', permissions: ['stock.record'], user_count: 0, protected: false }];
const hooks = [{ id: 'h1', key: 'identify', label: 'Identify', confirm: '' }];
const scale: IncomingHook = {
  id: 'abc',
  name: 'Shelf scale',
  enabled: true,
  auth: 'bearer',
  has_secret: true,
  action: 'quantity',
  asset: { by: 'code', path: 'code' },
  rate_limit: 60,
  roles: [{ role_id: 'counters', scope_ids: [] }],
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  last_used_at: null,
  url: 'https://bam.example.org/api/v1/incoming/abc',
};

beforeEach(() => {
  session.permissions = ['settings.manage', 'users.manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('IncomingHooksPage', () => {
  it('makes an incoming hook, and shows its secret once', async () => {
    let incoming: IncomingHook[] = [];
    const { sent } = fakeApi({
      'GET /incoming': () => ({ incoming_hooks: incoming }),
      'GET /roles': () => ({ roles }),
      'GET /hooks': () => ({ hooks }),
      'POST /incoming': (body) => {
        const b = body as Partial<IncomingHook>;
        const h: IncomingHook = { ...scale, id: 'new', name: b.name!, action: b.action!, hook: b.hook, asset: b.asset!, url: 'https://bam.example.org/api/v1/incoming/new' };
        incoming = [h];
        return { status: 201, body: { incoming_hook: h, secret: 'bamin_secret' } };
      },
    });
    render(IncomingHooksPage, { params: {} });
    await screen.findByText('No incoming hooks yet.');

    await fireEvent.click(screen.getByRole('button', { name: 'New incoming hook' }));
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Drawer button' } });
    await fireEvent.change(screen.getByLabelText('How the asset is found'), { target: { value: 'fixed' } });
    await fireEvent.input(screen.getByLabelText('Asset ID'), { target: { value: 'a1' } });
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Counters' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Create incoming hook' }));

    expect(((await screen.findByLabelText('The new secret')) as HTMLInputElement).value).toBe('bamin_secret');
    expect(screen.getByText('https://bam.example.org/api/v1/incoming/new', { selector: 'section code' })).toBeTruthy();
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({
      name: 'Drawer button',
      enabled: true,
      auth: 'bearer',
      action: 'hook',
      hook: 'identify',
      flow_id: '',
      fields: {},
      asset: { by: 'fixed', asset_id: 'a1' },
      rate_limit: 60,
      roles: [{ role_id: 'counters', scope_ids: [] }],
      new_secret: false,
    });
    const row = (await screen.findByText('Drawer button', { selector: 'td span' })).closest('tr')!;
    expect(row.textContent).toContain('Trigger Identify');
    expect(row.textContent).toContain('One asset');
    await fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    expect(screen.queryByLabelText('The new secret')).toBeNull();
  });

  it('maps fields from the request, gives a new secret, and deletes', async () => {
    let incoming: IncomingHook[] = [scale, { ...scale, id: 'old', name: 'Restored', has_secret: false, enabled: false }];
    const { sent } = fakeApi({
      'GET /incoming': () => ({ incoming_hooks: incoming }),
      'GET /roles': () => ({ roles }),
      'GET /fields': () => ({ fields: [{ id: 'f1', key: 'weight', label: 'Weight', validation_type: 'number', created_at: '' }] }),
      'PUT /incoming/abc': (body) => {
        const b = body as Partial<IncomingHook> & { new_secret: boolean };
        const h = { ...scale, ...b } as IncomingHook;
        incoming = [h, incoming[1]!];
        return b.new_secret ? { incoming_hook: h, secret: 'bamin_next' } : { incoming_hook: h };
      },
      'DELETE /incoming/old': () => {
        incoming = incoming.slice(0, 1);
        return { message: 'Incoming hook deleted' };
      },
    });
    render(IncomingHooksPage, { params: {} });
    const row = (await screen.findByText('Shelf scale', { selector: 'td span' })).closest('tr')!;
    expect(row.textContent).toContain('By code at code');
    const restored = screen.getByText('Restored', { selector: 'td span' }).closest('tr')!;
    expect(restored.textContent).toContain('No secret: give it a new one');
    expect(restored.textContent).toContain('Off');

    // Set fields, one mapped from the request.
    await fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    await fireEvent.change(screen.getByLabelText('What it does'), { target: { value: 'fields' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add a field' }));
    await fireEvent.input(screen.getByLabelText('Path for field 1'), { target: { value: 'data.grams' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByText('Saved Shelf scale.');
    const put = sent.find((r) => r.method === 'PUT')!.body as Record<string, unknown>;
    expect(put.action).toBe('fields');
    expect(put.fields).toEqual({ weight: 'data.grams' });
    expect(put.new_secret).toBe(false);

    // A new secret, asked for again first.
    const again = (await screen.findByText('Shelf scale', { selector: 'td span' })).closest('tr')!;
    await fireEvent.click(within(again).getByRole('button', { name: 'New secret' }));
    await fireEvent.click(within(within(again).getByRole('group')).getByRole('button', { name: 'New secret' }));
    expect(((await screen.findByLabelText('The new secret')) as HTMLInputElement).value).toBe('bamin_next');
    expect((sent.filter((r) => r.method === 'PUT').at(-1)!.body as { new_secret: boolean }).new_secret).toBe(true);

    const gone = screen.getByText('Restored', { selector: 'td span' }).closest('tr')!;
    await fireEvent.click(within(gone).getByRole('button', { name: 'Delete' }));
    await fireEvent.click(within(within(gone).getByRole('group')).getByRole('button', { name: 'Delete' }));
    await screen.findByText('Deleted Restored.');
    await waitFor(() => expect(screen.queryByText('Restored', { selector: 'td span' })).toBeNull());
  });
});
