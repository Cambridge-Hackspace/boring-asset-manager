<script lang="ts">
  import { tick } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { OfferedAction } from '../lib/models';

  // The actions an asset's categories offer on it, such as Identify (README
  // §5.4). Pressing one records an event devices and channels act on; one
  // with a question asks it first. Only for those who may press them; the
  // caller checks.
  let { assetId, actions }: { assetId: string; actions: OfferedAction[] } = $props();

  let busy = $state(false);
  let asking = $state<OfferedAction | null>(null);
  let status = $state('');
  let error = $state('');
  let yesButton: HTMLButtonElement | undefined = $state();

  async function ask(a: OfferedAction) {
    asking = a;
    await tick();
    yesButton?.focus();
  }

  async function press(a: OfferedAction) {
    asking = null;
    busy = true;
    status = error = '';
    try {
      await request('POST', `/assets/${encodeURIComponent(assetId)}/actions/${encodeURIComponent(a.key)}`);
      status = `${a.label}: sent.`;
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="flex flex-wrap items-center gap-2">
  {#if asking}
    <span class="inline-flex flex-wrap items-center gap-2" role="group" aria-label={asking.confirm}>
      <span class="text-sm">{asking.confirm}</span>
      <button type="button" class="btn-primary" bind:this={yesButton} onclick={() => press(asking!)}>{asking.label}</button>
      <button type="button" class="btn-secondary" onclick={() => (asking = null)}>Cancel</button>
    </span>
  {:else}
    {#each actions as a (a.id)}
      <button type="button" class="btn-secondary" disabled={busy} onclick={() => (a.confirm ? ask(a) : press(a))}>{a.label}</button>
    {/each}
  {/if}
  <span class="text-sm" aria-live="polite">
    {#if status}<span class="text-green-700 dark:text-green-400">{status}</span>{/if}
    {#if error}<span class="text-red-700 dark:text-red-400">{error}</span>{/if}
  </span>
</div>
