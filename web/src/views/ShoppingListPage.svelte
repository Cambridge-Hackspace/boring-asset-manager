<script lang="ts">
  // /shopping, the installed app's Shopping list shortcut
  // (docs/installing.md#shortcuts). Until the shopping list has a page of
  // its own (#214), it opens the Running low report.
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { Report } from '../lib/models';
  import { navigate } from '../lib/router.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  // The report every instance starts with (migration 017).
  const runningLow = '3a6c4e8b-0d2f-4b7a-9c1e-5f7a9b1d3e17';

  let error = $state('');
  let missing = $state(false);

  onMount(async () => {
    try {
      const { reports } = await request<{ reports: Report[] }>('GET', '/reports');
      const report = reports.find((r) => r.id === runningLow) ?? reports.find((r) => r.kind === 'report' && r.spec?.filter?.running_low);
      if (report) navigate(`/reports/${encodeURIComponent(report.id)}`, true);
      else missing = true;
    } catch (err) {
      error = messageOf(err);
    }
  });
</script>

<h1 class="mb-6 text-3xl font-bold">Shopping list</h1>
<ErrorMessage {error} />
{#if missing}
  <p>
    No report lists what is running low. <a class="inline-flex min-h-11 items-center underline" href="/reports/new">Make one</a> with only assets running low.
  </p>
{:else if !error}
  <p class="text-sm text-zinc-600 dark:text-zinc-400" role="status">Opening the Running low report…</p>
{/if}
