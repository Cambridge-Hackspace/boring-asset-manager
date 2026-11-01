<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { keyFrom } from '../lib/keys';

  // A key made from a label as it is typed, as field keys are (#171): shown
  // small under the label, as "Used as {what} · Change", rather than as a
  // box of its own. Change opens it to edit; once edited, it no longer
  // follows the label. A key something already depends on is not made from
  // the label at all (follows false), and says what a rename breaks.
  let {
    id,
    value = $bindable(),
    from,
    follows = true,
    derive = keyFrom,
    what,
    lead = 'Key:',
    format = (key: string) => key,
    warning = '',
    pattern,
    maxlength = 64,
  }: {
    /** The id of its box, once open. */
    id: string;
    value: string;
    /** The label it is made from. */
    from: string;
    /** Whether it starts out made from the label: true for something new. Read once. */
    follows?: boolean;
    derive?: (label: string) => string;
    /** What it is, in a sentence, such as "key of question 1": its box is named by it. */
    what: string;
    /** What comes before it, such as "Used as". */
    lead?: string;
    /** How it is shown, such as {what}. */
    format?: (key: string) => string;
    /** What a rename breaks, said once Change is pressed or the key differs from what it was. */
    warning?: string;
    pattern?: string;
    maxlength?: number;
  } = $props();

  const was = untrack(() => value);
  let following = $state(untrack(() => follows));
  let open = $state(false);
  let box = $state<HTMLInputElement | null>(null);

  $effect(() => {
    if (!following) return;
    const made = derive(from);
    if (made !== untrack(() => value)) value = made;
  });

  async function change() {
    open = true;
    await tick();
    box?.focus();
  }

  const named = $derived(what.charAt(0).toUpperCase() + what.slice(1));
  const warned = $derived(Boolean(warning) && (open || value !== was));
</script>

<div class="text-xs text-zinc-600 dark:text-zinc-400">
  {#if open}
    <label class="mt-1 block font-semibold" for={id}>{named}</label>
    <input
      {id}
      class="input font-mono"
      {pattern}
      {maxlength}
      aria-describedby={warned ? `${id}-warning` : undefined}
      bind:value
      bind:this={box}
      oninput={() => (following = false)}
    />
  {:else}
    <p class="flex flex-wrap items-center gap-x-1">
      <span>{lead} {#if value}<code class="font-mono text-zinc-900 dark:text-zinc-100">{format(value)}</code>{:else}<em>made from the label</em>{/if}</span>
      <span aria-hidden="true">·</span>
      <button type="button" class="inline-flex min-h-11 min-w-11 cursor-pointer items-center underline underline-offset-2 hover:text-zinc-900 dark:hover:text-zinc-100" aria-label="Change {what}" onclick={change}>Change</button>
    </p>
  {/if}
  {#if warned}<p id="{id}-warning" class="mt-1 text-amber-900 dark:text-amber-200" role="note">{warning}</p>{/if}
</div>
