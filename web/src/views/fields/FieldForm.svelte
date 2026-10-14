<script lang="ts">
  import { untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { fieldTypes, type Field, type FieldType, type SelectChoice } from '../../lib/models';
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
  let choices = $state<SelectChoice[]>(initial?.options?.choices.map((c) => ({ ...c })) ?? [{ label: '', value: '' }]);
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
        ? { choices: choices.filter((c) => c.label.trim() || c.value.trim()), default: defaultChoice || null }
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
      <p id="{idPrefix}-key-help" class="mt-1 text-xs text-zinc-500">
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
      {#each choices as choice, i (i)}
        <div class="flex flex-wrap items-end gap-2">
          <div class="flex-1">
            <label class="sr-only" for="{idPrefix}-choice-{i}-label">Choice {i + 1} label</label>
            <input id="{idPrefix}-choice-{i}-label" class="input" placeholder="Label" bind:value={choice.label} />
          </div>
          <div class="flex-1">
            <label class="sr-only" for="{idPrefix}-choice-{i}-value">Choice {i + 1} value</label>
            <input id="{idPrefix}-choice-{i}-value" class="input font-mono" placeholder="Value" bind:value={choice.value} />
          </div>
          <button type="button" class="btn-secondary" aria-label="Remove choice {i + 1}" onclick={() => choices.splice(i, 1)}>Remove</button>
        </div>
      {/each}
      <button type="button" class="btn-secondary" onclick={() => choices.push({ label: '', value: '' })}>Add choice</button>
      <div class="max-w-xs">
        <label class="label" for="{idPrefix}-default">Default</label>
        <select id="{idPrefix}-default" class="input" bind:value={defaultChoice}>
          <option value="">None</option>
          {#each choices.filter((c) => c.value) as c (c.value)}
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
