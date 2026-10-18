<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Report } from '../../lib/models';
  import { navigate } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let all = $state<Report[]>([]);
  const reports = $derived(all.filter((r) => r.kind !== 'scan'));
  // Scan templates (README §6.4): what scanning an asset shows.
  const scanTemplates = $derived(all.filter((r) => r.kind === 'scan'));
  let loading = $state(true);
  let error = $state('');
  let status = $state('');
  const canManage = $derived(session.can('reports.manage'));

  async function load() {
    try {
      all = (await request<{ reports: Report[] }>('GET', '/reports')).reports ?? [];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  // A copy to change, leaving the original as it is.
  async function duplicate(r: Report) {
    error = status = '';
    try {
      const res = await request<{ report: Report }>('POST', '/reports', { name: `${r.name} (copy)`, spec: r.spec });
      navigate(`/reports/${encodeURIComponent(res.report.id)}/edit`);
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function makeDefault(r: Report) {
    error = status = '';
    try {
      await request('POST', `/reports/${encodeURIComponent(r.id)}/default`);
      status = `${r.name} is now the default scan template.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(r: Report) {
    error = status = '';
    try {
      await request('DELETE', `/reports/${encodeURIComponent(r.id)}`);
      status = `Deleted ${r.name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Reports</h1>
  {#if canManage}<a class="btn-primary" href="/reports/new">New report</a>{/if}
</div>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading reports…</p>
{:else if reports.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No reports yet.</p>
{:else}
  <ul class="space-y-2">
    {#each reports as r (r.id)}
      <li class="card flex flex-wrap items-center justify-between gap-2">
        <a class="font-medium hover:underline" href="/reports/{encodeURIComponent(r.id)}">{r.name}</a>
        {#if canManage}
          <span class="inline-flex flex-wrap gap-2">
            <a class="btn-secondary" href="/reports/{encodeURIComponent(r.id)}/edit" aria-label="Edit {r.name}">Edit</a>
            <button type="button" class="btn-secondary" aria-label="Copy {r.name}" onclick={() => duplicate(r)}>Copy</button>
            <ConfirmButton label="Delete" question="Delete {r.name}?" onconfirm={() => remove(r)} />
          </span>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

{#if !loading && canManage}
  <section class="mt-8" aria-labelledby="scan-templates-heading">
    <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
      <h2 id="scan-templates-heading" class="text-xl font-semibold">Scan templates</h2>
      {#if canManage}<a class="btn-secondary" href="/scan-templates/new">New scan template</a>{/if}
    </div>
    <p class="mb-3 max-w-prose text-sm text-zinc-600 dark:text-zinc-400">
      What scanning an asset’s code shows. Each category can choose one for its assets and subcategories; the rest show
      the default.
    </p>
    <ul class="space-y-2">
      {#each scanTemplates as r (r.id)}
        <li class="card flex flex-wrap items-center justify-between gap-2">
          <span class="font-medium">{r.name}{#if r.default}<span class="ml-2 text-sm font-normal text-zinc-600 dark:text-zinc-400">(default)</span>{/if}</span>
          {#if canManage}
            <span class="inline-flex flex-wrap gap-2">
              <a class="btn-secondary" href="/scan-templates/{encodeURIComponent(r.id)}/edit" aria-label="Edit {r.name}">Edit</a>
              {#if !r.default}
                <button type="button" class="btn-secondary" aria-label="Make {r.name} the default" onclick={() => makeDefault(r)}>Make default</button>
                <ConfirmButton label="Delete" question="Delete {r.name}?" onconfirm={() => remove(r)} />
              {/if}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
  </section>
{/if}
