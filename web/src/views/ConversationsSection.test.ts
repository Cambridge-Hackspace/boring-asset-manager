import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import ConversationsSection from './ConversationsSection.svelte';
import { fakeApi } from '../test/fetch';
import { panel } from '../lib/assistant.svelte';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('ConversationsSection', () => {
  it('lists the person’s conversations, opens one in the panel and deletes one', async () => {
    panel.status = { available: true } as typeof panel.status;
    let list = [{ id: 'c1', title: 'Shelves', created_at: '2026-11-09T10:00:00Z', updated_at: '2026-11-09T10:00:00Z' }];
    const { sent } = fakeApi({
      'GET /assistant/conversations': () => ({ conversations: list }),
      'DELETE /assistant/conversations/c1': () => {
        list = [];
        return { message: 'Conversation deleted' };
      },
    });
    render(ConversationsSection);
    expect(await screen.findByText('Shelves')).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Open' }));
    expect(panel.open).toBe(true);
    expect(panel.conversation).toBe('c1');
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    expect(await screen.findByText('Deleted Shelves.')).toBeTruthy();
    expect(sent.some((s) => s.method === 'DELETE')).toBe(true);
    expect(panel.conversation).toBeNull();
  });

  it('shows nothing where the assistant is not in use', async () => {
    panel.status = null;
    fakeApi({ 'GET /assistant/conversations': () => ({ conversations: [] }) });
    render(ConversationsSection);
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByText('Conversations with the assistant')).toBeNull();
  });
});
