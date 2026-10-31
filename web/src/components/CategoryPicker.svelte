<script lang="ts">
  // Chooses categories from a tree of any size (#159): a field of chosen
  // ones, each a chip that removes it, and a panel that opens one level at
  // a time, with search across them all. It chooses categories and no more:
  // what a choice means for those beneath it is the page's to say.
  import { tick } from 'svelte';
  import type { Category } from '../lib/models';
  import { childCounts, childrenOf, pathTo, searchCategories } from '../lib/picker';

  let {
    categories,
    value = $bindable([]),
    label,
    id,
    onchange,
  }: { categories: Category[]; value?: string[]; label: string; id: string; onchange?: (value: string[]) => void } = $props();

  let open = $state(false);
  let level = $state<string | null>(null);
  let query = $state('');
  let list: HTMLUListElement | undefined = $state();

  const counts = $derived(childCounts(categories));
  const here = $derived(pathTo(categories, level));
  const levelCategory = $derived(here.at(-1));
  const rows = $derived(
    query.trim()
      ? searchCategories(categories, query).map((r) => ({ category: r.category, above: r.above.join(' / ') }))
      : childrenOf(categories, level).map((c) => ({ category: c, above: '' })),
  );
  const chosen = $derived(
    value.map((v) => categories.find((c) => c.id === v)).filter((c): c is Category => !!c).map((c) => ({ category: c, above: pathTo(categories, c.id).slice(0, -1).map((p) => p.name).join(' / ') })),
  );

  function toggle(cid: string, on: boolean): void {
    value = on ? (value.includes(cid) ? value : [...value, cid]) : value.filter((v) => v !== cid);
    onchange?.(value);
  }

  async function goTo(to: string | null): Promise<void> {
    level = to;
    query = '';
    await tick();
    list?.querySelector<HTMLInputElement>('input[type="checkbox"]')?.focus();
  }

  // Arrows move through the rows; → opens a category, ← goes back up.
  function onkeydown(e: KeyboardEvent): void {
    if (!list) return;
    const boxes = [...list.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')];
    const at = boxes.indexOf(document.activeElement as HTMLInputElement);
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      boxes[Math.max(0, Math.min(boxes.length - 1, at + (e.key === 'ArrowDown' ? 1 : -1)))]?.focus();
    } else if (e.key === 'ArrowRight' && at >= 0) {
      const into = boxes[at]!.dataset.opens;
      if (into) {
        e.preventDefault();
        void goTo(into);
      }
    } else if (e.key === 'ArrowLeft' && level && !query) {
      e.preventDefault();
      void goTo(levelCategory?.parent_id ?? null);
    }
  }
</script>

<div role="group" aria-labelledby="{id}-label">
  <span id="{id}-label" class="label">{label}</span>
  <div class="flex min-h-11 flex-wrap items-center gap-2 rounded border-2 border-zinc-900 bg-white px-2 py-1 dark:border-zinc-400 dark:bg-zinc-900">
    {#each chosen as c (c.category.id)}
      <button
        type="button"
        class="inline-flex min-h-11 cursor-pointer items-center gap-2 rounded border-[1.5px] border-zinc-900 px-2 text-sm hover:bg-zinc-100 dark:border-zinc-300 dark:hover:bg-zinc-800"
        aria-label="Remove {c.category.name}"
        onclick={() => toggle(c.category.id, false)}
      >
        <span>{#if c.above}<span class="text-zinc-600 dark:text-zinc-400">{c.above}{' / '}</span>{/if}<span class="font-semibold">{c.category.name}</span></span>
        <span aria-hidden="true">×</span>
      </button>
    {:else}
      <span class="px-1 text-sm text-zinc-600 dark:text-zinc-400">None chosen</span>
    {/each}
    <button
      type="button"
      class="ml-auto inline-flex min-h-11 cursor-pointer items-center px-2 text-sm font-semibold underline underline-offset-4"
      aria-expanded={open}
      aria-controls="{id}-panel"
      onclick={() => (open = !open)}
    >
      Choose categories
    </button>
  </div>

  {#if open}
    <div id="{id}-panel" class="mt-2 max-w-xl rounded border-2 border-zinc-900 bg-white dark:border-zinc-400 dark:bg-zinc-900">
      <div class="space-y-2 border-b border-zinc-300 p-3 dark:border-zinc-700">
        <label class="sr-only" for="{id}-find">Find a category</label>
        <input id="{id}-find" class="input" placeholder="Find a category by name" autocomplete="off" bind:value={query} />
        {#if !query.trim()}
          <nav aria-label="Where you are" class="flex flex-wrap items-center gap-x-1 text-sm">
            {#if level}
              <button type="button" class="inline-flex min-h-11 cursor-pointer items-center underline underline-offset-4" onclick={() => goTo(null)}>All categories</button>
              {#each here.slice(0, -1) as step (step.id)}
                <span aria-hidden="true" class="text-zinc-600 dark:text-zinc-400">/</span>
                <button type="button" class="inline-flex min-h-11 cursor-pointer items-center underline underline-offset-4" onclick={() => goTo(step.id)}>{step.name}</button>
              {/each}
              <span aria-hidden="true" class="text-zinc-600 dark:text-zinc-400">/</span>
              <span class="font-semibold" aria-current="location">{levelCategory?.name}</span>
            {:else}
              <span class="inline-flex min-h-11 items-center font-semibold" aria-current="location">All categories</span>
            {/if}
          </nav>
        {/if}
      </div>
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <ul class="max-h-72 overflow-y-auto" bind:this={list} {onkeydown}>
        {#if level && levelCategory && !query.trim()}
          <li class="border-b border-zinc-200 dark:border-zinc-800">
            <label class="flex min-h-11 cursor-pointer items-center gap-3 px-3 text-sm">
              <input type="checkbox" class="h-5 w-5 accent-zinc-900 dark:accent-zinc-100" checked={value.includes(levelCategory.id)} onchange={(e) => toggle(levelCategory.id, e.currentTarget.checked)} />
              <span class="font-semibold">{levelCategory.name} itself</span>
            </label>
          </li>
        {/if}
        {#each rows as r (r.category.id)}
          {@const n = counts.get(r.category.id) ?? 0}
          <li class="flex items-stretch border-b border-zinc-200 last:border-b-0 dark:border-zinc-800 {value.includes(r.category.id) ? 'bg-highlight/30' : ''}">
            <label class="flex min-h-11 flex-1 cursor-pointer items-center gap-3 px-3 text-sm">
              <input
                type="checkbox"
                class="h-5 w-5 accent-zinc-900 dark:accent-zinc-100"
                data-opens={n && !query.trim() ? r.category.id : undefined}
                checked={value.includes(r.category.id)}
                onchange={(e) => toggle(r.category.id, e.currentTarget.checked)}
              />
              <span>{#if r.above}<span class="text-zinc-600 dark:text-zinc-400">{r.above}{' / '}</span>{/if}{r.category.name}</span>
            </label>
            {#if n && !query.trim()}
              <button
                type="button"
                class="inline-flex min-h-11 min-w-11 cursor-pointer items-center justify-center gap-1 border-l border-zinc-200 px-3 text-sm text-zinc-600 hover:bg-zinc-100 dark:border-zinc-800 dark:text-zinc-400 dark:hover:bg-zinc-800"
                aria-label="Open {r.category.name}, {n} {n === 1 ? 'category' : 'categories'} under it"
                title="Open"
                onclick={() => goTo(r.category.id)}
              >
                {n} <span aria-hidden="true" class="text-lg text-zinc-900 dark:text-zinc-100">›</span>
              </button>
            {/if}
          </li>
        {:else}
          <li class="px-3 py-3 text-sm text-zinc-600 dark:text-zinc-400">{query.trim() ? 'No category has that in its name.' : 'No categories here.'}</li>
        {/each}
      </ul>
      <div class="flex items-center justify-between gap-3 border-t-2 border-zinc-900 bg-zinc-100 px-3 py-1 dark:border-zinc-400 dark:bg-zinc-800">
        <span class="text-sm" role="status">{value.length} chosen</span>
        <button type="button" class="btn-secondary" onclick={() => (open = false)}>Done</button>
      </div>
    </div>
  {/if}
</div>
