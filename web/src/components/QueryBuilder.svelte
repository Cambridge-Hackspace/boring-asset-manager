<script lang="ts">
  // Writes a search query (docs/data-model.md#queries) from conditions
  // picked from lists, and shows the query it writes, so that people who
  // use it learn to type one.
  import type { Category, Field } from '../lib/models';
  import { buildQuery, opsFor, queryKeys, type Condition } from '../lib/query';

  let { fields, categories, signedIn, onuse }: { fields: Field[]; categories: Category[]; signedIn: boolean; onuse: (query: string) => void } = $props();

  const keys = $derived(queryKeys(fields, categories, signedIn));
  const sortable = $derived(keys.filter((k) => k.kind !== 'bool' && k.key !== 'loan'));

  let conditions = $state<Condition[]>([{ key: 'name', op: '~', value: '' }]);
  let all = $state(true);
  let sort = $state('');
  let descending = $state(false);

  const query = $derived(buildQuery(conditions, all, sort, descending));

  function keyOf(c: Condition) {
    return keys.find((k) => k.key === c.key);
  }

  // A new key starts with its first comparison, and no value.
  function changeKey(c: Condition, key: string) {
    c.key = key;
    const k = keyOf(c);
    c.op = (k && opsFor(k)[0]?.op) || '=';
    c.value = k?.choices?.[0]?.[0] ?? '';
  }

  function placeholder(c: Condition): string {
    switch (keyOf(c)?.kind) {
      case 'date':
        return '2026-03-01, today or -30d';
      case 'number':
        return 'A number';
      case 'person':
        return 'A name, or me';
    }
    return '';
  }
</script>

<section class="card mb-4 space-y-3" aria-labelledby="query-builder-title">
  <h2 id="query-builder-title" class="text-lg font-semibold">Build a query</h2>
  <fieldset class="flex flex-wrap items-center gap-2">
    <legend class="sr-only">Match</legend>
    <span>Find assets matching</span>
    <label class="flex items-center gap-1"><input type="radio" name="query-join" value={true} bind:group={all} /> all</label>
    <label class="flex items-center gap-1"><input type="radio" name="query-join" value={false} bind:group={all} /> any</label>
    <span>of these:</span>
  </fieldset>
  <ol class="space-y-2">
    {#each conditions as c, i (i)}
      {@const k = keyOf(c)}
      <li class="flex flex-wrap items-center gap-2">
        <select class="input w-auto" aria-label={`Condition ${i + 1}: what to search by`} value={c.key} onchange={(e) => changeKey(c, e.currentTarget.value)}>
          {#each keys as key (key.key)}
            <option value={key.key}>{key.label}</option>
          {/each}
        </select>
        {#if k}
          <select class="input w-auto" aria-label={`Condition ${i + 1}: how to compare`} bind:value={c.op}>
            {#each opsFor(k) as op (op.op)}
              <option value={op.op}>{op.label}</option>
            {/each}
          </select>
          {#if !opsFor(k).find((o) => o.op === c.op)?.noValue}
            {#if k.choices}
              <select class="input w-auto" aria-label={`Condition ${i + 1}: value`} bind:value={c.value}>
                {#each k.choices as [value, label] (value)}
                  <option {value}>{label}</option>
                {/each}
              </select>
            {:else}
              <input class="input w-48" aria-label={`Condition ${i + 1}: value`} placeholder={placeholder(c)} bind:value={c.value} />
            {/if}
          {/if}
        {/if}
        {#if conditions.length > 1}
          <button type="button" class="btn-secondary" aria-label={`Remove condition ${i + 1}`} onclick={() => conditions.splice(i, 1)}>Remove</button>
        {/if}
      </li>
    {/each}
  </ol>
  <button type="button" class="btn-secondary" onclick={() => conditions.push({ key: 'name', op: '~', value: '' })}>Add a condition</button>
  <div class="flex flex-wrap items-end gap-2">
    <div>
      <label class="label" for="query-sort">Sort by</label>
      <select id="query-sort" class="input w-auto" bind:value={sort}>
        <option value="">Newest first</option>
        {#each sortable as key (key.key)}
          <option value={key.key}>{key.label}</option>
        {/each}
      </select>
    </div>
    {#if sort}
      <label class="flex items-center gap-1 pb-2"><input type="checkbox" bind:checked={descending} /> Highest or latest first</label>
    {/if}
  </div>
  <div>
    <p class="label">Query</p>
    <code class="block whitespace-pre-wrap break-all rounded bg-gray-100 px-2 py-1 dark:bg-gray-800" data-testid="built-query">{query || ' '}</code>
  </div>
  <button type="button" class="btn-primary" disabled={!query} onclick={() => onuse(query)}>Search with this query</button>
</section>
