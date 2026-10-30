import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import PackagesPage from './PackagesPage.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { Plan, Selection } from '../../lib/packages';

beforeEach(() => {
  session.permissions = ['assets.read', 'flows.run'];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

// A plan as the server works it out: Tools needs Serial and the Lend flow,
// which needs a printer that stays behind.
function planOf(sel: Selection): Plan {
  const tools = (sel.categories ?? []).includes('t');
  const items: Plan['items'] = (sel.categories ?? []).map((id) => ({ kind: 'category', id, name: id === 't' ? 'Tools' : 'Drills', chosen: true, needed_by: [] }));
  for (const id of sel.fields ?? []) items.push({ kind: 'field', id, name: 'Serial', chosen: true, needed_by: [] });
  if (tools) {
    if (!(sel.fields ?? []).includes('f')) items.push({ kind: 'field', id: 'f', name: 'Serial', chosen: false, needed_by: [{ kind: 'category', id: 't', name: 'Tools' }] });
    items.push({ kind: 'flow', id: 'l', name: 'Lend a tool', chosen: false, needed_by: [{ kind: 'category', id: 't', name: 'Tools' }] });
  }
  return {
    items,
    assets: sel.assets ? { assets: 2, photos: 1, links: 0, events: 0, unreadable: 1 } : undefined,
    left_out: tools
      ? [{ kind: 'printer', id: 'p', name: 'Front desk', needed_by: [{ kind: 'flow', id: 'l', name: 'Lend a tool' }], reason: 'printers are this instance’s own' }]
      : [],
  };
}

function server() {
  return fakeApi({
    'GET /categories': () => ({ categories: [{ id: 't', name: 'Tools' }, { id: 'd', name: 'Drills', parent_id: 't' }] }),
    'GET /fields': () => ({ fields: [{ id: 'f', key: 'serial', label: 'Serial' }, { id: 'g', key: 'bought', label: 'Bought' }] }),
    'GET /actions': () => ({ actions: [] }),
    'GET /relation-types': () => ({ relation_types: [] }),
    'GET /templates': () => ({ templates: [] }),
    'GET /reports': () => ({ reports: [] }),
    'GET /flows': () => ({ flows: [{ id: 'l', slug: 'lend', name: 'Lend a tool' }] }),
    'POST /exports/plan': (body) => planOf((body as { selection: Selection }).selection),
    'POST /exports': (body) => ({ status: 201, body: { id: 'e1', download: '/api/v1/exports/e1', plan: planOf((body as { selection: Selection }).selection) } }),
  });
}

describe('PackagesPage', () => {
  it('ticks a category with those under it, and marks what it needs', async () => {
    server();
    render(PackagesPage, { params: {} });
    const tree = await screen.findByTestId('category-tree');
    const tools = within(tree).getByRole('checkbox', { name: 'Tools' }) as HTMLInputElement;
    await fireEvent.click(tools);
    expect((within(tree).getByRole('checkbox', { name: 'Drills' }) as HTMLInputElement).checked).toBe(true);

    // Serial is needed: ticked, locked, and saying by what
    const fields = screen.getByTestId('group-field');
    await waitFor(() => expect(within(fields).getByText('Needed by Tools')).toBeTruthy());
    const serial = within(fields).getByRole('checkbox', { name: 'Serial' }) as HTMLInputElement;
    expect(serial.checked && serial.disabled).toBe(true);
    expect(serial.getAttribute('aria-describedby')).toBe('needed-field-f');
    const bought = within(fields).getByRole('checkbox', { name: 'Bought' }) as HTMLInputElement;
    expect(bought.checked || bought.disabled).toBe(false);
    expect(within(screen.getByTestId('group-flow')).getByText('Needed by Tools')).toBeTruthy();
    expect(within(screen.getByTestId('left-out')).getByText(/printer Front desk, named by Lend a tool/)).toBeTruthy();

    // unticking Drills leaves Tools neither ticked nor not
    await fireEvent.click(within(tree).getByRole('checkbox', { name: 'Drills' }));
    expect(tools.indeterminate).toBe(true);
    // clicking it ticks everything again, as an installer does; clicking
    // again unticks it all, freeing Serial
    await fireEvent.click(tools);
    expect(tools.checked && !tools.indeterminate).toBe(true);
    await fireEvent.click(tools);
    await waitFor(() => expect(within(fields).queryByText('Needed by Tools')).toBeNull());
    expect(serial.disabled).toBe(false);
  });

  it('chooses a group whole, and its box shows some when part is', async () => {
    server();
    render(PackagesPage, { params: {} });
    const fields = await screen.findByTestId('group-field');
    const all = within(fields).getByRole('checkbox', { name: 'Fields' }) as HTMLInputElement;
    await fireEvent.click(all);
    expect((within(fields).getByRole('checkbox', { name: 'Bought' }) as HTMLInputElement).checked).toBe(true);
    await fireEvent.click(within(fields).getByRole('checkbox', { name: 'Bought' }));
    expect(all.indeterminate).toBe(true);
  });

  it('shows no flows or scopes to those who cannot see them', async () => {
    session.permissions = ['assets.read'];
    const { sent } = server();
    render(PackagesPage, { params: {} });
    await screen.findByTestId('group-field');
    expect(screen.queryByTestId('group-flow')).toBeNull();
    expect(sent.some((r) => r.path === '/flows' || r.path === '/scopes')).toBe(false);
  });

  it('exports what is chosen once it has a name, and the browser downloads it', async () => {
    const { sent } = server();
    const clicked: string[] = [];
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clicked.push(this.getAttribute('href') ?? '');
    });
    render(PackagesPage, { params: {} });
    await fireEvent.click(within(await screen.findByTestId('category-tree')).getByRole('checkbox', { name: 'Tools' }));
    await fireEvent.click(screen.getByRole('radio', { name: 'Those in the categories chosen' }));
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Photos' }));
    const button = screen.getByTestId('export') as HTMLButtonElement;
    await waitFor(() => expect(screen.getByTestId('plan').querySelector('p')?.textContent).toBe('The package holds 4 things and 2 assets with 1 photo. 1 asset you cannot see is left out.'));
    expect(button.disabled).toBe(true);
    await fireEvent.input(screen.getByRole('textbox', { name: 'Name' }), { target: { value: 'Workshop' } });
    await fireEvent.input(screen.getByRole('textbox', { name: 'License' }), { target: { value: 'CC0-1.0' } });
    await waitFor(() => expect(button.disabled).toBe(false));
    await fireEvent.click(button);
    await waitFor(() => expect(clicked).toEqual(['/api/v1/exports/e1']));
    const posted = sent.find((r) => r.method === 'POST' && r.path === '/exports')!.body as Selection & { selection: Selection; name: string; license: string };
    expect(posted.name).toBe('Workshop');
    expect(posted.license).toBe('CC0-1.0');
    expect([...(posted.selection.categories ?? [])].sort()).toEqual(['d', 't']);
    expect(posted.selection.assets).toEqual({ all: false, photos: true, links: false, history: false });
    expect(screen.getByText('Your browser is saving Workshop.')).toBeTruthy();
    click.mockRestore();
  });
});
