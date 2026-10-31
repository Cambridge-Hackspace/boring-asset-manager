<script lang="ts">
  // /flows: the flows someone may run, as cards; for those who manage
  // flows, every flow, to make, change, turn off and on, and delete
  // (README §6.7).
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { session } from '../../lib/session.svelte';
  import type { Flow, Overdue } from '../../lib/flows';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let flows = $state<Flow[]>([]);
  let overdue = $state<Record<string, Overdue[]>>({});
  let loading = $state(true);
  let error = $state('');
  let status = $state('');
  const manages = $derived(session.can('flows.manage'));

  async function load() {
    try {
      const res = await request<{ flows: Flow[]; overdue?: Record<string, Overdue[]> }>('GET', '/flows');
      flows = res.flows ?? [];
      overdue = res.overdue ?? {};
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  const runnable = $derived(flows.filter((f) => f.enabled && (!manages || session.can('flows.run'))));

  async function act(action: () => Promise<unknown>, done: string) {
    error = status = '';
    try {
      await action();
      status = done;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  function setEnabled(f: Flow, enabled: boolean) {
    return act(
      () => request('PUT', `/flows/${encodeURIComponent(f.slug)}`, { slug: f.slug, name: f.name, enabled, definition: f.definition }),
      `${f.name} is turned ${enabled ? 'on' : 'off'}.`,
    );
  }
</script>

<div class="mb-6 flex flex-wrap items-end justify-between gap-4">
  <div>
    <h1 class="mb-1 text-3xl font-bold">Flows</h1>
    <p class="max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
      Guided pages for everyday things, such as leaving something here or collecting it. Each has a poster with a QR code to start it from a phone.
    </p>
  </div>
  {#if manages}<a class="btn-primary" href="/flows/new">New flow</a>{/if}
</div>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading flows…</p>
{:else if (manages ? flows : runnable).length === 0}
  <div class="empty-state">
    {#if manages}
      <p>No flows yet. <strong>New flow</strong> starts one from <em>Leave something</em> or <em>Collect it</em>.</p>
    {:else}
      <p>No flows to run yet.</p>
    {/if}
  </div>
{:else}
  <!-- One card a flow: those who manage flows see every one, to change. -->
  <ul class="grid gap-6 sm:grid-cols-2 lg:grid-cols-3" aria-label={manages ? 'Every flow' : 'Flows to run'}>
    {#each manages ? flows : runnable as f (f.id)}
      <li class="card flex flex-col gap-3 p-6">
        <p class="flex flex-wrap gap-2">
          <span class="chip-muted">/f/{f.slug}</span>
          {#if !f.enabled}<span class="chip">Turned off</span>{/if}
        </p>
        <h2 class="text-xl font-bold">{f.name}</h2>
        {#if f.definition.description}<p class="text-sm">{f.definition.description}</p>{/if}
        {#if manages}<p class="text-xs text-zinc-600 dark:text-zinc-400">{f.definition.steps.length} steps</p>{/if}
        {#if manages && overdue[f.id]?.length}
          <details class="text-sm">
            <summary class="text-amber-800 dark:text-amber-400">{overdue[f.id]!.length} past {overdue[f.id]!.length === 1 ? 'its' : 'their'} date</summary>
            <ul class="mt-1 space-y-0.5">
              {#each overdue[f.id]! as o (o.asset_id)}
                <li><a class="underline" href="/assets/{o.asset_id}">{o.name}</a>, due {o.until}</li>
              {/each}
            </ul>
          </details>
        {/if}
        <span class="mt-auto flex flex-wrap gap-2 pt-1">
          {#if runnable.includes(f)}<a class="btn-secondary" href="/f/{f.slug}" aria-label="Run {f.name}">Run</a>{/if}
          {#if manages}
            <a class="btn-secondary" href="/flows/{encodeURIComponent(f.slug)}/edit" aria-label="Edit {f.name}">Edit</a>
            <a class="btn-secondary" href="/flows/{encodeURIComponent(f.slug)}/poster" aria-label="Poster for {f.name}">Poster</a>
            <button type="button" class="btn-secondary" aria-label="Turn {f.name} {f.enabled ? 'off' : 'on'}" onclick={() => setEnabled(f, !f.enabled)}>
              Turn {f.enabled ? 'off' : 'on'}
            </button>
            <ConfirmButton
              label="Delete"
              question="Delete {f.name}? What it filed stays."
              onconfirm={() => act(() => request('DELETE', `/flows/${encodeURIComponent(f.slug)}`), `${f.name} deleted.`)}
            />
          {/if}
        </span>
      </li>
    {/each}
  </ul>
{/if}
