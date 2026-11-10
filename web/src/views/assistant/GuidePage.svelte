<!--
  /g/:slug: a guide (docs/assistant.md#guides), as its poster's code opens
  it: what it is for and the questions it asks, and a way to start it with
  the assistant beside the page.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { messageOf } from '../../lib/errors';
  import { assistantApi, panel, type Guide } from '../../lib/assistant.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const slug = $derived(params.slug ?? '');

  let guide = $state<Guide | null>(null);
  let error = $state('');
  let busy = $state(false);

  onMount(async () => {
    try {
      guide = await assistantApi.guide(slug);
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function begin() {
    error = '';
    busy = true;
    try {
      const c = await assistantApi.start(slug);
      panel.setPage(null);
      panel.show(c.id);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<PageHeader title={guide?.name ?? 'Guide'}>
  {#snippet actions()}
    {#if guide && panel.available}<button type="button" class="btn-primary" disabled={busy} onclick={begin}>Start</button>{/if}
  {/snippet}
</PageHeader>
<ErrorMessage {error} />
{#if guide}
  <div class="max-w-2xl space-y-4">
    <p class="whitespace-pre-wrap">{guide.purpose}</p>
    {#if !panel.available}
      <p class="notice text-sm">The assistant is not available to you here: an admin turns it on under Admin, Assistant.</p>
    {/if}
    {#if guide.questions.length}
      <section aria-labelledby="guide-questions-heading">
        <h2 id="guide-questions-heading" class="section-title mb-2">What it asks</h2>
        <ol class="list-decimal space-y-1 pl-6">
          {#each guide.questions as q, i (i)}<li>{q}</li>{/each}
        </ol>
      </section>
    {/if}
    {#if guide.categories.length}
      <p class="text-sm text-zinc-600 dark:text-zinc-400">Works with {guide.categories.map((c) => c.name).join(', ')}.</p>
    {/if}
  </div>
{/if}
