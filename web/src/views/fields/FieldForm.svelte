<script lang="ts">
  import { untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { fieldTypes, type Field, type FieldType, type SelectChoice } from '../../lib/models';
  import { valueFrom } from '../../lib/keys';
  import DerivedKey from '../../components/DerivedKey.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // Creates a field, or edits one when field is given. The key cannot change.
  let { field, onsaved, oncancel }: { field?: Field; onsaved: (f: Field) => void; oncancel: () => void } = $props();

  const typeLabels: Record<FieldType, string> = {
    text: 'Text',
    number: 'Number',
    date: 'Date',
    url: 'Web address',
    select: 'Choice from a list',
  };

  // Copied from the field once: the form edits its own state.
  const initial = untrack(() => $state.snapshot(field));
  let key = $state(initial?.key ?? '');
  let label = $state(initial?.label ?? '');
  let type = $state<FieldType>(initial?.validation_type ?? 'text');
  // Each choice by a number of its own, so removing one leaves the others
  // as they were. A choice's value is made from its label (#171), but only
  // a new one's: the values assets already hold never change by themselves.
  type Choice = SelectChoice & { n: number; stored: boolean };
  let nextChoice = 0;
  const choice = (c: SelectChoice, stored: boolean): Choice => ({ ...c, n: nextChoice++, stored });
  let choices = $state<Choice[]>(initial?.options?.choices.map((c) => choice(c, true)) ?? [choice({ label: '', value: '' }, false)]);
  // Values two choices share, which the server refuses.
  const shared = $derived([...new Set(choices.map((c) => c.value.trim()).filter((v, i, all) => v && all.indexOf(v) !== i))]);
  let defaultChoice = $state(initial?.options?.default ?? '');
  let error = $state('');
  let busy = $state(false);

  const idPrefix = `field-${initial?.id ?? 'new'}`;

  // Suggests a key from the label while creating, until the key is edited.
  let keyEdited = $state(Boolean(initial));
  $effect(() => {
    if (!keyEdited) key = label.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '');
  });

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    const options =
      type === 'select'
        ? { choices: choices.filter((c) => c.label.trim() || c.value.trim()).map((c) => ({ label: c.label, value: c.value })), default: defaultChoice || null }
        : undefined;
    try {
      const body = { label, validation_type: type, options };
      const res = initial
        ? await request<{ field: Field }>('PUT', `/fields/${encodeURIComponent(initial.id)}`, body)
        : await request<{ field: Field }>('POST', '/fields', { ...body, key });
      onsaved(res.field);
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<form class="card space-y-4" onsubmit={save}>
  <h2 class="text-lg font-semibold">{initial ? `Edit “${initial.label}”` : 'New field'}</h2>
  <div class="grid gap-4 sm:grid-cols-3">
    <div>
      <label class="label" for="{idPrefix}-label">Label</label>
      <input id="{idPrefix}-label" class="input" required bind:value={label} />
    </div>
    <div>
      <label class="label" for="{idPrefix}-key">Key</label>
      <input
        id="{idPrefix}-key"
        class="input font-mono"
        required
        pattern="[a-z0-9_]+"
        disabled={Boolean(initial)}
        aria-describedby="{idPrefix}-key-help"
        bind:value={key}
        oninput={() => (keyEdited = true)}
      />
      <p id="{idPrefix}-key-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        {initial ? 'Keys cannot change once created.' : 'Lowercase letters, digits and underscores. Used in label bindings.'}
      </p>
    </div>
    <div>
      <label class="label" for="{idPrefix}-type">Type</label>
      <select id="{idPrefix}-type" class="input" bind:value={type}>
        {#each fieldTypes as t (t)}
          <option value={t}>{typeLabels[t]}</option>
        {/each}
      </select>
    </div>
  </div>

  {#if type === 'select'}
    <fieldset class="space-y-2">
      <legend class="label">Choices</legend>
      {#each choices as c, i (c.n)}
        <div class="flex flex-wrap items-start gap-2">
          <div class="flex-1">
            <label class="sr-only" for="{idPrefix}-choice-{c.n}-label">Choice {i + 1}</label>
            <input id="{idPrefix}-choice-{c.n}-label" class="input" placeholder="Label" bind:value={c.label} />
            <DerivedKey
              id="{idPrefix}-choice-{c.n}-value"
              bind:value={c.value}
              from={c.label}
              follows={!c.stored}
              derive={valueFrom}
              what="value of choice {i + 1}"
              lead="Stored as"
              warning={c.stored ? 'Assets that hold the old value keep it, and it will match no choice.' : ''}
              maxlength={255}
            />
          </div>
          <button type="button" class="btn-secondary" aria-label="Remove choice {i + 1}" onclick={() => choices.splice(i, 1)}>Remove</button>
        </div>
      {/each}
      {#each shared as v (v)}<p class="error-text text-sm">Two choices are stored as <span class="font-mono">{v}</span>: change one.</p>{/each}
      <button type="button" class="btn-secondary" onclick={() => choices.push(choice({ label: '', value: '' }, false))}>Add choice</button>
      <div class="max-w-xs">
        <label class="label" for="{idPrefix}-default">Default</label>
        <select id="{idPrefix}-default" class="input" bind:value={defaultChoice}>
          <option value="">None</option>
          {#each choices.filter((c) => c.value) as c (c.n)}
            <option value={c.value}>{c.label || c.value}</option>
          {/each}
        </select>
      </div>
    </fieldset>
  {/if}

  <ErrorMessage {error} />
  <div class="flex gap-2">
    <button type="submit" class="btn-primary" disabled={busy}>{initial ? 'Save' : 'Create field'}</button>
    <button type="button" class="btn-secondary" onclick={oncancel}>Cancel</button>
  </div>
</form>
