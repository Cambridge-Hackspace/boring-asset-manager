<script lang="ts">
  // A page that needs the server, opened from the offline copy with no
  // connection (docs/installing.md#offline): what works offline instead.
  import { ago } from '../lib/offline/copy';
  import { offline } from '../lib/offline/offline.svelte';

  const link = 'inline-flex min-h-11 items-center underline';
</script>

<h1 class="mb-4 text-3xl font-bold">You're offline</h1>
<div class="max-w-xl space-y-3">
  <p>This page needs a connection to BAM!'s server. It opens again as soon as there is one.</p>
  {#if offline.copy}
    <p>
      Until then, this app has a copy of what you can see, from {ago(offline.copy.made_at)}. You can
      <a class={link} href="/scan">scan a label</a>, <a class={link} href="/offline">find something</a> or open the
      <a class={link} href="/shopping">shopping list</a>.
    </p>
  {/if}
</div>
