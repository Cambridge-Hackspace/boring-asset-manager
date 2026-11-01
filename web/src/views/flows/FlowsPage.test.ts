import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/svelte';
import FlowsPage from './FlowsPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Flow } from '../../lib/flows';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const flow = (slug: string, name: string, enabled = true): Flow => ({
  id: slug,
  slug,
  name,
  enabled,
  created_at: '',
  updated_at: '',
  definition: { version: 2, category_ids: [], description: `About ${name}.`, steps: [{ id: 'done', kind: 'done', title: 'Done' }] },
});

function api() {
  fakeApi({ 'GET /flows': () => ({ flows: [flow('leave', 'Leave something'), flow('old', 'Old flow', false)] }) });
}

describe('FlowsPage', () => {
  it('shows each flow a member may run as a card that runs it', async () => {
    session.permissions = ['flows.run'];
    api();
    render(FlowsPage, { params: {} });
    const list = await screen.findByRole('list', { name: 'Flows to run' });
    const cards = within(list).getAllByRole('listitem');
    expect(cards).toHaveLength(1);
    expect(within(cards[0]!).getByRole('heading', { name: 'Leave something' })).toBeTruthy();
    expect(cards[0]!.textContent).toContain('About Leave something.');
    expect(within(cards[0]!).getByRole('link', { name: 'Run Leave something' }).getAttribute('href')).toBe('/f/leave');
    expect(within(cards[0]!).queryByRole('link', { name: /^Edit/ })).toBeNull();
  });

  it('shows a manager every flow, those turned off too, to change', async () => {
    session.permissions = ['flows.run', 'flows.manage'];
    api();
    render(FlowsPage, { params: {} });
    const list = await screen.findByRole('list', { name: 'Every flow' });
    const [on, off] = within(list).getAllByRole('listitem');
    expect(within(on!).getByRole('link', { name: 'Run Leave something' })).toBeTruthy();
    expect(within(on!).getByRole('link', { name: 'Edit Leave something' }).getAttribute('href')).toBe('/flows/leave/edit');
    expect(off!.textContent).toContain('Turned off');
    expect(within(off!).queryByRole('link', { name: 'Run Old flow' })).toBeNull();
    expect(within(off!).getByRole('button', { name: 'Turn Old flow on' })).toBeTruthy();
    // The one primary action: a new flow.
    expect(screen.getByRole('link', { name: 'New flow' }).className).toContain('btn-primary');
  });
});
