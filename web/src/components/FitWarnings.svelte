<script lang="ts">
  // What to know before printing, such as part of a template where the
  // printer does not print (README §4.4). Warnings never stop printing.
  let { warnings, label = 'Before printing' }: { warnings: string[] | undefined; label?: string } = $props();
</script>

{#if warnings?.length}
  <ul class="space-y-1 text-sm text-amber-900 dark:text-amber-200" aria-label={label}>
    {#each warnings as w (w)}
      <li><span aria-hidden="true">⚠ </span>{w}</li>
    {/each}
  </ul>
{/if}
