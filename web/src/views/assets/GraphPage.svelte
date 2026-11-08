<!--
  The map of a category or a search (docs/data-model.md#following-links):
  every asset in it, with the followed links among them, drawn and listed.
  It takes the asset list's filters, ?category= and ?search=.
-->
<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import type { Category } from '../../lib/models';
  import { router } from '../../lib/router.svelte';
  import GraphView from '../../components/GraphView.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  const categoryId = untrack(() => router.query.get('category') ?? '');
  const search = untrack(() => router.query.get('search') ?? '');
  let category = $state<Category | null>(null);

  onMount(async () => {
    if (!categoryId) return;
    try {
      category = (await request<{ category: Category }>('GET', `/categories/${encodeURIComponent(categoryId)}`)).category;
    } catch {
      // Named as the category it is, once read; the graph says if it is not there.
    }
  });

  const of = $derived(
    [categoryId ? (category?.name ?? 'a category') : 'every asset', search ? `matching “${search}”` : ''].filter(Boolean).join(', '),
  );
  const back = $derived(categoryId ? `/categories/${encodeURIComponent(categoryId)}` : `/assets${search ? `?search=${encodeURIComponent(search)}` : ''}`);
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm">
  <a class="underline" href={back}>{categoryId ? (category?.name ?? 'Category') : 'Assets'}</a>
</nav>
<h1 class="mb-2 text-2xl font-bold">Graph of {of}</h1>
<p class="mb-4 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
  Every asset in it, with the links between them that are followed through several hops, such as <em>depends on</em>. Open one to see all it
  needs, and all that needs it, wherever they are.
</p>
<GraphView query={{ category_id: categoryId, search }} />
