<script lang="ts">
  // The changes made offline that are not yet recorded
  // (docs/installing.md#offline): those waiting for a connection, and those
  // held for the person to decide, with what the server said. A restock
  // someone else's restock may have counted already is shown, never added
  // to theirs unasked.
  import { ago } from '../lib/offline/copy';
  import { offline } from '../lib/offline/offline.svelte';
  import { describeChange, type QueuedChange } from '../lib/offline/queue';

  let busy = $state(false);

  async function decide(change: QueuedChange, decision: 'anyway' | 'retry' | 'drop') {
    busy = true;
    try {
      await offline.resolve(change.id, decision);
    } finally {
      busy = false;
    }
  }

  function when(at: string): string {
    return new Date(at).toLocaleString();
  }
</script>

{#if offline.queue.length}
  <section id="waiting" class="card space-y-3" aria-labelledby="waiting-heading">
    <h2 id="waiting-heading" class="text-lg font-semibold">Not yet recorded</h2>
    <ul class="space-y-3">
      {#each offline.queue as change (change.id)}
        <li class="space-y-2 border-t border-zinc-200 pt-3 first:border-t-0 first:pt-0 dark:border-zinc-800">
          <p>
            <span class="font-medium">{describeChange(change)}</span>
            <span class="text-sm text-zinc-600 dark:text-zinc-400">· made {ago(change.at)}</span>
          </p>
          {#if change.state === 'waiting'}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">Waiting to send. It is sent as soon as there is a connection.</p>
          {:else if change.state === 'conflict' && change.restocked?.length}
            <div role="alert" class="space-y-2 text-sm">
              <p>
                It was restocked after your copy of {when(change.since)}, so this may be the same purchase:
              </p>
              <ul class="list-disc pl-5">
                {#each change.restocked as r (r.at + r.change)}
                  <li>{r.change} {change.unit ?? ''} added {when(r.at)}{r.by ? ` by ${r.by}` : ''}</li>
                {/each}
              </ul>
              {#if change.now !== undefined}<p>There are {change.now} {change.unit ?? ''} now.</p>{/if}
              <div class="flex flex-wrap gap-2">
                <button type="button" class="btn-secondary" disabled={busy} onclick={() => decide(change, 'anyway')}>Record mine too</button>
                <button type="button" class="btn-secondary" disabled={busy} onclick={() => decide(change, 'drop')}>Drop mine</button>
              </div>
            </div>
          {:else}
            <div role="alert" class="space-y-2 text-sm">
              <p class="error-text">Not recorded: {change.message}</p>
              <div class="flex flex-wrap gap-2">
                <button type="button" class="btn-secondary" disabled={busy} onclick={() => decide(change, 'retry')}>Try again</button>
                <button type="button" class="btn-secondary" disabled={busy} onclick={() => decide(change, 'drop')}>Drop it</button>
              </div>
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  </section>
{/if}
