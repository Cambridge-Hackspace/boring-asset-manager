import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import ActionButtons from './ActionButtons.svelte';
import { fakeApi } from '../test/fetch';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('ActionButtons', () => {
  const actions = [
    { id: '1', key: 'identify', label: 'Identify' },
    { id: '2', key: 'open-drawer', label: 'Open drawer', confirm: 'Open the drawer?' },
  ];

  it('presses an action, asking first when it has a question', async () => {
    const { sent } = fakeApi({
      'POST /assets/a1/actions/identify': () => ({ status: 201, body: { event_id: 'e1' } }),
      'POST /assets/a1/actions/open-drawer': () => ({ status: 201, body: { event_id: 'e2' } }),
    });
    render(ActionButtons, { assetId: 'a1', actions });
    await fireEvent.click(screen.getByRole('button', { name: 'Identify' }));
    expect(await screen.findByText('Identify: sent.')).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Open drawer' }));
    expect(screen.getByRole('group', { name: 'Open the drawer?' })).toBeTruthy();
    expect(sent.length).toBe(1);
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('group')).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Open drawer' }));
    expect(document.activeElement?.textContent).toBe('Open drawer');
    await fireEvent.click(document.activeElement as HTMLElement);
    expect(await screen.findByText('Open drawer: sent.')).toBeTruthy();
    expect(sent.map((r) => r.path)).toEqual(['/assets/a1/actions/identify', '/assets/a1/actions/open-drawer']);
  });

  it('says why a press was refused', async () => {
    fakeApi({ 'POST /assets/a1/actions/identify': () => ({ status: 429, body: { error: 'That was pressed a moment ago; wait a little before pressing it again' } }) });
    render(ActionButtons, { assetId: 'a1', actions });
    await fireEvent.click(screen.getByRole('button', { name: 'Identify' }));
    expect(await screen.findByText(/pressed a moment ago/)).toBeTruthy();
  });

  it('shows what devices answer, or that none did', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let replies: unknown[] = [];
      fakeApi({
        'POST /assets/a1/actions/identify': () => ({ status: 201, body: { event_id: 'e1' } }),
        'GET /assets/a1/actions/replies': () => ({ replies }),
      });
      render(ActionButtons, { assetId: 'a1', actions });
      await fireEvent.click(screen.getByRole('button', { name: 'Identify' }));
      await screen.findByText('Identify: sent.');
      replies = [
        { id: 'r1', event_id: 'e1', name: 'Cabinet A', token: true, ok: true, message: 'Lit drawer A3', created_at: '' },
        { id: 'r2', event_id: 'e1', name: 'Cabinet B', token: true, ok: false, message: '', created_at: '' },
      ];
      await vi.advanceTimersByTimeAsync(1100);
      expect(await screen.findByText('Cabinet A: Lit drawer A3')).toBeTruthy();
      expect(screen.getByText('Cabinet B: failed')).toBeTruthy();

      replies = [];
      await fireEvent.click(screen.getByRole('button', { name: 'Identify' }));
      await vi.advanceTimersByTimeAsync(9000);
      expect(await screen.findByText('Identify: sent; nothing answered.')).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });
});
