import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import TemplateEditor from './TemplateEditor.svelte';
import { fakeApi } from '../../test/fetch';
import { session } from '../../lib/session.svelte';
import type { TemplateDoc } from '../../lib/template';

const template: TemplateDoc = {
  id: 'ql90',
  name: 'QL 90x29',
  units: 'mm',
  width: 90,
  height: 29,
  dpi: 300,
  elements: [{ type: 'text', x: 10, y: 5, width: 40, height: 10, content_binding: '{{asset.name}}', font_size: 12 }],
};

function api(extra: Record<string, (b: unknown) => unknown> = {}) {
  return fakeApi({
    'GET /generators': () => ({ generators: [{ type: 'qr', name: 'QR Code', error_correction_levels: ['M', 'L', 'Q', 'H'], code_rules: '' }] }),
    'GET /fields': () => ({ fields: [] }),
    'GET /templates/ql90': () => ({ template }),
    'GET /printers': () => ({ printers: [] }),
    'GET /printers/drivers': () => ({ drivers: [] }),
    'POST /templates/preview': () => '<svg/>',
    ...extra,
  });
}

const value = (selector: string) => (document.querySelector(selector) as HTMLInputElement).value;
const loaded = () => screen.findByDisplayValue('QL 90x29');
const canvasElement = (name: RegExp | string) => screen.getAllByRole('button', { name }).find((b) => b.hasAttribute('data-element'))!;
const layerNames = () =>
  within(screen.getByRole('list', { name: 'Layers' }))
    .getAllByRole('button')
    .filter((b) => !b.getAttribute('aria-label')?.startsWith('Lock'))
    .map((b) => b.textContent?.trim());

async function saved(sent: { method: string; body: unknown }[]): Promise<TemplateDoc> {
  await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
  return sent.find((r) => r.method === 'PUT')!.body as TemplateDoc;
}

beforeEach(() => {
  session.permissions = ['templates.manage'];
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: () => 'blob:preview', revokeObjectURL: () => {} }));
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('TemplateEditor', () => {
  it('lays out a toolbox, the tool options, the label, panels and a status bar', async () => {
    api();
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    expect(screen.getByRole('heading', { level: 1, name: 'QL 90x29' })).toBeTruthy();
    const tools = screen.getByRole('toolbar', { name: 'Tools' });
    expect(within(tools).getAllByRole('button').map((b) => b.getAttribute('aria-label'))).toEqual([
      'Select and move', 'Text', 'Barcode or QR code', 'Image', 'Rectangle', 'Ellipse', 'Line', 'Polygon',
    ]);
    expect(screen.getByRole('group', { name: 'Tool options' })).toBeTruthy();
    for (const panel of ['Properties', 'Layers', 'Arrange', 'Label', 'Printer', 'Preview data']) {
      expect(screen.getByRole('button', { name: panel, expanded: true })).toBeTruthy();
    }
    // jsdom runs no workers: the server draws the label, and the status bar says so.
    expect(screen.getByText('Live preview unavailable')).toBeTruthy();
    expect(screen.getByText('Click an element to select it. Shift+click: select several.')).toBeTruthy();
  });

  it('previews through the server, and moves the selection with the arrow keys', async () => {
    const { sent } = api();
    render(TemplateEditor, { params: { id: 'ql90' } });
    await vi.waitFor(() => expect(sent.some((r) => r.path === '/templates/preview')).toBe(true));
    expect((sent.find((r) => r.path === '/templates/preview')!.body as { template: TemplateDoc }).template.elements).toHaveLength(1);

    // Focused, an element is selected, and its numbers are in the tool options.
    const box = await screen.findByRole('button', { name: 'Text 1: {{asset.name}}', pressed: false });
    await fireEvent.focus(box);
    expect(box.getAttribute('aria-pressed')).toBe('true');
    await fireEvent.keyDown(box, { key: 'ArrowRight' });
    await fireEvent.keyDown(box, { key: 'ArrowDown', shiftKey: true });
    await fireEvent.keyDown(box, { key: 'ArrowRight', altKey: true });
    expect(value('#el-0-x')).toBe('10.55');
    expect(value('#el-0-y')).toBe('10');
    expect(screen.getByText('Unsaved changes')).toBeTruthy();

    await fireEvent.keyDown(window, { key: 'z', ctrlKey: true });
    await fireEvent.keyDown(window, { key: 'z', ctrlKey: true });
    expect(value('#el-0-y')).toBe('5');
    await fireEvent.keyDown(window, { key: 'Z', ctrlKey: true, shiftKey: true });
    expect(value('#el-0-y')).toBe('10');
  });

  it('keeps the newest preview when an earlier one answers after it', async () => {
    // Previews are held until released, the first one asked last.
    const held: { left: number; release: () => void }[] = [];
    let hold = false;
    const { sent } = api({
      'POST /templates/preview': (b) => {
        if (!hold) return '<svg/>';
        return new Promise((resolve) => held.push({ left: (b as { template: TemplateDoc }).template.elements[0]!.x, release: () => resolve('<svg/>') }));
      },
    });
    let made = 0;
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: () => `blob:preview-${++made}`, revokeObjectURL: () => {} }));
    render(TemplateEditor, { params: { id: 'ql90' } });
    const image = () => screen.getByAltText('Label preview').getAttribute('src');
    await vi.waitFor(() => expect(image()).toBe('blob:preview-1'));

    hold = true;
    const box = await screen.findByRole('button', { name: 'Text 1: {{asset.name}}', pressed: false });
    await fireEvent.focus(box);
    await fireEvent.keyDown(box, { key: 'ArrowRight' });
    await vi.waitFor(() => expect(held).toHaveLength(1));
    await fireEvent.keyDown(box, { key: 'ArrowRight' });
    await vi.waitFor(() => expect(held).toHaveLength(2));
    expect(held.map((h) => h.left)).toEqual([10.5, 11]);

    // The second answers first, and is shown; the first, late, is dropped.
    held[1]!.release();
    await vi.waitFor(() => expect(image()).toBe('blob:preview-2'));
    held[0]!.release();
    await new Promise((r) => setTimeout(r, 50));
    expect(image()).toBe('blob:preview-2');
    expect(sent.filter((r) => r.path === '/templates/preview')).toHaveLength(3);
  });

  it('selects all with Ctrl+A, none with Ctrl+Shift+A or Escape, and copies and pastes', async () => {
    const two = { ...template, elements: [...template.elements, { type: 'text' as const, x: 60, y: 5, width: 20, height: 10, content_binding: 'B', font_size: 12 }] };
    const { sent } = api({ 'GET /templates/ql90': () => ({ template: two }), 'PUT /templates/ql90': () => ({ template: two }) });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.keyDown(window, { key: 'a', ctrlKey: true });
    expect(screen.getByText('2 elements')).toBeTruthy();
    await fireEvent.keyDown(window, { key: 'A', ctrlKey: true, shiftKey: true });
    expect(screen.queryByText('2 elements')).toBeNull();
    await fireEvent.keyDown(window, { key: 'a', ctrlKey: true });
    await fireEvent.keyDown(window, { key: 'Escape' });
    expect(screen.getByText(/Nothing selected/)).toBeTruthy();

    // Grouped, copied and pasted: a little way off, in a group of its own.
    await fireEvent.keyDown(window, { key: 'a', ctrlKey: true });
    await fireEvent.click(screen.getByRole('button', { name: 'Group' }));
    expect(screen.getByText('Group group-1')).toBeTruthy();
    await fireEvent.click(within(screen.getByRole('group', { name: 'Line up' })).getByRole('button', { name: 'Tops' }));
    const data = new Map<string, string>();
    const clipboardData = { setData: (k: string, v: string) => data.set(k, v), getData: (k: string) => data.get(k) ?? '' };
    window.dispatchEvent(Object.assign(new Event('copy', { cancelable: true }), { clipboardData }));
    window.dispatchEvent(Object.assign(new Event('paste', { cancelable: true }), { clipboardData }));
    await vi.waitFor(() => expect(screen.getByText('Group group-2')).toBeTruthy());

    const body = await saved(sent);
    expect(body.elements.map((e) => e.group)).toEqual(['group-1', 'group-1', 'group-2', 'group-2']);
    expect(body.elements.map((e) => e.y)).toEqual([5, 5, 7, 7]);
  });

  it('hides everything but the label with Tab, and shows it again', async () => {
    api();
    const { container } = render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    (document.activeElement as HTMLElement | null)?.blur();
    await fireEvent.keyDown(window, { key: 'Tab' });
    expect(screen.getByRole('button', { name: /Canvas only. Press Tab/ })).toBeTruthy();
    expect((container.querySelector('aside[aria-label="Panels"]') as HTMLElement).hidden).toBe(true);
    expect((container.querySelector('aside[aria-label="Toolbox"]') as HTMLElement).hidden).toBe(true);
    // What was in the panels is kept, not made again.
    expect((document.querySelector('#tpl-name') as HTMLInputElement).value).toBe('QL 90x29');
    await fireEvent.keyDown(window, { key: 'Tab' });
    expect((container.querySelector('aside[aria-label="Panels"]') as HTMLElement).hidden).toBe(false);
    // Focused on a field, Tab moves on, as it always does.
    (document.querySelector('#tpl-name') as HTMLElement).focus();
    await fireEvent.keyDown(document.querySelector('#tpl-name')!, { key: 'Tab' });
    expect((container.querySelector('aside[aria-label="Panels"]') as HTMLElement).hidden).toBe(false);
  });

  it('collapses panels and moves them, as remembered in this browser', async () => {
    api();
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Arrange', expanded: true }));
    expect(screen.getByRole('button', { name: 'Arrange', expanded: false })).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Move Label up' }));
    const titles = () => screen.getAllByRole('button', { expanded: true }).concat(screen.getAllByRole('button', { expanded: false })).map((b) => b.id).filter((id) => id.startsWith('panel-'));
    expect(titles()).toContain('panel-label');
    expect(JSON.parse(localStorage.getItem('bam.editor.panels')!)).toEqual({
      order: ['properties', 'layers', 'label', 'arrange', 'printer', 'preview'],
      collapsed: ['arrange'],
    });
    cleanup();
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    expect(screen.getByRole('button', { name: 'Arrange', expanded: false })).toBeTruthy();
    const order = [...document.querySelectorAll('[id^="panel-"]')].map((b) => b.id);
    expect(order).toEqual(['panel-properties', 'panel-layers', 'panel-label', 'panel-arrange', 'panel-printer', 'panel-preview']);
  });

  const printers = () => ({
    printers: [
      { id: 'p0', name: 'Rollo', driver: 'tspl', model: 'Rollo X1038', media: '4x6', connection: { type: 'tcp', address: 'rollo' }, created_at: '', updated_at: '' },
      { id: 'p1', name: 'QL', driver: 'brother_ql', model: 'QL-810W', media: 'dk-11201', connection: { type: 'tcp', address: 'ql' }, created_at: '', updated_at: '',
        loaded: { id: 'dk-11201', name: 'DK-11201 29 × 90 mm die-cut', width_mm: 29, length_mm: 90, die_cut: true } },
    ],
  });

  it('checks the fit on the first printer that takes it, and shades what it does not print', async () => {
    const { sent } = api({
      'GET /printers': printers,
      'POST /printers/p0/fit': () => ({ fits: false, message: "This template can't print on Rollo: it is laid out at 300 DPI, but the Rollo X1038 prints at 203 DPI." }),
      'POST /printers/p1/fit': () => ({
        fits: true, warnings: ['Part of the template is where the printer does not print.'],
        printable: { x: 3.05, y: 1.55, width: 83.9, height: 25.9 }, edge_mm: 1, outside: [0], near_edge: [],
      }),
    });
    const { container } = render(TemplateEditor, { params: { id: 'ql90' } });
    // The Rollo does not take it, so the QL is chosen.
    expect(await screen.findByText('Fits QL, with 1 warning.', {}, { timeout: 2000 })).toBeTruthy();
    expect(value('#tpl-printer')).toBe('p1');
    expect(screen.getByText('In the shaded area, which it does not print: Text 1: {{asset.name}}.')).toBeTruthy();
    expect(screen.getByText('Loaded: DK-11201 29 × 90 mm die-cut.')).toBeTruthy();
    // Four strips around what it prints, and the element reaching into them outlined.
    expect(container.querySelectorAll('.unprinted')).toHaveLength(4);
    expect(canvasElement(/^Text 1/).classList.contains('warned')).toBe(true);
    const fit = sent.find((r) => r.path === '/printers/p1/fit')!;
    expect(fit.body).toEqual({ width_mm: 90, height_mm: 29, dpi: 300, content: [{ x: 10, y: 5, width: 40, height: 10 }] });

    // The shading can be put away, and that is remembered.
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Shade what it does not print' }));
    await vi.waitFor(() => expect(container.querySelectorAll('.unprinted')).toHaveLength(0));
    expect(localStorage.getItem('bam.editor-printer-limits')).toBe('off');
    // Choosing None turns the check off, and is remembered.
    await fireEvent.change(document.querySelector('#tpl-printer')!, { target: { value: '' } });
    expect(localStorage.getItem('bam.editor-printer.ql90')).toBe('');
  });

  it('keeps a printer chosen by hand while it is still choosing one, and drops late answers', async () => {
    let release: () => void = () => {};
    const slow = new Promise<void>((r) => (release = r));
    let rolloChecks = 0;
    const { sent } = api({
      'GET /printers': printers,
      'POST /printers/p0/fit': async () => {
        if (++rolloChecks === 1) await slow;
        return { fits: true, warnings: [], printable: { x: 0, y: 0, width: 90, height: 29 }, edge_mm: 1, outside: [], near_edge: [] };
      },
      'POST /printers/p1/fit': () => ({ fits: false, message: "This template can't print on QL: wrong size." }),
    });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await vi.waitFor(() => expect(document.querySelector('#tpl-printer')).not.toBeNull());
    const select = document.querySelector('#tpl-printer') as HTMLSelectElement;
    await vi.waitFor(() => expect(sent.some((r) => r.path === '/printers/p0/fit')).toBe(true));
    await fireEvent.change(select, { target: { value: 'p1' } });
    expect(await screen.findByText(/can't print on QL/, {}, { timeout: 2000 })).toBeTruthy();
    release();
    await new Promise((r) => setTimeout(r, 400));
    expect(select.value).toBe('p1');
    expect(screen.queryByText('Fits Rollo.')).toBeNull();
  });

  it('test-prints the template as it is, saved or not', async () => {
    session.permissions = ['templates.manage', 'labels.print'];
    localStorage.setItem('bam.editor-printer.ql90', 'p1');
    const { sent } = api({
      'GET /printers': printers,
      'POST /printers/p1/fit': () => ({ fits: true, warnings: [], printable: { x: 0, y: 0, width: 90, height: 29 }, edge_mm: 1, outside: [], near_edge: [] }),
      'POST /printers/p1/test-print': () => ({ labels: 1, warnings: ["Part of the template is within 1 mm of the label's edge."] }),
    });
    render(TemplateEditor, { params: { id: 'ql90' } });
    expect(await screen.findByText('Fits QL.', {}, { timeout: 2000 })).toBeTruthy();
    // An unsaved change prints too.
    await fireEvent.change(document.querySelector('#tpl-height')!, { target: { value: '30' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Test print' }));
    expect(await screen.findByText('Sent a test label to QL.')).toBeTruthy();
    expect(within(screen.getByRole('list', { name: 'Printed, with warnings' })).getByText(/within 1 mm/)).toBeTruthy();
    const printed = sent.find((r) => r.path === '/printers/p1/test-print')!.body as { template: TemplateDoc; asset_id?: string };
    expect(printed.template.height).toBe(30);
    expect(printed.asset_id).toBeUndefined();
  });

  it('scales the elements when taking the label size from a printer', async () => {
    api({ 'GET /printers': printers, 'GET /printers/drivers': () => ({ drivers: [] }) });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await vi.waitFor(() => expect(document.querySelector('#tpl-printer')).not.toBeNull());
    await fireEvent.change(document.querySelector('#tpl-printer')!, { target: { value: 'p1' } });
    await fireEvent.change(document.querySelector('#tpl-width')!, { target: { value: '180' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Use its label size' }));
    expect(value('#tpl-width')).toBe('90');
    await fireEvent.focus(canvasElement('Text 1: {{asset.name}}'));
    // Halved across, as the label was.
    expect(value('#el-0-x')).toBe('5');
  });

  it('keeps an adaptive template adaptive when saving it', async () => {
    const adaptive = { ...template, sizing: 'adaptive', adapt: { min_width: 25, max_width: 102, min_height: 20, max_height: 60, continuous_length: 'proportional' },
      elements: [{ ...template.elements[0]!, anchor_x: 'both' }] };
    const { sent } = api({ 'GET /templates/ql90': () => ({ template: adaptive }), 'PUT /templates/ql90': () => ({ template: adaptive }) });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.input(document.querySelector('#tpl-name')!, { target: { value: 'Any tag' } });
    const body = await saved(sent);
    expect(body).toMatchObject({ name: 'Any tag', sizing: 'adaptive', adapt: adaptive.adapt, elements: [{ anchor_x: 'both' }] });
  });

  it('makes a template adaptive, and sets how its elements follow the label', async () => {
    const { sent } = api({ 'PUT /templates/ql90': () => ({ template }), 'POST /templates/range': () => ({ fits: false, units: 'mm' }) });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.click(screen.getByRole('radio', { name: /Fits several sizes/ }));
    expect(value('#tpl-min_width')).toBe('45');
    await fireEvent.change(document.querySelector('#tpl-min_width')!, { target: { value: '25' } });

    await fireEvent.click(within(screen.getByRole('list', { name: 'Layers' })).getByRole('button', { name: 'Text: {{asset.name}}' }));
    await fireEvent.change(screen.getByLabelText('Across'), { target: { value: 'both' } });
    await fireEvent.change(screen.getByLabelText('Where it does not fit'), { target: { value: 'alternative' } });
    await fireEvent.change(screen.getByLabelText('In its place'), { target: { value: 'generator' } });
    const body = await saved(sent);
    expect(body.sizing).toBe('adaptive');
    expect(body.adapt).toMatchObject({ min_width: 25, max_width: 180, continuous_length: 'proportional' });
    expect(body.elements[0]).toMatchObject({ anchor_x: 'both', when_too_small: 'alternative', alternative: { type: 'generator' } });

    // Fixed again, the adaptive settings go.
    await fireEvent.click(screen.getByRole('radio', { name: /One label size/ }));
    expect(document.querySelector('#tpl-min_width')).toBeNull();
    expect(screen.queryByLabelText('Across')).toBeNull();
  });

  it('undoes a field edit even after focus moves on, and not a rename', async () => {
    api();
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    const height = document.querySelector('#tpl-height') as HTMLInputElement;
    await fireEvent.focus(height);
    await fireEvent.change(height, { target: { value: '40' } });
    await fireEvent.focus(document.querySelector('#tpl-dpi')!);
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
    expect(height.value).toBe('29');
    await fireEvent.input(document.querySelector('#tpl-name')!, { target: { value: 'Renamed' } });
    expect((screen.getByRole('button', { name: 'Undo' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('saves a new template and moves to the ID the server gave it', async () => {
    const id = '3f6c2a9e-8b1d-4e57-a0c4-91d2b7e5f803';
    const { sent } = api({ 'POST /templates': (b) => ({ status: 201, body: { template: { ...(b as TemplateDoc), id } } }) });
    render(TemplateEditor, { params: {} });
    expect(await screen.findByRole('heading', { level: 1, name: 'New template' })).toBeTruthy();
    await fireEvent.input(document.querySelector('#tpl-name')!, { target: { value: 'Shelf tag' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(window.location.pathname).toBe(`/templates/${id}/edit`));
    expect((sent.find((r) => r.method === 'POST' && r.path === '/templates')!.body as TemplateDoc).name).toBe('Shelf tag');
  });

  it('shows a saved template its ID', async () => {
    api();
    render(TemplateEditor, { params: { id: 'ql90' } });
    expect(await screen.findByText('ql90')).toBeTruthy();
  });

  it('saves, and shows save errors on the element they name', async () => {
    const { sent } = api({
      'PUT /templates/ql90': () => ({
        status: 400,
        body: { error: 'Invalid template', details: [{ path: 'elements[1].content_binding', message: 'unknown placeholder {{asset.nope}}' }] },
      }),
    });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    // From the keyboard, the code tool adds a code in the middle of the label.
    await fireEvent.click(screen.getByRole('button', { name: 'Barcode or QR code' }), { detail: 0 });
    const body = await saved(sent);
    expect(body.elements[1]).toMatchObject({ type: 'generator', generator_type: 'qr' });
    expect(await screen.findByText('unknown placeholder {{asset.nope}}')).toBeTruthy();
    expect(screen.getByText('Barcode 2 (qr)')).toBeTruthy();
    expect(canvasElement(/^Barcode 2/).classList.contains('problem')).toBe(true);
  });

  it('keeps unsaved work in the browser and offers it back', async () => {
    api();
    const first = render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Text' }), { detail: 0 });
    await vi.waitFor(() => expect(localStorage.getItem('bam.template-draft.ql90')).not.toBeNull());
    first.unmount();

    render(TemplateEditor, { params: { id: 'ql90' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore them' }));
    expect(within(screen.getByRole('list', { name: 'Layers' })).getAllByRole('listitem')).toHaveLength(2);
  });

  it('lists layers top first, and stacks them from the Arrange panel', async () => {
    const { sent } = api({ 'PUT /templates/ql90': () => ({ template }) });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Barcode or QR code' }), { detail: 0 });
    expect(layerNames()).toEqual(['QR code: {{scan_url}}', 'Text: {{asset.name}}']);
    await fireEvent.click(screen.getByRole('button', { name: 'Send to back' }));
    expect(layerNames()).toEqual(['Text: {{asset.name}}', 'QR code: {{scan_url}}']);
    await fireEvent.click(screen.getByRole('button', { name: 'Bring forward' }));
    expect(layerNames()).toEqual(['QR code: {{scan_url}}', 'Text: {{asset.name}}']);
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
    expect(layerNames()).toEqual(['Text: {{asset.name}}', 'QR code: {{scan_url}}']);
    // A locked element is not taken on the label, nor by Ctrl+A.
    await fireEvent.click(screen.getByRole('button', { name: 'Lock Text: {{asset.name}}' }));
    expect(canvasElement(/^Text 2/).classList.contains('locked')).toBe(true);
    await fireEvent.keyDown(window, { key: 'a', ctrlKey: true });
    expect(screen.getByText('Barcode 1 (qr)')).toBeTruthy();

    const body = await saved(sent);
    expect(body.layering).toBe('list');
    expect(body.elements.map((e) => e.type)).toEqual(['generator', 'text']);
    expect(body.elements[1]!.locked).toBe(true);
  });

  it('lists a template from before layering as it was drawn', async () => {
    api({
      'GET /templates/old': () => ({
        template: { ...template, id: 'old', elements: [{ type: 'generator', generator_type: 'qr', x: 1, y: 1, width: 9, height: 9 }, ...template.elements] },
      }),
    });
    render(TemplateEditor, { params: { id: 'old' } });
    await loaded();
    expect(layerNames()).toEqual(['QR code', 'Text: {{asset.name}}']);
  });

  it('turns an element from the tool options', async () => {
    const { sent } = api({ 'PUT /templates/ql90': () => ({ template }) });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.focus(canvasElement('Text 1: {{asset.name}}'));
    await fireEvent.click(screen.getByRole('button', { name: 'Turn 90° right' }));
    expect(value('#el-0-rotation')).toBe('90');
    await fireEvent.change(document.querySelector('#el-0-rotation')!, { target: { value: '400' } });
    expect(value('#el-0-rotation')).toBe('40');
    expect(canvasElement(/^Text 1/).getAttribute('style')).toContain('rotate(40deg)');
    expect((await saved(sent)).elements[0]!.rotation).toBe(40);
  });

  it('sets the margins of a code and where it sits, and warns of a narrow one', async () => {
    const code = { ...template, elements: [{ type: 'generator' as const, generator_type: 'qr', x: 1, y: 1, width: 20, height: 20 }] };
    const { sent } = api({
      'GET /templates/ql90': () => ({ template: code }),
      'PUT /templates/ql90': () => ({ template: code }),
      'POST /templates/preview': (b) => {
        const body = b as { format?: string; template: TemplateDoc };
        if (body.format !== 'json') return '<svg/>';
        const left = body.template.elements[0]!.margin?.left === 0 ? 0 : 1;
        return { elements: [{ index: 0, code: { module: 0.5, module_dots: 6, margin: { top: 2, right: 2, bottom: 2, left }, ...(left ? {} : { narrow: ['left'] }) } }], warnings: [] };
      },
    });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.focus(canvasElement(/^Barcode 1/));
    await fireEvent.change(document.querySelector('#el-0-margin-left')!, { target: { value: '0' } });
    expect(await screen.findByText(/Less than the code's standard quiet zone on its left/)).toBeTruthy();
    await fireEvent.change(document.querySelector('#el-0-margin-top')!, { target: { value: '5' } });
    await fireEvent.change(document.querySelector('#el-0-code-align')!, { target: { value: 'left' } });
    await fireEvent.change(document.querySelector('#el-0-code-valign')!, { target: { value: 'top' } });
    expect((await saved(sent)).elements[0]).toMatchObject({ margin: { left: 0, top: 5 }, align: 'left', vertical_align: 'top' });
  });

  it('adds a shape from the keyboard, sets its paint, and chooses tools with the mouse', async () => {
    const { sent } = api({ 'PUT /templates/ql90': () => ({ template }) });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.click(screen.getByRole('button', { name: 'Rectangle' }), { detail: 0 });
    expect(screen.getByText('Shape 2 (rectangle)')).toBeTruthy();
    await fireEvent.change(document.querySelector('#el-1-fill')!, { target: { value: 'black' } });
    await fireEvent.change(document.querySelector('#el-1-radius')!, { target: { value: '2' } });
    await fireEvent.change(document.querySelector('#el-1-stroke-width')!, { target: { value: '0.05' } });
    expect(await screen.findByText(/thinner than one printer dot at 300 DPI/)).toBeTruthy();
    await fireEvent.change(document.querySelector('#el-1-stroke')!, { target: { value: 'none' } });
    // A mouse click takes the tool in hand, to draw on the label; Escape puts it down.
    const ellipse = screen.getByRole('button', { name: 'Ellipse' });
    await fireEvent.click(ellipse, { detail: 1 });
    expect(ellipse.getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByText('Drag to draw an ellipse, or click to place one. Esc: back to selecting.')).toBeTruthy();
    await fireEvent.keyDown(window, { key: 'Escape' });
    expect(ellipse.getAttribute('aria-pressed')).toBe('false');
    expect(screen.getByRole('button', { name: 'Select and move' }).getAttribute('aria-pressed')).toBe('true');

    const body = await saved(sent);
    expect(body.elements[1]).toEqual({ type: 'shape', shape: 'rectangle', x: 37.75, y: 7.25, width: 14.5, height: 14.5, stroke: 'none', fill: 'black', corner_radius: 2 });
  });

  it('turns the template portrait and back, with what is on it, and chooses the feed', async () => {
    const { sent } = api({ 'PUT /templates/ql90': () => ({ template }), 'GET /printers': printers });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.click(screen.getByRole('radio', { name: 'Portrait' }));
    expect(value('#tpl-width')).toBe('29');
    expect(value('#tpl-height')).toBe('90');
    expect(canvasElement(/^Text 1/).getAttribute('style')).toContain('rotate(90deg)');
    await fireEvent.click(screen.getByRole('radio', { name: 'Landscape' }));
    expect(value('#tpl-width')).toBe('90');
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
    await fireEvent.change(await screen.findByLabelText('Feed'), { target: { value: 'along' } });
    const body = await saved(sent);
    expect(body).toMatchObject({ width: 29, height: 90, feed: 'along' });
    expect(body.elements[0]).toMatchObject({ rotation: 90 });
  });

  it('imports SVG artwork as shapes, selected together, saying what it left out', async () => {
    const shape = (x: number) => ({ type: 'shape' as const, shape: 'path' as const, x, y: 3, width: 5, height: 5, d: 'M0 0 L1 0 L1 1 Z', fill: 'black' as const, stroke: 'none' as const });
    const { sent } = api({
      'POST /templates/import-svg': () => ({ width: 72, height: 23.2, elements: [shape(10), shape(20)], skipped: ['text, which needs the fonts'] }),
      'PUT /templates/ql90': () => ({ template }),
    });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    const file = new File(['<svg viewBox="0 0 10 10"><path d="M0 0 L10 0 L10 10 Z"/></svg>'], 'logo.svg', { type: 'image/svg+xml' });
    await fireEvent.change(screen.getByLabelText('Import SVG'), { target: { files: [file] } });
    expect(await screen.findByText('Imported 2 shapes from logo.svg. Left out: text, which needs the fonts.')).toBeTruthy();
    expect(screen.getByText('2 elements')).toBeTruthy();
    const asked = sent.find((r) => r.path === '/templates/import-svg')!.body as { svg: string; box: { x: number; width: number } };
    expect(asked.box).toMatchObject({ x: 9, width: 72 });
    expect((await saved(sent)).elements.map((e) => e.shape ?? e.type)).toEqual(['text', 'path', 'path']);
  });

  it('keeps guides and groups made before, though it has no tools for them yet', async () => {
    const kept = { ...template, guides: { x: [45] }, elements: [{ ...template.elements[0]!, group: 'group-1' }] };
    const { sent } = api({ 'GET /templates/ql90': () => ({ template: kept }), 'PUT /templates/ql90': () => ({ template: kept }) });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await loaded();
    await fireEvent.change(document.querySelector('#tpl-dpi')!, { target: { value: '203' } });
    expect(await saved(sent)).toMatchObject({ dpi: 203, guides: { x: [45] }, elements: [{ group: 'group-1' }] });
  });
});
