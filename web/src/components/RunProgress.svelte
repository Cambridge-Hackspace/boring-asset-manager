<script lang="ts">
  // A flow run's name and how far along it is (#166): "Take filament ·
  // step 2 of 3", with a bar of one segment a step, those reached filled.
  // The words say it; the bar, for the eye, is hidden from screen readers.
  // Nothing to count with one step, so then only the name.
  import type { Progress } from '../lib/flows';

  let { flowName, progress }: { flowName: string; progress?: Progress | null } = $props();
  const counted = $derived(progress && progress.of > 1 ? progress : null);
</script>

<div class="space-y-2">
  <p class="font-mono text-sm tracking-wide text-zinc-600 uppercase dark:text-zinc-400">
    {flowName}{#if counted}{' · '}Step {counted.step} of {counted.of}{/if}
  </p>
  {#if counted}
    <div class="flex gap-1.5" aria-hidden="true" data-progress-bar>
      {#each Array.from({ length: counted.of }, (_, i) => i) as i (i)}
        <span class="h-2 flex-1 rounded-sm {i < counted.step ? 'bg-zinc-900 dark:bg-zinc-100' : 'bg-zinc-300 dark:bg-zinc-700'}"></span>
      {/each}
    </div>
  {/if}
</div>
