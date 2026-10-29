<script lang="ts">
  // /flows: the flows someone may run, and, for those who manage flows,
  // every flow, to make, change, turn off and on, and delete (README §6.7).
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

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Flows</h1>
  {#if manages}<a class="btn-primary" href="/flows/new">New flow</a>{/if}
</div>
<p class="mb-4 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
  Guided pages for everyday things, such as leaving something here or collecting it. Each has a poster with a QR code to start it from a phone.
</p>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
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

{#if manages && !loading}
  <section class="mt-8 max-w-3xl" aria-labelledby="manage-heading">
    <h2 id="manage-heading" class="mb-2 text-lg font-semibold">Every flow</h2>
    {#if flows.length === 0}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">None yet. <strong>New flow</strong> starts one from <em>Leave something</em> or <em>Collect it</em>.</p>
    {:else}
      <ul class="space-y-2">
        {#each flows as f (f.id)}
          <li class="card flex flex-wrap items-center justify-between gap-2">
            <div>
              <h3 class="font-semibold">{f.name}{#if !f.enabled}{' '}<span class="text-sm font-normal text-zinc-600 dark:text-zinc-400">(turned off)</span>{/if}</h3>
              <p class="font-mono text-xs text-zinc-600 dark:text-zinc-400">/f/{f.slug} · {f.definition.steps.length} steps</p>
              {#if overdue[f.id]?.length}
                <details class="mt-1 text-sm">
                  <summary class="text-amber-800 dark:text-amber-400">{overdue[f.id]!.length} past {overdue[f.id]!.length === 1 ? 'its' : 'their'} date</summary>
                  <ul class="mt-1 space-y-0.5">
                    {#each overdue[f.id]! as o (o.asset_id)}
                      <li><a class="underline" href="/assets/{o.asset_id}">{o.name}</a>, due {o.until}</li>
                    {/each}
                  </ul>
                </details>
              {/if}
            </div>
            <span class="flex flex-wrap gap-2">
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
            </span>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}
