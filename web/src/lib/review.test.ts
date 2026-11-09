import { describe, expect, it } from 'vitest';
import { describeReviewEvent, dueText, effectiveReview, monthsText, reviewChip, reviewLabel, usesReview } from './review';
import { describeEvent } from './assets';
import type { AssetEvent, AssetReview, Category } from './models';

const cat = (id: string, parent: string | null, review: Category['review']): Category => ({
  id, name: id, parent_id: parent, color_hex: '#000000', is_consumable: false, unit: '', is_lendable: false, is_running_low: false, review, created_at: '', updated_at: '',
});

const review = (over: Partial<AssetReview>): AssetReview => ({
  state: 'confirmed', set_by: null, set_at: null, confirmed_by: null, confirmed_at: null, reviewed_at: null, due_at: '2027-11-08T10:00:00Z',
  due: false, overdue: false, rules: { enabled: true, months: 12, on_edit: 'draft', self_verify: false }, proposal: null, ...over,
});

const event = (kind: string, payload: Partial<AssetEvent['payload']>): AssetEvent => ({ id: 'e', kind, created_at: '', payload: { trigger: 'manual', ...payload } });

describe('review states', () => {
  it('names states, and shows drafts muted', () => {
    expect(reviewLabel('verified')).toBe('Verified');
    expect(reviewChip('draft')).toBe('chip-muted');
    expect(reviewChip('confirmed')).toBe('chip');
  });

  it('says when a review is due', () => {
    expect(dueText(review({}))).toBe('Next review by 2027-11-08');
    expect(dueText(review({ due: true }))).toBe('Review due by 2027-11-08');
    expect(dueText(review({ due: true, overdue: true }))).toBe('Review overdue since 2027-11-08');
    expect(dueText(review({ due_at: null }))).toBe('');
    expect(monthsText(0)).toBe('No review dates');
    expect(monthsText(1)).toBe('Every month');
  });

  it('takes each setting from the nearest category that sets it, as fields are passed down', () => {
    const all = [
      cat('tools', null, { enabled: true, months: 12, on_edit: null, self_verify: null }),
      cat('saws', 'tools', { enabled: null, months: 6, on_edit: 'keep', self_verify: null }),
      cat('hand', 'saws', { enabled: false, months: null, on_edit: null, self_verify: null }),
      cat('other', null, undefined),
    ];
    expect(effectiveReview(all, 'saws')).toEqual({ enabled: true, months: 6, on_edit: 'keep', self_verify: false });
    expect(effectiveReview(all, 'hand')).toEqual({ enabled: false, months: 6, on_edit: 'keep', self_verify: false });
    expect(effectiveReview(all, 'other')).toEqual({ enabled: false, months: 0, on_edit: 'draft', self_verify: false });
    expect(usesReview(all, ['other', 'saws'])).toBe(true);
    expect(usesReview(all, ['hand'])).toBe(false);
  });

  it('describes review moves in history', () => {
    expect(describeEvent(event('asset.review_changed', { review: { from: 'draft', to: 'confirmed', cause: 'move' } }))).toBe('Confirmed');
    expect(describeEvent(event('asset.review_changed', { review: { from: 'confirmed', to: 'draft', cause: 'move' }, note: 'Wrong size' }))).toBe('Returned to draft: “Wrong size”');
    expect(describeEvent(event('asset.review_changed', { review: { from: 'verified', to: 'draft', cause: 'edit' } }))).toBe('Returned to draft by an edit');
    expect(describeEvent(event('asset.review_changed', { review: { to: 'confirmed', cause: 'create' } }))).toBe('Added as confirmed');
    expect(describeEvent(event('asset.review_changed', { review: { from: 'draft', to: 'draft', cause: 'accept' } }))).toBe('Assistant changes accepted');
    expect(describeEvent(event('asset.review_changed', { review: { from: 'draft', to: 'confirmed', cause: 'accept' } }))).toBe('Assistant changes accepted; now confirmed');
    expect(describeReviewEvent(event('asset.review_changed', { review: { from: 'draft', to: 'verified', cause: 'reject' } }))).toBe('Assistant changes rejected');
    expect(describeEvent(event('asset.review_changed', { review: { from: 'confirmed', to: 'draft', cause: 'assistant' } }))).toContain('through an assistant');
    expect(describeEvent(event('asset.review_due', { trigger: 'schedule', due_at: '2027-01-02T00:00:00Z' }))).toBe('Review due by 2027-01-02');
  });
});
