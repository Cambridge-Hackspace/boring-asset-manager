<!--
  Review states (docs/review.md): whether changes through an assistant land
  as drafts, for a person to accept or reject, and how many days before an
  asset's review date it falls due, which announces it.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { ReviewSettings } from '../../lib/models';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let assistantDrafts = $state(true);
  let dueDays = $state(14);
  let error = $state('');
  let status = $state('');
  let busy = $state(false);
  // Nothing can be changed until the settings are read, so a change is not
  // overwritten as they arrive.
  let loaded = $state(false);

  function show(s: ReviewSettings) {
    [assistantDrafts, dueDays] = [s.assistant_drafts, s.due_days];
    loaded = true;
  }

  onMount(async () => {
    try {
      show(await request<ReviewSettings>('GET', '/settings/review'));
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    busy = true;
    try {
      show(await request<ReviewSettings>('PUT', '/settings/review', { assistant_drafts: assistantDrafts, due_days: Number(dueDays) }));
      status = 'Saved.';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<section class="card mb-6 max-w-3xl space-y-3" aria-labelledby="review-settings-heading">
  <h2 id="review-settings-heading" class="text-lg font-semibold">Review</h2>
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    For categories that use review states. A review falls due, and is announced as <em>An asset's review falls due</em>, this many days
    before its date.
  </p>
  <ErrorMessage {error} />
  <form class="space-y-3" onsubmit={save}>
    <label class="flex items-start gap-2 text-sm">
      <input class="mt-1" type="checkbox" bind:checked={assistantDrafts} disabled={!loaded} />
      <span>Changes through an assistant land as drafts, for someone to accept or reject</span>
    </label>
    <div class="flex flex-wrap items-end gap-3">
      <div>
        <label class="label" for="review-due-days">Days before a review date it is due</label>
        <input id="review-due-days" class="input w-24" type="number" min="0" max="365" required disabled={!loaded} bind:value={dueDays} />
      </div>
      <button type="submit" class="btn-secondary" disabled={busy || !loaded}>Save</button>
      {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
    </div>
  </form>
</section>
