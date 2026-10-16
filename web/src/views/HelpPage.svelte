<script lang="ts">
  import { untrack } from 'svelte';
  import { helpTopic, helpTopics } from '../lib/help';
  import { renderMarkdown } from '../lib/markdown';

  // /help and /help/:topic: the user help (README §6.5).
  let { params }: { params: Record<string, string> } = $props();
  const slug = untrack(() => params.topic ?? '');
  const topic = slug ? helpTopic(slug) : undefined;
</script>

<div class="flex flex-col gap-8 md:flex-row">
  <nav aria-label="Help topics" class="md:w-56 md:shrink-0">
    <p class="mb-2 text-sm font-semibold text-zinc-600 dark:text-zinc-400">Help</p>
    <ul class="space-y-1 text-sm">
      {#each helpTopics as t (t.slug)}
        <li>
          <a class="hover:underline aria-[current=page]:font-semibold" href="/help/{t.slug}" aria-current={t.slug === slug ? 'page' : undefined}>{t.title}</a>
        </li>
      {/each}
    </ul>
  </nav>
  <article class="help max-w-prose">
    {#if slug && !topic}
      <h1 class="mb-4 text-2xl font-bold">No such help</h1>
      <p class="text-sm">Choose a topic from the list.</p>
    {:else}
      {@const shown = topic ?? helpTopic('start')!}
      <h1 class="mb-4 text-2xl font-bold">{shown.title}</h1>
      <!-- Our own help, rendered with everything escaped (lib/markdown.ts). -->
      <!-- eslint-disable-next-line svelte/no-at-html-tags -->
      {@html renderMarkdown(shown.body)}
    {/if}
  </article>
</div>

<style>
  .help :global(h2) {
    margin: 1.5rem 0 0.5rem;
    font-size: 1.125rem;
    font-weight: 600;
  }
  .help :global(p),
  .help :global(ul),
  .help :global(ol) {
    margin-bottom: 0.75rem;
    font-size: 0.875rem;
    line-height: 1.5;
  }
  .help :global(ul) {
    list-style: disc;
    padding-left: 1.25rem;
  }
  .help :global(ol) {
    list-style: decimal;
    padding-left: 1.25rem;
  }
  .help :global(code) {
    font-family: ui-monospace, monospace;
    font-size: 0.8125rem;
  }
</style>
