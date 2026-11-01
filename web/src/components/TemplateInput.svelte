<script lang="ts">
  import { tick } from 'svelte';
  import { suggest, unknownNames, type AvailableValue } from '../lib/flowEditor';

  // A box that takes a template, such as {owner}: {what} (#169), as a code
  // editor's autocomplete: typing { lists the values it can name there,
  // narrowing as you type; the arrow keys move, Enter or Tab inserts the
  // one chosen at the cursor, Escape closes. The { } button beside it lists
  // them all, to insert one where the cursor was. A name it cannot use
  // there is marked under it, before saving.
  let {
    id,
    value = $bindable(),
    values,
    name,
    labelled = false,
    placeholder = '',
    maxlength = 255,
    list,
    class: cls = '',
    describedby,
  }: {
    /** The id of its box. */
    id: string;
    value: string | undefined;
    /** The values it can name there. */
    values: AvailableValue[];
    /** What it is called, as its label says: "Heading", "Value of field 1". */
    name: string;
    /** Whether a <label for={id}> names it; else it is named by name. */
    labelled?: boolean;
    placeholder?: string;
    maxlength?: number;
    /** A datalist of fixed values to offer too. */
    list?: string;
    class?: string;
    describedby?: string;
  } = $props();

  let box = $state<HTMLInputElement | null>(null);
  let open = $state(false);
  let typed = $state('');
  let active = $state(0);
  // What inserting replaces: the { typed and the name after it, or, from
  // the button, the cursor alone.
  let from = 0;
  let to = 0;
  // Where the cursor was when the box lost focus, for the button.
  let caret: [number, number] = [0, 0];

  const found = $derived(open ? suggest(values, typed) : []);
  const unknown = $derived(unknownNames(value, values));
  const listId = $derived(`${id}-values`);
  const flagId = $derived(`${id}-unknown`);
  const describe = $derived([describedby, unknown.length ? flagId : ''].filter(Boolean).join(' ') || undefined);

  /** Opens or closes the list as the text before the cursor has an unclosed {name. */
  function follow() {
    const el = box;
    if (!el) return;
    const at = el.selectionStart ?? el.value.length;
    const m = /\{([a-z0-9_]*)$/.exec(el.value.slice(0, at));
    if (!m) {
      open = false;
      return;
    }
    from = at - m[0].length;
    // Within a name already closed, as {own|er}, the whole of it.
    const rest = /^[a-z0-9_]*\}?/.exec(el.value.slice(at))![0];
    to = at + rest.length;
    if (!open || typed !== m[1]) active = 0;
    typed = m[1]!;
    open = true;
  }

  async function insert(v: AvailableValue | undefined) {
    if (!v) return;
    const now = value ?? '';
    const text = `{${v.key}}`;
    value = now.slice(0, from) + text + now.slice(to);
    open = false;
    await tick();
    box?.focus();
    box?.setSelectionRange(from + text.length, from + text.length);
  }

  function keydown(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (found.length) active = (active + (e.key === 'ArrowDown' ? 1 : found.length - 1)) % found.length;
      void tick().then(() => document.getElementById(`${id}-value-${active}`)?.scrollIntoView?.({ block: 'nearest' }));
    } else if ((e.key === 'Enter' || e.key === 'Tab') && found.length) {
      e.preventDefault();
      void insert(found[active]);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      open = false;
    } else if (e.key === 'Enter') {
      // Nothing to insert: Enter does not submit the form either.
      e.preventDefault();
    }
  }

  // The button: every value, to insert where the cursor was.
  async function browse() {
    if (open) {
      open = false;
      return;
    }
    // Pressed with the box focused, the cursor is where it is now.
    if (box && document.activeElement === box) caret = [box.selectionStart ?? 0, box.selectionEnd ?? 0];
    [from, to] = caret;
    typed = '';
    active = 0;
    open = true;
    await tick();
    box?.focus();
    box?.setSelectionRange(from, to);
  }
</script>

<div class="relative {cls}">
  <div class="flex gap-1">
    <input
      bind:this={box}
      {id}
      class="input min-w-0 flex-1"
      {maxlength}
      {placeholder}
      {list}
      role="combobox"
      aria-label={labelled ? undefined : name}
      aria-expanded={open}
      aria-controls={listId}
      aria-autocomplete="list"
      aria-activedescendant={open && found.length ? `${id}-value-${active}` : undefined}
      aria-describedby={describe}
      aria-invalid={unknown.length ? true : undefined}
      autocomplete="off"
      spellcheck="false"
      bind:value
      oninput={follow}
      onkeydown={keydown}
      onclick={follow}
      onkeyup={(e) => (e.key === 'ArrowLeft' || e.key === 'ArrowRight' || e.key === 'Home' || e.key === 'End') && follow()}
      onblur={(e) => {
        caret = [e.currentTarget.selectionStart ?? 0, e.currentTarget.selectionEnd ?? 0];
        open = false;
      }}
    />
    <button
      type="button"
      class="btn-secondary shrink-0 px-0 font-mono"
      aria-label="Insert a value into {name}"
      title="Insert a value"
      aria-expanded={open}
      aria-controls={listId}
      onpointerdown={(e) => e.preventDefault()}
      onclick={browse}>{'{ }'}</button
    >
  </div>
  <ul
    id={listId}
    role="listbox"
    aria-label="Values {name} can use"
    class="absolute inset-x-0 top-full z-30 mt-1 max-h-72 overflow-y-auto rounded border-2 border-zinc-900 bg-white py-1 text-sm shadow-lg dark:border-zinc-400 dark:bg-zinc-900"
    hidden={!open || !found.length}
  >
    {#each found as v, i (v.key)}
      <li
        id="{id}-value-{i}"
        role="option"
        aria-selected={i === active}
        class="cursor-pointer px-3 py-2 aria-selected:bg-zinc-800 aria-selected:text-white dark:aria-selected:bg-zinc-200 dark:aria-selected:text-zinc-900"
        onpointerenter={() => (active = i)}
        onpointerdown={(e) => {
          e.preventDefault();
          void insert(v);
        }}
      >
        <span class="flex flex-wrap items-baseline justify-between gap-x-3">
          <span><code class="font-mono font-semibold">{`{${v.key}}`}</code> {v.label}</span>
          <span class="text-xs opacity-80">{v.from}</span>
        </span>
        {#if v.note}<span class="block text-xs opacity-80">{v.note}</span>{/if}
      </li>
    {/each}
  </ul>
  {#if open && !found.length}
    <p
      class="absolute inset-x-0 top-full z-30 mt-1 rounded border-2 border-zinc-900 bg-white px-3 py-2 text-sm text-zinc-600 shadow-lg dark:border-zinc-400 dark:bg-zinc-900 dark:text-zinc-400"
    >
      No value by that name is set before this step.
    </p>
  {/if}
  {#if unknown.length}
    <p id={flagId} class="mt-1 text-xs text-red-700 dark:text-red-400">
      {#each unknown as n, i (n)}{i ? ' ' : ''}{`{${n}}`} is not set before this step.{/each}
    </p>
  {/if}
</div>
