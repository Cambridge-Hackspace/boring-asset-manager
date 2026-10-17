<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { ActionReply, OfferedAction } from '../lib/models';

  // The actions an asset's categories offer on it, such as Identify (README
  // §5.4). Pressing one records an event devices and channels act on; one
  // with a question asks it first. Devices may answer, and their answers
  // show here for a few seconds. Only for those who may press them; the
  // caller checks.
  let { assetId, actions }: { assetId: string; actions: OfferedAction[] } = $props();

  // How long, and how often, to look for answers.
  const listenFor = 8000;
  const every = 1000;

  let busy = $state(false);
  let asking = $state<OfferedAction | null>(null);
  let status = $state('');
  let error = $state('');
  let replies = $state<ActionReply[]>([]);
  let yesButton: HTMLButtonElement | undefined = $state();
  let timer: ReturnType<typeof setTimeout> | undefined;
  onDestroy(() => clearTimeout(timer));

  async function ask(a: OfferedAction) {
    asking = a;
    await tick();
    yesButton?.focus();
  }

  async function press(a: OfferedAction) {
    asking = null;
    busy = true;
    status = error = '';
    replies = [];
    clearTimeout(timer);
    try {
      const { event_id } = await request<{ event_id: string }>('POST', `/assets/${encodeURIComponent(assetId)}/actions/${encodeURIComponent(a.key)}`);
      status = `${a.label}: sent.`;
      listen(a, event_id, Date.now() + listenFor);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  // Looks for answers until the time is up, then says if none came.
  function listen(a: OfferedAction, eventId: string, until: number) {
    timer = setTimeout(async () => {
      try {
        const got = (await request<{ replies: ActionReply[] }>('GET', `/assets/${encodeURIComponent(assetId)}/actions/replies?event_id=${encodeURIComponent(eventId)}`)).replies ?? [];
        if (got.length !== replies.length) replies = got;
      } catch {
        // Answers are a courtesy; the press itself was sent.
      }
      if (Date.now() < until) listen(a, eventId, until);
      else if (!replies.length) status = `${a.label}: sent; nothing answered.`;
    }, every);
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
  <div class="text-sm" aria-live="polite">
    {#if status}<p class="text-green-700 dark:text-green-400">{status}</p>{/if}
    {#if error}<p class="text-red-700 dark:text-red-400">{error}</p>{/if}
    {#if replies.length}
      <ul>
        {#each replies as r (r.id)}
          <li class={r.ok ? '' : 'text-red-700 dark:text-red-400'}>{r.name || 'A device'}: {r.message || (r.ok ? 'done' : 'failed')}</li>
        {/each}
      </ul>
    {/if}
  </div>
</div>
