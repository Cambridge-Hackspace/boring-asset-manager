<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Report, ReportResult } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import ReportTables from '../../components/ReportTables.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const id = untrack(() => params.id ?? '');
  const path = `/reports/${encodeURIComponent(id)}`;

  let report = $state<Report | null>(null);
  let result = $state<ReportResult | null>(null);
  let ranAt = $state('');
  let error = $state('');
  let loading = $state(true);

  async function run() {
    error = '';
    try {
      const r = await request<{ report: Report; result: ReportResult }>('GET', `${path}/run`);
      [report, result, ranAt] = [r.report, r.result, new Date().toLocaleString()];
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(run);
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm print:hidden"><a class="underline" href="/reports">Reports</a></nav>
<ErrorMessage {error} />
{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Running the report…</p>
{:else if report && result}
  <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
    <h1 class="text-2xl font-bold">{report.name}</h1>
    <div class="flex flex-wrap gap-2 print:hidden">
      <button type="button" class="btn-secondary" onclick={run}>Run again</button>
      <button type="button" class="btn-secondary" onclick={() => window.print()}>Print</button>
      {#if session.can('reports:manage')}<a class="btn-secondary" href="{path}/edit">Edit</a>{/if}
    </div>
  </div>
  <p class="mb-2 text-xs text-zinc-600 dark:text-zinc-400">As of {ranAt}.</p>
  <ReportTables {result} csvHref={(n) => `/api/v1${path}/sections/${n}/csv`} />
{:else}
  <p class="text-sm">
    {#if session.can('reports:manage')}<a class="underline" href="{path}/edit">Edit the report</a> to fix it.{/if}
  </p>
{/if}
