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
