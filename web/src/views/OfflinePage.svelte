<script lang="ts">
  // What an installed BAM! shows when it opens with no connection
  // (docs/installing.md#offline), rather than an error: its shell is kept,
  // but what it shows comes from the server.
  let { onretry }: { onretry: () => void } = $props();
</script>

<main class="mx-auto max-w-md space-y-4 p-8">
  <h1 class="text-2xl font-bold">You're offline</h1>
  <p>BAM! cannot reach its server from here. It opens as soon as there is a connection again.</p>
  <button type="button" class="btn-primary" onclick={onretry}>Try again</button>
</main>
