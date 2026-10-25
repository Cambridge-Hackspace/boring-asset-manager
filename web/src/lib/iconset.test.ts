import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { iconset } from './iconset.svelte';
import { session } from './session.svelte';
import type { IrDocument, IrIcon } from './isolex/ir';
import type { AssetWithDetails } from './models';
import { fakeApi } from '../test/fetch';
import AssetThumb from '../components/AssetThumb.svelte';
import CategoryBadge from '../components/CategoryBadge.svelte';
import IconPicker from '../components/IconPicker.svelte';

// isolex's own sample (Vitest runs in web/)
const doc = JSON.parse(readFileSync(resolve(process.cwd(), '../internal/isolex/testdata/sample.ir.golden.json'), 'utf8')) as IrDocument;
const unknown: IrIcon = { ...doc.icons[0]!, name: 'bam-unknown' };

let users = 0;
/** Signs someone new in, so the icons are read afresh. */
function signIn() {
  session.user = { id: `u${++users}`, email: 'ada@hackspace.org', name: '', is_active: true, created_at: '', updated_at: '' };
  session.permissions = ['assets.read'];
}

function serveIcons() {
  return fakeApi({
    'GET /icons': () => ({ category_icons: { net: 'switch-l2', routers: 'switch-l2' }, unknown }),
    'GET /icons/ir': () => doc,
    'GET /icons/palette': () => ({ palette: [{ token: 'ink', light: '#111111', dark: '#eeeeee' }] }),
  });
}

beforeEach(() => signIn());
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  session.user = null;
});

describe('iconset', () => {
  it('reads nothing without a session, since the icons are not public', async () => {
    session.user = null;
    const { sent } = serveIcons();
    await iconset.load();
    expect(sent).toEqual([]);
  });

  it('reads the icons once, and draws the unknown icon for a name it does not draw', async () => {
    const { sent } = serveIcons();
    await Promise.all([iconset.load(), iconset.load()]);
    await iconset.load();
    expect(sent.map((r) => r.path)).toEqual(['/icons', '/icons/ir', '/icons/palette']);
    expect(iconset.named('switch-poe-down')?.name).toBe('switch-poe-down');
    expect(iconset.named('gone')?.name).toBe('bam-unknown');
    expect(iconset.forCategory('routers')?.name).toBe('switch-l2');
    expect(iconset.forCategory('chairs')).toBeUndefined();
    expect(iconset.forAsset(null)).toBeUndefined();
    expect(iconset.forAsset({ name: 'switch-l3', from: 'asset' })?.name).toBe('switch-l3');
    expect(iconset.palette).toEqual([{ token: 'ink', light: '#111111', dark: '#eeeeee' }]);
    expect(iconset.all.map((i) => i.name)).toEqual([...doc.icons.map((i) => i.name)].sort());
  });

  it('tries again after a failure', async () => {
    const { sent } = fakeApi({});
    await iconset.load();
    expect(sent.length).toBe(3);
    vi.unstubAllGlobals();
    serveIcons();
    await iconset.load();
    expect(iconset.named('switch-l2')?.name).toBe('switch-l2');
  });
});

const asset = (over: Partial<AssetWithDetails>): Pick<AssetWithDetails, 'asset' | 'photos' | 'resolved_icon'> => ({
  asset: { id: 'a1', name: 'Switch', created_at: '', updated_at: '', condition: 'ok', condition_note: '' },
  photos: [],
  resolved_icon: null,
  ...over,
});

describe('icons on pages', () => {
  it('shows an asset by its photo with its icon in the corner, by its icon alone, or by nothing', async () => {
    serveIcons();
    await iconset.refresh();
    const icon = { name: 'switch-l2', from: 'category' as const, color: '#aa0000' };

    const both = render(AssetThumb, { asset: asset({ photos: [{ id: 'p1', filename: 'front.jpg', content_type: 'image/jpeg', width_px: 1, height_px: 1, byte_size: 1, created_at: '' }], resolved_icon: icon }) });
    expect(both.container.querySelector('img')).not.toBeNull();
    const chip = both.container.querySelector('[data-icon-chip] svg')!;
    expect(chip.getAttribute('data-icon')).toBe('switch-l2');
    expect((chip as SVGElement).style.getPropertyValue('--iso-category').trim()).toBe('#aa0000');
    both.unmount();

    const alone = render(AssetThumb, { asset: asset({ resolved_icon: icon }) });
    expect(alone.container.querySelector('img')).toBeNull();
    expect(alone.container.querySelector('svg')!.getAttribute('data-icon')).toBe('switch-l2');
    alone.unmount();

    const none = render(AssetThumb, { asset: asset({}) });
    expect(none.container.innerHTML.replace(/<!--.*?-->/gu, '')).toBe('');
  });

  it("puts a category's icon, its own or inherited, in its pill, and none for one without", async () => {
    serveIcons();
    await iconset.refresh();
    const { container } = render(CategoryBadge, { category: { id: 'routers', name: 'Routers', color_hex: '#336699' } });
    expect(container.querySelector('svg')!.getAttribute('data-icon')).toBe('switch-l2');
    expect(screen.getByText('Routers')).toBeTruthy();
    cleanup();
    const plain = render(CategoryBadge, { category: { id: 'chairs', name: 'Chairs', color_hex: '#336699' } });
    expect(plain.container.querySelector('svg')).toBeNull();
  });

  it('picks an icon, searched by name or description', async () => {
    serveIcons();
    await iconset.refresh();
    const picked: string[] = [];
    let closed = false;
    render(IconPicker, { value: 'switch-l2', onpick: (n: string) => picked.push(n), onclose: () => (closed = true) });
    expect(screen.getByRole('button', { name: 'switch-l2' }).getAttribute('aria-pressed')).toBe('true');
    const search = screen.getByRole('searchbox', { name: 'Search icons' });
    search.focus();
    await fireEvent.input(search, { target: { value: 'poe-d' } });
    expect(screen.getAllByRole('button', { pressed: false }).map((b) => b.textContent?.trim())).toEqual(['switch-poe-down']);
    screen.getByRole('button', { name: 'switch-poe-down' }).click();
    expect(picked).toEqual(['switch-poe-down']);
    await fireEvent.keyDown(search, { key: 'Escape' });
    expect(closed).toBe(true);
  });
});
