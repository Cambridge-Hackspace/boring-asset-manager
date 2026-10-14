import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import NotificationsPage from './NotificationsPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import { outcome } from '../../lib/notifications';
import type { Channel, Delivery, NotifierDriver } from '../../lib/models';

const drivers: NotifierDriver[] = [
  {
    driver: 'discord',
    label: 'Discord',
    options: [{ name: 'webhook_url', label: 'Webhook URL', description: 'From Discord.', required: true, secret: true }],
  },
];
const events = [
  { kind: 'category.running_low', label: 'A category is running low' },
  { kind: 'category.restocked', label: 'A category is restocked' },
];
const channel: Channel = {
  id: 'c1', name: 'Stores', driver: 'discord', options: {}, secrets_set: { webhook_url: true }, enabled: true,
  events: ['category.running_low'], created_at: '', updated_at: '',
};
const delivery = (over: Partial<Delivery>): Delivery => ({
  id: 'd1', event_id: 'e1', channel_id: 'c1', status: 'sent', attempts: 1, next_attempt_at: '2026-10-14T10:00:00Z',
  created_at: '2026-10-14T10:00:00Z', channel_name: 'Stores', event_kind: 'category.running_low', category_name: 'Tape', ...over,
});

beforeEach(() => {
  session.permissions = ['settings:manage'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function api(channels: Channel[], extra: Record<string, (b: unknown) => unknown> = {}) {
  return fakeApi({
    'GET /notifiers/drivers': () => ({ drivers, events }),
    'GET /notifiers': () => ({ channels }),
    'GET /notifiers/deliveries': () => ({
      deliveries: [delivery({}), delivery({ id: 'd2', status: 'pending', attempts: 2, last_error: 'temporary failure: HTTP 502' })],
    }),
    ...extra,
  });
}

describe('NotificationsPage', () => {
  it('adds a channel from the driver description', async () => {
    const { sent } = api([], { 'POST /notifiers': () => ({ status: 201, body: { channel } }) });
    render(NotificationsPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'Add channel' }));
    const form = screen.getByRole('form', { name: 'New channel' });
    await fireEvent.input(within(form).getByLabelText('Name'), { target: { value: 'Stores' } });
    const url = within(form).getByLabelText('Webhook URL') as HTMLInputElement;
    expect(url.type).toBe('password');
    expect(url.required).toBe(true);
    await fireEvent.input(url, { target: { value: ' https://discord.com/api/webhooks/1/x ' } });
    // Every event is on to start with; turn one off.
    await fireEvent.click(within(form).getByLabelText('A category is restocked'));
    await fireEvent.submit(within(form).getByRole('button', { name: 'Add channel' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'POST')).toBe(true));
    expect(sent.find((r) => r.method === 'POST')!.body).toEqual({
      name: 'Stores', driver: 'discord', options: { webhook_url: 'https://discord.com/api/webhooks/1/x' }, enabled: true,
      events: ['category.running_low'],
    });
    expect(await screen.findByText('Channel saved.')).toBeTruthy();
  });

  it('keeps a stored secret when it is left blank', async () => {
    const { sent } = api([channel], { 'PUT /notifiers/c1': () => ({ channel }) });
    render(NotificationsPage, { params: {} });
    await fireEvent.click(await screen.findByRole('button', { name: 'Edit Stores' }));
    const form = screen.getByRole('form', { name: 'Edit Stores' });
    const url = within(form).getByLabelText('Webhook URL') as HTMLInputElement;
    expect(url.value).toBe('');
    expect(url.required).toBe(false);
    expect(url.placeholder).toBe('Set; leave blank to keep it');
    await fireEvent.click(within(form).getByLabelText('Enabled'));
    await fireEvent.submit(within(form).getByRole('button', { name: 'Save channel' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    expect(sent.find((r) => r.method === 'PUT')!.body).toEqual({
      name: 'Stores', driver: 'discord', options: {}, enabled: false, events: ['category.running_low'],
    });
  });

  it('tests a channel and shows the delivery log', async () => {
    api([channel], {
      'POST /notifiers/c1/test': () => ({ status: 502, body: { error: 'The test message was not delivered: HTTP 404: Unknown Webhook' } }),
    });
    render(NotificationsPage, { params: {} });
    expect(await screen.findByText(/Discord, enabled\. A category is running low\./)).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Send a test message to Stores' }));
    expect(await screen.findByText('The test message was not delivered: HTTP 404: Unknown Webhook')).toBeTruthy();

    const log = screen.getByRole('region', { name: 'Recent deliveries' });
    const rows = within(log).getAllByRole('row').slice(1);
    expect(rows[0]!.textContent).toContain('A category is running low: Tape');
    expect(rows[0]!.textContent).toContain('Sent');
    expect(rows[1]!.textContent).toContain('Waiting to retry (2 attempts so far)');
    expect(rows[1]!.textContent).toContain('temporary failure: HTTP 502');
  });
});

describe('outcome', () => {
  it('says what became of a delivery', () => {
    expect(outcome({ status: 'pending', attempts: 0 })).toBe('Waiting to send');
    expect(outcome({ status: 'pending', attempts: 1 })).toBe('Waiting to retry (1 attempt so far)');
    expect(outcome({ status: 'sent', attempts: 1 })).toBe('Sent');
    expect(outcome({ status: 'sent', attempts: 3 })).toBe('Sent after 3 attempts');
    expect(outcome({ status: 'failed', attempts: 1 })).toBe('Failed');
    expect(outcome({ status: 'failed', attempts: 9 })).toBe('Failed after 9 attempts');
  });
});
