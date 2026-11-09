// Review states (docs/review.md): draft, confirmed and verified assets, the
// dates they are due to be reviewed by, and changes an assistant made that
// wait for someone to accept or reject them.

import type { AssetEvent, AssetReview, Category, CategoryReview, ReviewRules, ReviewState } from './models';

/** A review state in words. */
export function reviewLabel(state: ReviewState): string {
  return { draft: 'Draft', confirmed: 'Confirmed', verified: 'Verified' }[state] ?? state;
}

/** The chip class a state shows with. */
export function reviewChip(state: ReviewState): string {
  return state === 'draft' ? 'chip-muted' : 'chip';
}

/** The asset list's review filters, as ?review= takes them. */
export const reviewFilters: { value: string; label: string }[] = [
  { value: '', label: 'Any review state' },
  { value: 'draft', label: 'Draft' },
  { value: 'confirmed', label: 'Confirmed' },
  { value: 'verified', label: 'Verified' },
  { value: 'due', label: 'Review due' },
  { value: 'overdue', label: 'Review overdue' },
  { value: 'proposed', label: 'Assistant changes' },
];

/** A date as written: the day, not a moment. */
const day = (iso: string) => iso.slice(0, 10);

/** When an asset's review is due, in words: empty when it never is. */
export function dueText(r: AssetReview): string {
  if (!r.due_at) return '';
  if (r.overdue) return `Review overdue since ${day(r.due_at)}`;
  if (r.due) return `Review due by ${day(r.due_at)}`;
  return `Next review by ${day(r.due_at)}`;
}

/** What an asset's history says of a review event, without who did it. */
export function describeReviewEvent(e: AssetEvent): string {
  const p = e.payload;
  if (e.kind === 'asset.review_due') return p.due_at ? `Review due by ${day(p.due_at)}` : 'Review due';
  const r = p.review;
  if (!r) return 'Review changed';
  const said = p.note ? `: “${p.note}”` : '';
  const to = reviewLabel(r.to).toLowerCase();
  switch (r.cause) {
    case 'create':
      return `Added as ${to}`;
    case 'edit':
      return 'Returned to draft by an edit';
    case 'assistant':
      return 'Changed through an assistant: a draft until someone accepts or rejects it';
    case 'accept':
      return r.from === r.to ? 'Assistant changes accepted' : `Assistant changes accepted; now ${to}`;
    case 'reject':
      return `Assistant changes rejected${said}`;
  }
  if (r.to === 'draft') return `Returned to draft${said}`;
  return `${reviewLabel(r.to)}${said}`;
}

/** The settings a category's own review settings leave as they are: each taken from its parent. */
export const inheritAll: CategoryReview = { enabled: null, months: null, on_edit: null, self_verify: null };

/** The review settings in effect for a category, as the server works them out, from a category list. */
export function effectiveReview(categories: Category[], id: string): ReviewRules {
  const byId = new Map(categories.map((c) => [c.id, c]));
  const out: { enabled?: boolean; months?: number; on_edit?: 'draft' | 'keep'; self_verify?: boolean } = {};
  for (let c = byId.get(id), depth = 0; c && depth < 100; c = c.parent_id ? byId.get(c.parent_id) : undefined, depth++) {
    const own = c.review ?? inheritAll;
    out.enabled ??= own.enabled ?? undefined;
    out.months ??= own.months ?? undefined;
    out.on_edit ??= own.on_edit ?? undefined;
    out.self_verify ??= own.self_verify ?? undefined;
  }
  return { enabled: out.enabled ?? false, months: out.months ?? 0, on_edit: out.on_edit ?? 'draft', self_verify: out.self_verify ?? false };
}

/** Whether an asset in these categories would use review states. */
export function usesReview(categories: Category[], ids: string[]): boolean {
  return ids.some((id) => effectiveReview(categories, id).enabled);
}

/** Months between reviews in words. */
export function monthsText(months: number): string {
  if (months <= 0) return 'No review dates';
  return months === 1 ? 'Every month' : `Every ${months} months`;
}
