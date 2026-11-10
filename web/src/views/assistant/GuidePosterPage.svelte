<script lang="ts">
  // /assistant/guides/:slug/poster: a page to print and put up where a
  // guide is used, with its name, its QR code and its purpose
  // (docs/assistant.md#guides), as flows have.
  import { onMount } from 'svelte';
  import { messageOf } from '../../lib/errors';
  import { assistantApi, type Guide } from '../../lib/assistant.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const slug = $derived(params.slug ?? '');

  let guide = $state<Guide | null>(null);
  let error = $state('');
  const address = $derived(`${location.origin}/g/${encodeURIComponent(slug)}`);

  onMount(async () => {
    try {
      guide = await assistantApi.guide(slug);
    } catch (err) {
      error = messageOf(err);
    }
  });
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2 print:hidden">
  <a class="inline-flex min-h-11 items-center text-sm underline" href="/assistant-settings">Back to the assistant</a>
  <button type="button" class="btn-primary" onclick={() => window.print()}>Print</button>
</div>
<ErrorMessage {error} />

{#if guide}
  <article class="mx-auto max-w-xl space-y-6 rounded-lg border border-zinc-300 bg-white p-8 text-center text-zinc-900 print:border-0">
    <h1 class="text-4xl font-bold">{guide.name}</h1>
    <img class="mx-auto w-72 max-w-full" src="/api/v1/assistant/guides/{encodeURIComponent(guide.slug)}/qr.svg" alt="QR code opening {guide.name}" />
    <p class="text-2xl">Scan to start, with the assistant.</p>
    <p class="font-mono text-sm break-all">{address}</p>
  </article>
{/if}
