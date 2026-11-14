import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import FlowSearchPage from './FlowSearchPage.svelte';
import { fakeApi } from '../../test/fetch';
import { navigate } from '../../lib/router.svelte';
import { session } from '../../lib/session.svelte';
import type { AssetWithDetails } from '../../lib/models';
import type { FlowSearchView } from '../../lib/flows';

const asset = (id: string, name: string, place = ''): AssetWithDetails => ({
  asset: { id, name, created_at: '', updated_at: '', condition: 'ok', condition_note: '' },
  categories: [{ id: 'shelf', name: 'Shelf', color_hex: '#336699' } as AssetWithDetails['categories'][number]],
  generator_keys: [{ generator_type: 'qr', assigned_code: `CODE-${id}` } as AssetWithDetails['generator_keys'][number]],
  field_values: {},
  effective_fields: [],
  lendable: false,
  weighed: false,
  loan: null,
  links: [],
  location: place ? [{ id: 'room', name: place } as AssetWithDetails['location'][number]] : [],
  suggested_relation_type_ids: [],
  scopes: ['public'],
  scan_template: { template_id: 'scan' },
  label_template: { template_id: '' },
  hooks: [],
  photos: [],
});

const flow = (extra: Partial<FlowSearchView> = {}): FlowSearchView => ({
  name: 'Find it',
  title: 'Find something',
  text: 'Type what you are looking for.',
  columns: ['where', 'categories'],
  reset_after: 0,
  public: true,
  ...extra,
});

/** The search endpoint, answering by what is searched for. */
function searching(found: Record<string, AssetWithDetails[]>, view = flow(), extra: Record<string, () => unknown> = {}) {
  return fakeApi({
    'GET /flows/kiosk/search': () => {
      const search = new URLSearchParams(location.search).get('search') ?? '';
      return { flow: view, assets: found[search] ?? [], ...(search === 'flexible' ? { matched: { t1: 'TPU, narrower than flexible' }, suggestions: ['TPE'] } : {}) };
    },
    'GET /settings/search': () => ({ direct: false }),
    ...extra,
  });
}

beforeEach(() => {
  session.user = null;
  session.permissions = ['public:assets.read'];
  navigate('/f/kiosk', true);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('FlowSearchPage', () => {
  it('lists nothing until searched, then the results as the asset list shows them, opened here', async () => {
    const { sent } = searching({ torch: [asset('a1', 'Torch', 'Room 2'), asset('a2', 'Head torch')] });
    render(FlowSearchPage, { params: { slug: 'kiosk' } });
    expect(await screen.findByRole('heading', { name: 'Find something' })).toBeTruthy();
    expect(screen.getByText('Type what you are looking for.')).toBeTruthy();
    expect(screen.queryByRole('table')).toBeNull();

    await fireEvent.input(screen.getByRole('searchbox', { name: 'Search' }), { target: { value: 'torch' } });
    await fireEvent.submit(screen.getByRole('search', { name: 'Search' }));
    const link = await screen.findByRole('link', { name: 'Torch' });
    expect(location.search).toBe('?search=torch');
    expect(sent.at(-1)?.path).toBe('/flows/kiosk/search');
    // The columns the flow chose, and nothing that leads out of the flow:
    // places are plain text, and each name opens here.
    const headers = screen.getAllByRole('columnheader').map((h) => h.textContent);
    expect(headers).toEqual(['Name', 'Where', 'Categories']);
    expect(screen.getByText('Room 2').closest('a')).toBeNull();
    for (const a of screen.getAllByRole('link')) expect(a.getAttribute('href')).toMatch(/^\/f\/kiosk\?/);
    expect(screen.queryByRole('link', { name: 'Scan page' })).toBeNull();
    expect(screen.queryByRole('checkbox')).toBeNull();
    expect(link.getAttribute('href')).toBe('/f/kiosk?search=torch&open=a1');
  });

  it('opens a result on its scan page’s layout, without its actions, and goes back to the results', async () => {
    searching(
      { torch: [asset('a1', 'Torch', 'Room 2')] },
      flow(),
      {
        'GET /scan/a1': () => ({
          asset: { id: 'a1', name: 'Torch' },
          template: { id: 'scan', name: 'Scan' },
          result: {
            sections: [
              { type: 'details', title: '', columns: ['Name', 'Where'], groups: [{ rows: [{ cells: ['Torch', 'Room 2'] }] }] },
              { type: 'actions', columns: [], actions: ['open', 'borrow'] },
            ],
          },
        }),
      },
    );
    navigate('/f/kiosk?search=torch&open=a1', true);
    render(FlowSearchPage, { params: { slug: 'kiosk' } });
    expect(await screen.findByRole('heading', { name: 'Torch' })).toBeTruthy();
    await screen.findByText('Room 2');
    expect(screen.queryByRole('link', { name: /Open its page/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /Borrow/ })).toBeNull();
    const back = screen.getByRole('link', { name: 'Back to the results' });
    expect(back.getAttribute('href')).toBe('/f/kiosk?search=torch');
    navigate('/f/kiosk?search=torch');
    expect(await screen.findByRole('link', { name: 'Torch' })).toBeTruthy();
  });

  it('shows why a result matched, and offers related terms to search for instead', async () => {
    searching({ flexible: [asset('t1', 'Black TPU')], TPE: [asset('t2', 'Grey TPE')] });
    navigate('/f/kiosk?search=flexible', true);
    render(FlowSearchPage, { params: { slug: 'kiosk' } });
    expect((await screen.findByTestId('match-why')).textContent).toContain('TPU, narrower than flexible');
    await fireEvent.click(within(screen.getByTestId('search-suggestions')).getByRole('button', { name: 'TPE' }));
    expect(await screen.findByRole('link', { name: 'Grey TPE' })).toBeTruthy();
    expect(location.search).toBe('?search=TPE');
  });

  it('underlines a query mistake under the search box', async () => {
    fakeApi({
      'GET /flows/kiosk/search': () => {
        const search = new URLSearchParams(location.search).get('search') ?? '';
        if (!search) return { flow: flow(), assets: [] };
        return { status: 400, body: { error: 'The search can\'t be run', query_error: { message: 'no field has the key "nope"', start: 0, end: 10 } } };
      },
    });
    render(FlowSearchPage, { params: { slug: 'kiosk' } });
    const box = await screen.findByRole('searchbox', { name: 'Search' });
    await fireEvent.input(box, { target: { value: 'field.nope = 1' } });
    await fireEvent.submit(screen.getByRole('search', { name: 'Search' }));
    expect((await screen.findByRole('alert')).textContent).toContain('no field has the key "nope"');
    expect(box.getAttribute('aria-invalid')).toBe('true');
  });

  it('clears itself after the time it is set to, unused, for the next person', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    searching({ torch: [asset('a1', 'Torch')] }, flow({ reset_after: 30 }));
    navigate('/f/kiosk?search=torch', true);
    render(FlowSearchPage, { params: { slug: 'kiosk' } });
    await screen.findByRole('link', { name: 'Torch' });
    // Used, it waits again from then.
    await vi.advanceTimersByTimeAsync(20_000);
    await fireEvent.keyDown(window, { key: 'a' });
    await vi.advanceTimersByTimeAsync(20_000);
    expect(location.search).toBe('?search=torch');
    await vi.advanceTimersByTimeAsync(11_000);
    await vi.waitFor(() => expect(location.search).toBe(''));
    await vi.waitFor(() => expect(screen.queryByRole('link', { name: 'Torch' })).toBeNull());
    expect((screen.getByRole('searchbox', { name: 'Search' }) as HTMLInputElement).value).toBe('');
  });

  it('asks to sign in where the flow is not open to signed-out visitors', async () => {
    fakeApi({ 'GET /flows/kiosk/search': () => ({ status: 401, body: { error: 'Authentication required' } }) });
    render(FlowSearchPage, { params: { slug: 'kiosk' } });
    expect(await screen.findByRole('heading', { name: 'Sign in to search here' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Sign in' }).getAttribute('href')).toContain('/sign-in');
  });

  it('goes straight to a search’s one result, where that is chosen', async () => {
    searching({ torch: [asset('a1', 'Torch')] }, flow(), { 'GET /settings/search': () => ({ direct: true }), 'GET /scan/a1': () => ({ status: 404, body: { error: 'No asset has that code' } }) });
    render(FlowSearchPage, { params: { slug: 'kiosk' } });
    await fireEvent.input(await screen.findByRole('searchbox', { name: 'Search' }), { target: { value: 'torch' } });
    await fireEvent.submit(screen.getByRole('search', { name: 'Search' }));
    await vi.waitFor(() => expect(location.search).toBe('?search=torch&open=a1'));
    expect(await screen.findByRole('heading', { name: 'Torch' })).toBeTruthy();
    // Without a scan page to see, it shows as its result does.
    expect(await screen.findByText('CODE-a1')).toBeTruthy();
  });
});
