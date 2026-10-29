<script lang="ts">
  // /flows: the flows someone may run (README §6.7).
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Flow } from '../../lib/flows';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let flows = $state<Flow[]>([]);
  let loading = $state(true);
  let error = $state('');

  onMount(async () => {
    try {
      flows = (await request<{ flows: Flow[] }>('GET', '/flows')).flows ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  });

  const runnable = $derived(flows.filter((f) => f.enabled));
</script>

<h1 class="mb-4 text-2xl font-bold">Flows</h1>
<p class="mb-4 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
  Guided pages for everyday things, such as leaving something here or collecting it. Each has a poster with a QR code to start it from a phone.
</p>
<ErrorMessage {error} />
{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading flows…</p>
{:else if runnable.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No flows to run yet.</p>
{:else}
  <ul class="grid max-w-3xl gap-3 sm:grid-cols-2">
    {#each runnable as f (f.id)}
      <li>
        <a class="flow-choice" href="/f/{f.slug}">
          <span class="flex flex-col">
            <span>{f.name}</span>
            {#if f.definition.description}<span class="text-sm font-normal text-zinc-600 dark:text-zinc-400">{f.definition.description}</span>{/if}
          </span>
        </a>
      </li>
    {/each}
  </ul>
{/if}
