<script lang="ts">
  // /f/:slug: a flow's page (docs/flows.md). A flow that only searches is a
  // search page (#397), which signed-out visitors may open where the flow
  // is public; any other runs one step at a time.
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { searchStepOf, type Flow } from '../../lib/flows';
  import { session } from '../../lib/session.svelte';
  import FlowRunPage from './FlowRunPage.svelte';
  import FlowSearchPage from './FlowSearchPage.svelte';

  let { params }: { params: Record<string, string> } = $props();

  let kind = $state<'' | 'run' | 'search'>('');

  onMount(async () => {
    // Signed out, or without flows to see, only a search page can open;
    // it says so where this one isn't.
    if (!session.user || !session.canAny('flows.run', 'flows.manage')) {
      kind = 'search';
      return;
    }
    try {
      const f = (await request<{ flow: Flow }>('GET', `/flows/${encodeURIComponent(params.slug ?? '')}`)).flow;
      kind = searchStepOf(f.definition) ? 'search' : 'run';
    } catch {
      // Running it says what is wrong, as it always has.
      kind = 'run';
    }
  });
</script>

{#if kind === 'search'}
  <FlowSearchPage {params} />
{:else if kind === 'run'}
  <FlowRunPage {params} />
{:else}
  <p class="text-sm text-zinc-600 dark:text-zinc-400" role="status">Loading…</p>
{/if}
