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
  session.permissions = ['templates:manage'];
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
