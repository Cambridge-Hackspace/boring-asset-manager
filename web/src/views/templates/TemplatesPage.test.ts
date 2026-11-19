import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import TemplatesPage from './TemplatesPage.svelte';
import { fakeApi } from '../../test/fetch';
import { router } from '../../lib/router.svelte';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('TemplatesPage', () => {
  it('writes a screen’s odd size to a hundredth, so the list fits a phone', async () => {
    fakeApi({
      'GET /templates': () => ({
        templates: [{ id: 't1', name: 'Shelf screen', units: 'mm', width: (296 * 25.4) / 112, height: (128 * 25.4) / 112, dpi: 112 }],
      }),
    });
    render(TemplatesPage, { params: {} });
    expect(await screen.findAllByText('67.13 × 29.03 mm, 112 DPI')).toHaveLength(2);
  });

  it('duplicates a template beside Delete, and opens the copy', async () => {
    const { sent } = fakeApi({
      'GET /templates': () => ({ templates: [{ id: 't1', name: 'Shelf', units: 'mm', width: 62, height: 29, dpi: 300 }] }),
      'POST /templates/t1/clone': () => ({ status: 201, body: { template: { id: 't2', name: 'Shelf copy', units: 'mm', width: 62, height: 29, dpi: 300 } } }),
    });
    render(TemplatesPage, { params: {} });
    const button = await screen.findByRole('button', { name: 'Duplicate Shelf' });
    expect(button.nextElementSibling?.textContent).toBe('Delete');
    await fireEvent.click(button);
    await waitFor(() => expect(router.path).toBe('/templates/t2/edit'));
    expect(sent.some((r) => r.method === 'POST' && r.path === '/templates/t1/clone')).toBe(true);
  });
});
