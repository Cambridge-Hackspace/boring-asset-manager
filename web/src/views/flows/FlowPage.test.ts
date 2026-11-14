import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/svelte';
import FlowPage from './FlowPage.svelte';
import { fakeApi } from '../../test/fetch';
import { navigate } from '../../lib/router.svelte';
import { session } from '../../lib/session.svelte';
import type { Flow } from '../../lib/flows';

const user = { id: 'sam', email: 'sam@hackspace.org', name: 'Sam', is_active: true, created_at: '', updated_at: '' };
const flowOf = (steps: Flow['definition']['steps']): Flow => ({ id: 'f', slug: 'kiosk', name: 'Find it', enabled: true, created_at: '', updated_at: '', definition: { version: 2, category_ids: ['shelf'], steps } });
const searchView = { flow: { name: 'Find it', title: 'Find something', columns: ['where'], reset_after: 0, public: true }, assets: [] };

afterEach(() => {
  cleanup();
  session.user = null;
  vi.unstubAllGlobals();
});

describe('FlowPage', () => {
  it('signed out, opens the search page without asking for the flow', async () => {
    session.user = null;
    session.permissions = ['public:assets.read'];
    navigate('/f/kiosk', true);
    const { sent } = fakeApi({ 'GET /flows/kiosk/search': () => searchView });
    render(FlowPage, { params: { slug: 'kiosk' } });
    expect(await screen.findByRole('heading', { name: 'Find something' })).toBeTruthy();
    expect(sent.some((r) => r.path === '/flows/kiosk')).toBe(false);
  });

  it('signed in, opens a flow that only searches as its search page, and any other as a run', async () => {
    session.user = user;
    session.permissions = ['flows.run', 'assets.read'];
    navigate('/f/kiosk', true);
    fakeApi({ 'GET /flows/kiosk': () => ({ flow: flowOf([{ id: 'search', kind: 'search' }]) }), 'GET /flows/kiosk/search': () => searchView });
    render(FlowPage, { params: { slug: 'kiosk' } });
    expect(await screen.findByRole('heading', { name: 'Find something' })).toBeTruthy();
    cleanup();

    const { sent } = fakeApi({
      'GET /flows/kiosk': () => ({ flow: flowOf([{ id: 'done', kind: 'done' }]) }),
      'POST /flows/kiosk/runs': () => ({ status: 201, body: { run: { id: 'r1', flow_id: 'f', step: 0, values: { flow: 'Find it' }, done: [], finished: true }, flow: 'kiosk', prompt: { step: 'done', kind: 'done', title: 'Done' } } }),
    });
    render(FlowPage, { params: { slug: 'kiosk' } });
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'POST' && r.path === '/flows/kiosk/runs')).toBe(true));
  });
});
