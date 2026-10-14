<script lang="ts">
  let { error }: { error: string } = $props();
</script>

{#if error}
  <p class="rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-800 dark:bg-red-950 dark:text-red-200" role="alert">
    {error}
  </p>
{/if}
