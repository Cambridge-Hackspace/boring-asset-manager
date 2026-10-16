<script lang="ts">
  import { conditionLabel } from '../lib/assets';
  import type { Condition } from '../lib/models';

  // An asset's condition when it is not working; nothing when it is.
  let { condition }: { condition: Condition } = $props();
</script>

{#if condition === 'out_of_service'}
  <span class="rounded bg-red-100 px-1.5 py-0.5 text-xs font-medium text-red-900 dark:bg-red-900 dark:text-red-100">{conditionLabel(condition)}</span>
{:else if condition === 'needs_maintenance'}
  <span class="rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-900 dark:bg-amber-900 dark:text-amber-100">{conditionLabel(condition)}</span>
{/if}
