<script lang="ts">
  // The yellow triangle marking a scan template chosen by a tie between
  // categories as close as each other (README §6.4).
  let { label = 'Scan template chosen by a tie between categories' }: { label?: string } = $props();
</script>

<span class="text-amber-500" role="img" aria-label={label} title={label}>⚠</span>
