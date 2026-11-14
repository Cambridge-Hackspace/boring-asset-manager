<script lang="ts">
  // A Search flow's settings (docs/flows.md#search-search): what its page
  // says, the filter every search keeps to, what its results show, how
  // long it waits unused before clearing, and whether signed-out visitors
  // may open it, as on a kiosk.
  import type { Step } from '../../lib/flows';
  import { maxResetAfter, minResetAfter } from '../../lib/flowEditor';
  import { resultColumns } from '../../components/AssetResults.svelte';

  let { step = $bindable(), public: open = $bindable(false) }: { step: Step; public?: boolean } = $props();

  const columns = $derived(step.columns?.length ? step.columns : ['where', 'categories']);

  function toggle(column: (typeof resultColumns)[number]['value'], on: boolean) {
    const next = resultColumns.map((c) => c.value).filter((c) => (c === column ? on : columns.includes(c)));
    step.columns = next as Step['columns'];
  }
</script>

<section class="card space-y-3" aria-labelledby="flow-search">
  <h2 id="flow-search" class="text-lg font-semibold">The search</h2>
  <div>
    <label class="label" for="search-title">Heading</label>
    <input id="search-title" class="input" maxlength="255" placeholder="Find something" bind:value={step.title} />
  </div>
  <div>
    <label class="label" for="search-text">What to tell people</label>
    <input id="search-text" class="input" maxlength="1000" placeholder="Type what you are looking for: a name, a code, or what it is." bind:value={step.text} />
  </div>
  <div>
    <label class="label" for="search-filter">Always keep to</label>
    <input id="search-filter" class="input font-mono" maxlength="1000" placeholder="condition = ok" aria-describedby="search-filter-help" bind:value={step.filter} />
    <p id="search-filter-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
      Plain words or a query, as the asset list takes them, joined to whatever is searched for. Leave it empty to search everything in the flow’s categories.
      <a class="underline" href="/help/assets">How queries work</a>
    </p>
  </div>
  <fieldset>
    <legend class="label">Results show, beside each name</legend>
    <div class="flex flex-wrap gap-x-4">
      {#each resultColumns as c (c.value)}
        <label class="inline-flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" checked={columns.includes(c.value)} onchange={(e) => toggle(c.value, e.currentTarget.checked)} /> {c.label}</label>
      {/each}
    </div>
  </fieldset>
  <div>
    <label class="label" for="search-reset">Clears after this many seconds unused</label>
    <input id="search-reset" class="input w-32" type="number" min="0" max={maxResetAfter} aria-describedby="search-reset-help" bind:value={step.reset_after} />
    <p id="search-reset-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
      For a kiosk: the search empties and the page starts over, for the next person. {minResetAfter} to {maxResetAfter}, or 0 to never clear.
    </p>
  </div>
  <label class="inline-flex items-start gap-2 text-sm">
    <input type="checkbox" class="mt-1" bind:checked={open} />
    <span>
      Open to signed-out visitors, as on a kiosk
      <span class="block text-xs text-zinc-600 dark:text-zinc-400">They search as the public role, and find only what it may read in these categories. People signed in search with their own permissions.</span>
    </span>
  </label>
</section>
