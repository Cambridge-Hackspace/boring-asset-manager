import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/svelte';
import TemplatesPage from './TemplatesPage.svelte';
import { fakeApi } from '../../test/fetch';

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
    expect(await screen.findByText('67.13 × 29.03 mm, 112 DPI')).toBeTruthy();
  });
});
