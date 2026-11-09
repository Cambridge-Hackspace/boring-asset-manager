<script lang="ts">
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { matchPeople, namesSomeone, personIds, personLabel, personValue } from '../lib/fieldPeople';
  import type { FieldPerson } from '../lib/models';

  // Chooses people for a person field (docs/data-model.md#person-fields), as
  // assignee pickers elsewhere do: whom it names show as chips, and typing
  // part of a name offers the people the viewer may choose there, read once
  // from the server. A name matching no one can be added as a person
  // without an account, by those who may. The value holds user IDs.
  let {
    id,
    value = $bindable(),
    multiple = false,
    required = false,
    known = {},
    hidden = 0,
    categoryIds = [],
    assetId = '',
    invalid = false,
    describedBy,
  }: {
    /** The text input's ID, for its label. */
    id: string;
    /** The user IDs it names, separated by commas; none when unset. */
    value?: string;
    /** Whether it names several people, or one. */
    multiple?: boolean;
    required?: boolean;
    /** The people it may already name, by ID, to show them by. */
    known?: Record<string, FieldPerson>;
    /** How many people it names that the viewer may not see; they stay. */
    hidden?: number;
    /** Where the asset is, or will be: its categories, or the asset. */
    categoryIds?: string[];
    assetId?: string;
    invalid?: boolean;
    describedBy?: string;
  } = $props();

  let query = $state('');
  let open = $state(false);
  let active = $state(0);
  let error = $state('');
  let busy = $state(false);
  // The people that may be chosen, for the scope they were read for.
  let choices = $state<FieldPerson[] | null>(null);
  let canAdd = $state(false);
  let readFor = '';
  // Everyone seen, by ID, to show the chips by.
  let seen = $state<Record<string, FieldPerson>>({});

  const chosen = $derived(
    personIds(value).map((pid) => seen[pid] ?? known[pid] ?? choices?.find((p) => p.id === pid) ?? { id: pid, name: 'Unknown person' }),
  );
  const offered = $derived(choices ? matchPeople(choices, query).filter((p) => !personIds(value).includes(p.id)) : []);
  const offerAdd = $derived(canAdd && query.trim() !== '' && !namesSomeone(choices ?? [], query));
  const options = $derived([...offered.map((p) => ({ kind: 'person' as const, p })), ...(offerAdd ? [{ kind: 'add' as const, p: null }] : [])]);
  const listID = $derived(`${id}-choices`);
  const scope = $derived(categoryIds.length ? categoryIds.map((c) => `category_id=${encodeURIComponent(c)}`).join('&') : assetId ? `asset_id=${encodeURIComponent(assetId)}` : '');

  async function load() {
    if (readFor === scope && choices !== null) return;
    readFor = scope;
    try {
      const res = await request<{ people: FieldPerson[]; can_add: boolean }>('GET', `/people/choices${scope ? `?${scope}` : ''}`);
      choices = res.people ?? [];
      canAdd = res.can_add;
    } catch (err) {
      choices = [];
      error = messageOf(err);
    }
  }

  function choose(p: FieldPerson) {
    seen[p.id] = p;
    value = multiple ? personValue([...personIds(value), p.id]) : p.id;
    query = '';
    active = 0;
    open = multiple;
  }

  function remove(pid: string) {
    value = personValue(personIds(value).filter((x) => x !== pid));
  }

  async function add() {
    const name = query.trim();
    if (!name) return;
    busy = true;
    error = '';
    try {
      const res = await request<{ person: FieldPerson }>('POST', '/people', { name });
      choices = [...(choices ?? []), res.person];
      choose(res.person);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function pick(i: number) {
    const o = options[i];
    if (!o) return;
    if (o.kind === 'person') choose(o.p);
    else void add();
  }

  function keydown(event: KeyboardEvent) {
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        open = true;
        active = Math.min(active + 1, Math.max(options.length - 1, 0));
        break;
      case 'ArrowUp':
        event.preventDefault();
        active = Math.max(active - 1, 0);
        break;
      case 'Enter':
        if (open && options.length) {
          event.preventDefault();
          pick(active);
        }
        break;
      case 'Escape':
        if (open) {
          event.preventDefault();
          open = false;
        }
        break;
      case 'Backspace':
        if (query === '' && chosen.length) remove(chosen[chosen.length - 1]!.id);
        break;
    }
  }
</script>

<div class="relative" data-person-picker>
  <div class="input flex min-h-11 flex-wrap items-center gap-1 py-1" class:ring-red-600={invalid}>
    {#each chosen as p (p.id)}
      <span class="inline-flex items-center gap-1 rounded bg-zinc-100 px-2 py-0.5 text-sm dark:bg-zinc-800">
        {personLabel(p)}
        <button type="button" class="inline-flex min-h-6 min-w-6 items-center justify-center rounded hover:bg-zinc-200 dark:hover:bg-zinc-700" aria-label="Remove {p.name}" onclick={() => remove(p.id)}>×</button>
      </span>
    {/each}
    {#if hidden > 0}
      <span class="text-sm text-zinc-600 dark:text-zinc-400">{hidden === 1 ? 'and someone you cannot see' : `and ${hidden} people you cannot see`}</span>
    {/if}
    {#if multiple || chosen.length === 0}
      <input
        {id}
        class="min-w-32 flex-1 border-0 bg-transparent p-1 focus:outline-none focus:ring-0"
        role="combobox"
        autocomplete="off"
        aria-expanded={open && options.length > 0}
        aria-controls={listID}
        aria-autocomplete="list"
        aria-activedescendant={open && options.length ? `${listID}-${active}` : undefined}
        aria-invalid={invalid}
        aria-describedby={describedBy}
        required={required && chosen.length === 0 && hidden === 0}
        placeholder={chosen.length ? 'Add someone…' : 'Type a name…'}
        bind:value={query}
        onfocus={() => {
          open = true;
          void load();
        }}
        oninput={() => {
          open = true;
          active = 0;
          void load();
        }}
        onblur={() => setTimeout(() => (open = false), 150)}
        onkeydown={keydown}
      />
    {:else}
      <button type="button" {id} class="ml-auto inline-flex min-h-6 items-center text-sm underline" aria-describedby={describedBy} onclick={() => remove(chosen[0]!.id)}>Change</button>
    {/if}
  </div>
  {#if open && options.length}
    <ul id={listID} role="listbox" class="absolute z-20 mt-1 max-h-60 w-full overflow-auto rounded border border-zinc-300 bg-white shadow dark:border-zinc-700 dark:bg-zinc-900">
      {#each options as o, i (o.kind === 'person' ? o.p.id : 'add')}
        <li
          id="{listID}-{i}"
          role="option"
          aria-selected={i === active}
          tabindex="-1"
          class="cursor-pointer px-3 py-2 text-sm {i === active ? 'bg-zinc-100 dark:bg-zinc-800' : ''}"
          onmousedown={(e) => {
            e.preventDefault();
            pick(i);
          }}
        >
          {#if o.kind === 'person'}{personLabel(o.p)}{:else}{busy ? 'Adding…' : `Add “${query.trim()}” as a person without an account`}{/if}
        </li>
      {/each}
    </ul>
  {/if}
  {#if error}<p class="error-text mt-1 text-sm" role="alert">{error}</p>{/if}
</div>
