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
  it('previews through the server and nudges elements with the keyboard', async () => {
    const { sent } = api();
    render(TemplateEditor, { params: { id: 'ql90' } });
    await vi.waitFor(() => expect(sent.some((r) => r.path === '/templates/preview')).toBe(true));
    expect((sent.find((r) => r.path === '/templates/preview')!.body as { template: TemplateDoc }).template.elements).toHaveLength(1);

    // The canvas boxes are the buttons with a pressed state; the element list repeats the names.
    const box = await screen.findByRole('button', { name: 'Text 1: {{asset.name}}', pressed: false });
    await fireEvent.focus(box);
    await fireEvent.keyDown(box, { key: 'ArrowRight' });
    await fireEvent.keyDown(box, { key: 'ArrowDown', shiftKey: true });
    expect((screen.getByLabelText('Left') as HTMLInputElement).value).toBe('10.5');
    expect((screen.getByLabelText('Top') as HTMLInputElement).value).toBe('10');
    expect(screen.getByText('Unsaved changes')).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
    expect((screen.getByLabelText('Top') as HTMLInputElement).value).toBe('5');
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
    expect((screen.getByLabelText('Printer') as HTMLSelectElement).value).toBe('p1');
    expect(within(screen.getByRole('list', { name: 'Before printing' })).getByText(/does not print/)).toBeTruthy();
    expect(screen.getByText('In the shaded area, which it does not print: Text 1: {{asset.name}}.')).toBeTruthy();
    expect(screen.getByText('Loaded: DK-11201 29 × 90 mm die-cut.')).toBeTruthy();
    // Four strips around what it prints, and the line a die-cut label may be cut at.
    expect(container.querySelectorAll('.unprinted')).toHaveLength(4);
    expect(screen.getByText(/Dashed: within 1 mm of the edge/)).toBeTruthy();
    const fit = sent.find((r) => r.path === '/printers/p1/fit')!;
    expect(fit.body).toEqual({ width_mm: 90, height_mm: 29, dpi: 300, content: [{ x: 10, y: 5, width: 40, height: 10 }] });

    // Choosing None turns the check off, and is remembered.
    await fireEvent.change(screen.getByLabelText('Printer'), { target: { value: '' } });
    await vi.waitFor(() => expect(container.querySelectorAll('.unprinted')).toHaveLength(0));
    expect(localStorage.getItem('bam.editor-printer.ql90')).toBe('');
  });

  it('keeps a printer chosen by hand while it is still choosing one, and drops late answers', async () => {
    // The Rollo, first, takes it but answers slowly; the person chooses the
    // QL meanwhile, which does not.
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
    const select = (await screen.findByLabelText('Printer')) as HTMLSelectElement;
    await vi.waitFor(() => expect(sent.some((r) => r.path === '/printers/p0/fit')).toBe(true));
    await fireEvent.change(select, { target: { value: 'p1' } });
    expect(await screen.findByText(/can't print on QL/, {}, { timeout: 2000 })).toBeTruthy();
    release();
    await new Promise((r) => setTimeout(r, 400));
    expect(select.value).toBe('p1');
    expect(screen.queryByText('Fits Rollo.')).toBeNull();
    expect(screen.getByText(/can't print on QL/)).toBeTruthy();
  });

  it('test-prints the template as it is, saved or not', async () => {
    session.permissions = ['templates.manage', 'labels.print'];
    localStorage.setItem('bam.editor-printer.ql90', 'p1');
    const { sent } = api({
      'GET /printers': printers,
      'POST /printers/p1/fit': () => ({ fits: true, warnings: [], printable: { x: 0, y: 0, width: 90, height: 29 }, edge_mm: 1, outside: [], near_edge: [] }),
      'POST /printers/p1/test-print': () => ({ labels: 1, warnings: ['Part of the template is within 1 mm of the label\'s edge.'] }),
    });
    render(TemplateEditor, { params: { id: 'ql90' } });
    expect(await screen.findByText('Fits QL.', {}, { timeout: 2000 })).toBeTruthy();
    // An unsaved change prints too.
    await fireEvent.change(screen.getByLabelText('Height'), { target: { value: '30' } });
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
    await fireEvent.change(await screen.findByLabelText('Printer'), { target: { value: 'p1' } });
    await fireEvent.change(screen.getByLabelText('Width'), { target: { value: '180' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Use its label size' }));
    expect((screen.getByLabelText('Width') as HTMLInputElement).value).toBe('90');
    await fireEvent.focus(await screen.findByRole('button', { name: 'Text 1: {{asset.name}}', pressed: false }));
    // Halved across, as the label was.
    expect((screen.getByLabelText('Left') as HTMLInputElement).value).toBe('5');
  });

  it('keeps an adaptive template adaptive when saving it', async () => {
    const adaptive = { ...template, sizing: 'adaptive', adapt: { min_width: 25, max_width: 102, min_height: 20, max_height: 60, continuous_length: 'proportional' },
      elements: [{ ...template.elements[0]!, anchor_x: 'both' }] };
    const { sent } = api({ 'GET /templates/ql90': () => ({ template: adaptive }), 'PUT /templates/ql90': () => ({ template: adaptive }) });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await screen.findByDisplayValue('QL 90x29');
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Any tag' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    expect(sent.find((r) => r.method === 'PUT')!.body).toMatchObject({ name: 'Any tag', sizing: 'adaptive', adapt: adaptive.adapt, elements: [{ anchor_x: 'both' }] });
  });

  it('makes a template adaptive, and sets how its elements follow the label', async () => {
    const { sent } = api({ 'PUT /templates/ql90': () => ({ template }), 'POST /templates/range': () => ({ fits: false, units: 'mm' }) });
    render(TemplateEditor, { params: { id: 'ql90' } });
    await screen.findByDisplayValue('QL 90x29');
    await fireEvent.click(screen.getByRole('radio', { name: /Fits several label sizes/ }));
    expect((screen.getByLabelText('Narrowest (mm)') as HTMLInputElement).value).toBe('45');
    await fireEvent.change(screen.getByLabelText('Narrowest (mm)'), { target: { value: '25' } });

    await fireEvent.click(within(screen.getByRole('list', { name: 'Elements' })).getByRole('button', { name: /^Text 1/ }));
    await fireEvent.change(screen.getByLabelText('Across'), { target: { value: 'both' } });
    await fireEvent.change(screen.getByLabelText('Where it does not fit'), { target: { value: 'alternative' } });
    await fireEvent.change(screen.getByLabelText('In its place'), { target: { value: 'generator' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    const body = sent.find((r) => r.method === 'PUT')!.body as TemplateDoc;
    expect(body.sizing).toBe('adaptive');
    expect(body.adapt).toMatchObject({ min_width: 25, max_width: 180, continuous_length: 'proportional' });
    expect(body.elements[0]).toMatchObject({ anchor_x: 'both', when_too_small: 'alternative', alternative: { type: 'generator' } });

    // Fixed again, the adaptive settings go.
    await fireEvent.click(screen.getByRole('radio', { name: /One label size/ }));
    expect(screen.queryByLabelText('Narrowest (mm)')).toBeNull();
    expect(screen.queryByLabelText('Across')).toBeNull();
  });

  it('undoes a field edit even after focus moves on', async () => {
    api();
    render(TemplateEditor, { params: { id: 'ql90' } });
    const height = (await screen.findByLabelText('Height', { selector: '#tpl-height' })) as HTMLInputElement;
    await fireEvent.focus(height);
    await fireEvent.change(height, { target: { value: '40' } });
    // Moving to the next field must not record the edited state as a step.
    await fireEvent.focus(screen.getByLabelText('DPI'));
    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
    expect(height.value).toBe('29');
    // Renaming is not an undo step at all.
    await fireEvent.input(screen.getByLabelText('Name', { selector: '#tpl-name' }), { target: { value: 'Renamed' } });
    expect((screen.getByRole('button', { name: 'Undo' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('saves a new template and moves to the ID the server gave it', async () => {
    const id = '3f6c2a9e-8b1d-4e57-a0c4-91d2b7e5f803';
    const { sent } = api({ 'POST /templates': (b) => ({ status: 201, body: { template: { ...(b as TemplateDoc), id } } }) });
    render(TemplateEditor, { params: {} });
    const name = await screen.findByLabelText('Name', { selector: '#tpl-name' });
    // There is no ID to choose.
    expect(screen.queryByText('ID')).toBeNull();
    await fireEvent.input(name, { target: { value: 'Shelf tag' } });
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
    await fireEvent.click(await screen.findByRole('button', { name: 'Add barcode' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await vi.waitFor(() => expect(sent.some((r) => r.method === 'PUT')).toBe(true));
    const body = sent.find((r) => r.method === 'PUT')!.body as TemplateDoc;
    expect(body.elements[1]).toMatchObject({ type: 'generator', generator_type: 'qr' });
    // The element with the problem is selected and its panel shows the message.
    expect(await screen.findByText('unknown placeholder {{asset.nope}}')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Barcode 2 (qr)' })).toBeTruthy();
  });

  it('keeps unsaved work in the browser and offers it back', async () => {
    api();
    const first = render(TemplateEditor, { params: { id: 'ql90' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Add text' }));
    await vi.waitFor(() => expect(localStorage.getItem('bam.template-draft.ql90')).not.toBeNull());
    first.unmount();

    render(TemplateEditor, { params: { id: 'ql90' } });
    await fireEvent.click(await screen.findByRole('button', { name: 'Restore them' }));
    expect(within(screen.getByRole('list', { name: 'Elements' })).getAllByRole('button')).toHaveLength(2);
  });
});
