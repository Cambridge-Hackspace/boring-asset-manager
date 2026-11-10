import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import AssistantPanel from './AssistantPanel.svelte';
import { panel, type MessageView } from '../lib/assistant.svelte';

const conversation = { id: 'c1', title: 'A lathe', created_at: '2026-11-09T10:00:00Z', updated_at: '2026-11-09T10:00:00Z' };
const userSaid: MessageView = { id: 'm1', role: 'user', blocks: [{ type: 'text', text: 'Add a lathe' }], context: [{ kind: 'asset', label: 'Band saw' }], created_at: '' };
const modelSaid: MessageView = {
  id: 'm2',
  role: 'assistant',
  blocks: [
    { type: 'text', text: '<b>Adding</b> it.' },
    { type: 'tool_call', id: 't1', name: 'create_asset', title: 'Add assets', input: { assets: [{ name: 'Lathe' }] } },
  ],
  created_at: '',
};

type Sent = { method: string; path: string; body: unknown };

/** Answers the panel's requests; turns are streamed as lines of JSON. */
function fake(turns: Record<string, string>) {
  const sent: Sent[] = [];
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    const path = url.replace(/^\/api\/v1/, '');
    sent.push({ method, path, body: typeof init.body === 'string' ? JSON.parse(init.body) : undefined });
    const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { 'Content-Type': 'application/json' } });
    if (method === 'GET' && path === '/assistant/conversations') return json({ conversations: [conversation] });
    if (method === 'GET' && path === '/assistant/guides') return json({ guides: [] });
    if (method === 'GET' && path === '/assistant') return json({ available: true, name: 'Helper' });
    if (method === 'GET' && path === '/assistant/conversations/c1')
      return json({ conversation, messages: [userSaid, modelSaid], pending: { id: 't1', name: 'create_asset', title: 'Add assets', input: { assets: [{ name: 'Lathe' }] } } });
    const turn = turns[`${method} ${path}`];
    if (turn !== undefined) return turn.startsWith('{"error"') ? new Response(turn, { status: 429 }) : new Response(turn, { status: 200 });
    return json({ error: `no fake for ${method} ${path}` }, 404);
  });
  return sent;
}

beforeEach(() => {
  panel.status = { available: true, name: 'Helper' } as typeof panel.status;
  panel.conversation = 'c1';
  panel.open = true;
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('AssistantPanel', () => {
  it('shows the conversation as text, and what tools it used', async () => {
    fake({});
    render(AssistantPanel);
    expect(await screen.findByText('Add a lathe')).toBeTruthy();
    // Markup the model writes is shown as it is, never rendered.
    expect(screen.getByText('<b>Adding</b> it.')).toBeTruthy();
    expect(screen.getByText('Used Add assets')).toBeTruthy();
    expect(screen.getByText('From Band saw')).toBeTruthy();
  });

  it('confirms the change waiting, and the page reads itself again after it', async () => {
    const before = panel.version;
    const sent = fake({
      'POST /assistant/conversations/c1/confirm':
        '{"type":"message","message":{"id":"m3","role":"tool","blocks":[{"type":"tool_result","id":"t1","content":"Added"}],"created_at":""}}\n' +
        '{"type":"changed","tool":"create_asset"}\n' +
        '{"type":"message","message":{"id":"m4","role":"assistant","blocks":[{"type":"text","text":"Done."}],"created_at":""}}\n' +
        `{"type":"done","conversation":${JSON.stringify(conversation)},"pending":null}\n`,
    });
    render(AssistantPanel);
    expect(await screen.findByText('Confirm: Add assets')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(await screen.findByText('Done.')).toBeTruthy();
    expect(sent.find((s) => s.method === 'POST')?.body).toEqual({ call: 't1', approve: true });
    expect(panel.version).toBe(before + 1);
    expect(screen.queryByText('Confirm: Add assets')).toBeNull();
  });

  it('says when a limit resets', async () => {
    fake({ 'POST /assistant/conversations/c1/messages': '{"error":"You have sent the 2 messages a day each person may send to the assistant. It resets at 00:00 UTC on 10 November.","resets_at":"2026-11-10T00:00:00Z"}' });
    render(AssistantPanel);
    await screen.findByText('Add a lathe');
    await fireEvent.input(screen.getByLabelText('Message to Helper'), { target: { value: 'More' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Send' }));
    const alert = await screen.findByText(/You have sent the 2 messages a day/);
    expect(alert.textContent).toMatch(/It resets .+\./);
    expect(alert.textContent).not.toContain('UTC on 10 November');
  });
});
