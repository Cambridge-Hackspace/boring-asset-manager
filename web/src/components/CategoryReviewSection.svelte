<script lang="ts">
  // A category's review settings (docs/review.md): each its own, or taken
  // from its parent as fields are, with what is in effect beside it.
  import { untrack } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { Category, CategoryReview, ReviewRules } from '../lib/models';
  import { effectiveReview, inheritAll, monthsText } from '../lib/review';
  import ErrorMessage from './ErrorMessage.svelte';

  let {
    category,
    categories,
    canManage,
    onsaved,
  }: {
    category: Category;
    /** Every category, for what its parent passes down. */
    categories: Category[];
    canManage: boolean;
    onsaved: (c: Category) => void;
  } = $props();

  // Each setting as the form holds it: '' takes the parent's.
  const own = untrack(() => category.review ?? inheritAll);
  let enabled = $state(own.enabled === null ? '' : own.enabled ? 'on' : 'off');
  let months = $state(own.months === null ? '' : String(own.months));
  let onEdit = $state(own.on_edit ?? '');
  let selfVerify = $state(own.self_verify === null ? '' : own.self_verify ? 'yes' : 'no');
  let busy = $state(false);
  let error = $state('');

  // What its parent passes down, and what is in effect as saved.
  const fromParent = $derived<ReviewRules>(
    category.parent_id ? effectiveReview(categories, category.parent_id) : { enabled: false, months: 0, on_edit: 'draft', self_verify: false },
  );
  const inEffect = $derived(category.effective_review ?? effectiveReview(categories, category.id));

  const onOff = (b: boolean) => (b ? 'On' : 'Off');
  const editText = (e: 'draft' | 'keep') => (e === 'draft' ? 'Returns it to draft' : 'Keeps its state');
  const yesNo = (b: boolean) => (b ? 'Allowed' : 'Someone else verifies');

  async function save(event: SubmitEvent) {
    event.preventDefault();
    busy = true;
    error = '';
    const body: CategoryReview = {
      enabled: enabled === '' ? null : enabled === 'on',
      months: months === '' ? null : Number(months),
      on_edit: onEdit === '' ? null : (onEdit as 'draft' | 'keep'),
      self_verify: selfVerify === '' ? null : selfVerify === 'yes',
    };
    try {
      const r = await request<{ category: Category }>('PUT', `/categories/${encodeURIComponent(category.id)}/review`, body);
      onsaved(r.category);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<section class="mt-6 max-w-2xl space-y-3" aria-labelledby="review-heading">
  <h2 id="review-heading" class="text-lg font-semibold">Review</h2>
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    With review states on, its assets, and those below it, are drafts until someone confirms them, and can be verified by someone
    else. Each setting is its own, or taken from {category.parent_id ? 'its parent' : 'the defaults'}.
  </p>
  <dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 text-sm" aria-label="In effect">
    <dt class="text-zinc-600 dark:text-zinc-400">Review states</dt><dd>{onOff(inEffect.enabled)}</dd>
    {#if inEffect.enabled}
      <dt class="text-zinc-600 dark:text-zinc-400">Review dates</dt><dd>{monthsText(inEffect.months)}</dd>
      <dt class="text-zinc-600 dark:text-zinc-400">Editing a checked asset</dt><dd>{editText(inEffect.on_edit)}</dd>
      <dt class="text-zinc-600 dark:text-zinc-400">Verifying one's own</dt><dd>{yesNo(inEffect.self_verify)}</dd>
    {/if}
  </dl>
  {#if canManage}
    <form class="card space-y-3" onsubmit={save} aria-label="Review settings">
      <div class="grid gap-3 sm:grid-cols-2">
        <div>
          <label class="label" for="review-enabled">Review states</label>
          <select id="review-enabled" class="input" bind:value={enabled}>
            <option value="">From above ({onOff(fromParent.enabled).toLowerCase()})</option>
            <option value="on">On</option>
            <option value="off">Off</option>
          </select>
        </div>
        <div>
          <label class="label" for="review-months">Review every (months)</label>
          <select id="review-months" class="input" bind:value={months}>
            <option value="">From above ({monthsText(fromParent.months).toLowerCase()})</option>
            <option value="0">No review dates</option>
            {#each [1, 3, 6, 12, 24, 36] as m (m)}<option value={String(m)}>{monthsText(m)}</option>{/each}
            {#if months !== '' && ![0, 1, 3, 6, 12, 24, 36].includes(Number(months))}<option value={months}>{monthsText(Number(months))}</option>{/if}
          </select>
        </div>
        <div>
          <label class="label" for="review-on-edit">Editing a confirmed or verified asset</label>
          <select id="review-on-edit" class="input" bind:value={onEdit}>
            <option value="">From above ({editText(fromParent.on_edit).toLowerCase()})</option>
            <option value="draft">Returns it to draft</option>
            <option value="keep">Keeps its state</option>
          </select>
        </div>
        <div>
          <label class="label" for="review-self-verify">Whoever confirmed it verifying it</label>
          <select id="review-self-verify" class="input" bind:value={selfVerify}>
            <option value="">From above ({yesNo(fromParent.self_verify).toLowerCase()})</option>
            <option value="no">Someone else verifies</option>
            <option value="yes">Allowed</option>
          </select>
        </div>
      </div>
      <ErrorMessage {error} />
      <button type="submit" class="btn-secondary" disabled={busy}>Save review settings</button>
    </form>
  {/if}
</section>
