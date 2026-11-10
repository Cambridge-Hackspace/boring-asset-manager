<script lang="ts">
  // A new release's shell, fetched in the background, waits until the
  // person chooses to reload (docs/installing.md#updates): it never takes
  // over a page they are in the middle of.
  import { installedApp } from '../lib/install/app.svelte';
</script>

{#if installedApp.waiting}
  <div
    role="status"
    class="fixed inset-x-4 bottom-4 z-50 mx-auto flex max-w-md flex-wrap items-center justify-between gap-3 rounded border-2 border-zinc-900 bg-zinc-50 px-4 py-2 shadow-hard print:hidden dark:border-zinc-400 dark:bg-zinc-900"
  >
    <span class="text-sm">A new version of BAM! is ready.</span>
    <button type="button" class="btn-secondary" onclick={() => installedApp.update()}>Reload to update</button>
  </div>
{/if}
