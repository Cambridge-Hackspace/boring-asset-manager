import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import AdaptivePreview from './AdaptivePreview.svelte';
import { fakeApi } from '../../test/fetch';
import type { TemplateDoc } from '../../lib/template';

const doc: TemplateDoc = {
  id: 'tag',
  name: 'Any tag',
  units: 'mm',
  width: 62,
  height: 29,
  dpi: 300,
  sizing: 'adaptive',
  adapt: { min_width: 10, max_width: 102, min_height: 10, max_height: 60, continuous_length: 'proportional' },
  elements: [
    { type: 'generator', generator_type: 'qr', x: 2, y: 2, width: 25, height: 25 },
    { type: 'text', x: 30, y: 2, width: 30, height: 10, content_binding: '{{asset.name}}', font_size: 12 },
  ],
};

const printer = (id: string, name: string) => ({
  id, name, driver: 'brother_ql', model: 'QL-810W', media: 'm', connection: { type: 'tcp', address: 'ql' }, created_at: '', updated_at: '',
  loaded: { id: 'm', name: `${name} roll`, width_mm: 29, die_cut: false },
});

beforeEach(() => {
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: () => 'blob:preview', revokeObjectURL: () => {} }));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('AdaptivePreview', () => {
  it('shows the sizes it takes, and how each printer lays it out, naming elements as the editor does', async () => {
    const { sent } = fakeApi({
      'GET /printers': () => ({ printers: [printer('p1', 'Tape'), printer('p2', 'Tiny')] }),
      'POST /templates/range': () => ({
        fits: true, units: 'mm', smallest: { width: 55.8, height: 26.1 }, largest: { width: 102, height: 47.71 },
        smaller: 'elements[0]: would run off a 55 × 25.7 mm label',
      }),
      'POST /templates/adapt': (b) => {
        const ask = b as { printer_id?: string; width?: number };
        if (ask.printer_id === 'p1') return { fits: true, template: { ...doc, width: 62, height: 29 }, notices: ['elements[1] (text) is left off a 62 × 29 mm label: its box would be small.'] };
        if (ask.printer_id === 'p2') return { fits: false, problem: { path: 'elements[0]', message: 'would run off a 12 × 6 mm label' } };
        return { fits: true, template: { ...doc, width: ask.width }, notices: [] };
      },
      'POST /templates/preview': () => '<svg/>',
    });
    render(AdaptivePreview, { doc });

    expect(await screen.findByText(/In its shape it fits labels from 55.8 × 26.1 to 102 × 47.71 mm/, {}, { timeout: 2000 })).toBeTruthy();
    expect(screen.getByText('Smaller: Barcode 1 (qr): would run off a 55 × 25.7 mm label')).toBeTruthy();
    // Its declared range goes down to 10 mm, smaller than it fits.
    expect(screen.getByText(/Its range goes smaller than it fits/)).toBeTruthy();

    const list = screen.getByRole('list', { name: 'On each printer' });
    expect(within(list).getByRole('img', { name: 'Preview at Tape, Tape roll' })).toBeTruthy();
    expect(within(list).getByText(/Text 2: \{\{asset.name\}\} is left off/)).toBeTruthy();
    expect(within(list).getByText('Does not fit: Barcode 1 (qr): would run off a 12 × 6 mm label')).toBeTruthy();
    expect(sent.filter((r) => r.path === '/templates/adapt').map((r) => (r.body as { printer_id: string }).printer_id).sort()).toEqual(['p1', 'p2']);

    await fireEvent.input(screen.getByLabelText('Width (mm)'), { target: { value: '80' } });
    await fireEvent.input(screen.getByLabelText('Height (mm)'), { target: { value: '40' } });
    await fireEvent.submit(screen.getByRole('button', { name: 'Show this size' }));
    const typed = await screen.findByRole('list', { name: 'At the size typed in' });
    expect(within(typed).getByText('80 × 40 mm at 300 DPI')).toBeTruthy();
    expect(sent.at(-2)!.body).toMatchObject({ width: 80, height: 40, dpi: 300 });
  });
});
