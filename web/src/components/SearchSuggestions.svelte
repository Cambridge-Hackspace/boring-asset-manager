<script lang="ts">
  // Related terms to search for instead (docs/data-model.md#related-terms),
  // as the asset list and a flow's Search step both offer them.
  let { suggestions, onpick }: { suggestions: string[]; onpick: (term: string) => void } = $props();
</script>

{#if suggestions.length}
  <p class="mb-3 text-sm" data-testid="search-suggestions">
    Related:
    {#each suggestions as s, i (s)}{#if i > 0},{' '}{/if}<button type="button" class="underline" onclick={() => onpick(s)}>{s}</button>{/each}
  </p>
{/if}
