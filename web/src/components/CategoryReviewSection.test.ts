import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/svelte';
import CategoryReviewSection from './CategoryReviewSection.svelte';
import { fakeApi } from '../test/fetch';
import type { Category } from '../lib/models';

const cat = (id: string, parent: string | null, review: Category['review'], effective?: Category['effective_review']): Category => ({
  id, name: id, parent_id: parent, color_hex: '#000000', is_consumable: false, unit: '', is_lendable: false, is_running_low: false, review, effective_review: effective,
  created_at: '', updated_at: '',
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('CategoryReviewSection', () => {
  it('shows what is in effect and what comes from above, and saves its own settings', async () => {
    const tools = cat('tools', null, { enabled: true, months: 12, on_edit: null, self_verify: null }, { enabled: true, months: 12, on_edit: 'draft', self_verify: false });
    const saws = cat('saws', 'tools', { enabled: null, months: null, on_edit: null, self_verify: null }, { enabled: true, months: 12, on_edit: 'draft', self_verify: false });
    const { sent } = fakeApi({ 'PUT /categories/saws/review': (body) => ({ category: { ...saws, review: body } }) });
    const saved = vi.fn();
    render(CategoryReviewSection, { category: saws, categories: [tools, saws], canManage: true, onsaved: saved });
    expect(screen.getByLabelText('In effect').textContent).toContain('Every 12 months');
    const form = screen.getByRole('form', { name: 'Review settings' });
    expect(within(form).getByRole('option', { name: 'From above (on)' })).toBeTruthy();
    await fireEvent.change(within(form).getByLabelText('Review every (months)'), { target: { value: '6' } });
    await fireEvent.change(within(form).getByLabelText('Editing a confirmed or verified asset'), { target: { value: 'keep' } });
    await fireEvent.click(within(form).getByRole('button', { name: 'Save review settings' }));
    await vi.waitFor(() => expect(saved).toHaveBeenCalled());
    expect(sent[0]?.body).toEqual({ enabled: null, months: 6, on_edit: 'keep', self_verify: null });
  });

  it('shows only what is in effect to those who cannot manage categories', () => {
    const tools = cat('tools', null, { enabled: false, months: null, on_edit: null, self_verify: null }, { enabled: false, months: 0, on_edit: 'draft', self_verify: false });
    render(CategoryReviewSection, { category: tools, categories: [tools], canManage: false, onsaved: vi.fn() });
    expect(screen.queryByRole('form')).toBeNull();
    expect(screen.getByLabelText('In effect').textContent).toContain('Off');
  });
});
