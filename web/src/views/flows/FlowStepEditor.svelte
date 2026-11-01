<script lang="ts">
  // The settings of one step of a flow (README §6.7), as its card in the
  // editor shows them.
  import type { Category, Field } from '../../lib/models';
  import type { Change, ChangeKind, FieldSetting, Flow, Question, Step } from '../../lib/flows';
  import LocationPicker from '../../components/LocationPicker.svelte';

  let {
    prefix,
    step = $bindable(),
    categories,
    fields,
    flows,
    destinations,
    templates,
    hooks,
  }: {
    /** Unique on the page, for the ids of its controls. */
    prefix: string;
    step: Step;
    /** The flow's categories. */
    categories: Category[];
    /** The fields an asset in any of the flow's categories records. */
    fields: Field[];
    flows: Flow[];
    destinations: { value: string; label: string }[];
    templates: { id: string; name: string }[];
    hooks: { key: string; label: string }[];
  } = $props();

  const p = $derived(prefix);
  const changeKinds: [ChangeKind, string][] = [
    ['archive', 'Archive it'],
    ['condition', 'Set its condition'],
    ['borrow', 'Lend it to whoever runs the flow'],
    ['return', 'Return it'],
    ['place', 'Move it to a place'],
    ['category', 'Move it to another category'],
    ['fields', 'Set fields'],
    ['hook', 'Trigger a hook'],
  ];

  // Form: a field's setting, made when first changed.
  function settingOf(key: string): FieldSetting {
    return step.fields?.find((f) => f.key === key) ?? { key };
  }
  function setField(key: string, change: Partial<FieldSetting>) {
    const list = [...(step.fields ?? [])];
    const i = list.findIndex((f) => f.key === key);
    const next = { ...settingOf(key), ...change };
    if (i < 0) list.push(next);
    else list[i] = next;
    step.fields = list;
  }
  // The fields in the order asked: those set first, as set, then the rest.
  const ordered = $derived([
    ...(step.fields ?? []).map((s) => fields.find((f) => f.key === s.key)).filter((f): f is Field => !!f),
    ...fields.filter((f) => !(step.fields ?? []).some((s) => s.key === f.key)),
  ]);
  function moveField(key: string, by: -1 | 1) {
    const keys = ordered.map((f) => f.key);
    const i = keys.indexOf(key);
    const j = i + by;
    if (j < 0 || j >= keys.length) return;
    [keys[i], keys[j]] = [keys[j]!, keys[i]!];
    step.fields = keys.map((k) => settingOf(k));
  }

  function setQuestion(i: number, change: Partial<Question>) {
    const list = [...(step.questions ?? [])];
    list[i] = { ...list[i]!, ...change };
    step.questions = list;
  }

  function setChange(i: number, change: Partial<Change>) {
    const list = [...(step.changes ?? [])];
    list[i] = { ...list[i]!, ...change };
    step.changes = list;
  }

  function toggleFindBy(by: 'scan' | 'search' | 'list', on: boolean) {
    const list = new Set(step.find_by ?? []);
    if (on) list.add(by);
    else list.delete(by);
    step.find_by = (['scan', 'search', 'list'] as const).filter((b) => list.has(b));
  }

  function toggleOffered(id: string, on: boolean) {
    const list = new Set(step.categories ?? []);
    if (on) list.add(id);
    else list.delete(id);
    step.categories = categories.map((c) => c.id).filter((c) => list.has(c));
  }

  // Only when: a condition on an earlier value.
  function setWhen(on: boolean) {
    step.when = on ? { value: 'category_id', op: 'is', to: categories[0]?.id ?? '' } : null;
  }
</script>

<div class="space-y-3 text-sm">
  <div class="grid gap-3 sm:grid-cols-2">
    <div>
      <label class="label" for="{p}-id">Name</label>
      <input id="{p}-id" class="input font-mono" maxlength="32" bind:value={step.id} aria-describedby="{p}-id-help" />
      <p id="{p}-id-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">How conditions and the server name it.</p>
    </div>
    {#if step.kind !== 'label' && step.kind !== 'change'}
      <div>
        <label class="label" for="{p}-title">Heading</label>
        <input id="{p}-title" class="input" maxlength="255" bind:value={step.title} />
      </div>
    {/if}
  </div>
  {#if step.kind === 'confirm' || step.kind === 'done' || step.kind === 'form' || step.kind === 'find'}
    <div>
      <label class="label" for="{p}-text">Text</label>
      <input id="{p}-text" class="input" maxlength="255" placeholder={step.kind === 'done' ? '{asset} is filed.' : ''} bind:value={step.text} aria-describedby="{p}-text-help" />
      <p id="{p}-text-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">May name earlier answers, as {'{owner}'} or {'{asset}'}.</p>
    </div>
  {/if}

  {#if step.kind === 'choose_category'}
    <fieldset>
      <legend class="label">Offer</legend>
      {#each categories as c (c.id)}
        <label class="mr-4 inline-flex items-center gap-2">
          <input type="checkbox" checked={!step.categories?.length || step.categories.includes(c.id)} onchange={(e) => toggleOffered(c.id, e.currentTarget.checked)} />
          {c.name}
        </label>
      {/each}
    </fieldset>
    <div>
      <label class="label" for="{p}-fixed">Or always</label>
      <select id="{p}-fixed" class="input" bind:value={step.fixed}>
        <option value="">— let them choose —</option>
        {#each categories as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
      </select>
    </div>
  {:else if step.kind === 'form'}
    <div class="flex flex-wrap gap-4">
      <label class="inline-flex items-center gap-2"><input type="checkbox" bind:checked={step.ask_name} /> Ask for its name</label>
      <label class="inline-flex items-center gap-2"><input type="checkbox" bind:checked={step.ask_photo} /> Ask for a photo</label>
    </div>
    {#if step.ask_name}
      <div>
        <label class="label" for="{p}-name-label">The name's question</label>
        <input id="{p}-name-label" class="input" maxlength="255" placeholder="What is it?" bind:value={step.name_label} />
      </div>
    {/if}
    {#if ordered.length}
      <fieldset class="space-y-2">
        <legend class="label">Fields</legend>
        <p class="text-xs text-zinc-600 dark:text-zinc-400">
          Each category asks the fields it records; a field it requires is always asked. Adding a field to a category adds it here.
        </p>
        {#each ordered as f, i (f.key)}
          {@const s = settingOf(f.key)}
          <div class="rounded border border-zinc-200 p-2 dark:border-zinc-800" role="group" aria-label="Field {f.label}">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span class="font-medium">{f.label} <span class="font-mono text-xs text-zinc-600 dark:text-zinc-400">{f.key}</span></span>
              <span class="flex gap-1">
                <button type="button" class="btn-sm" aria-label="Move {f.label} up" disabled={i === 0} onclick={() => moveField(f.key, -1)}>↑</button>
                <button type="button" class="btn-sm" aria-label="Move {f.label} down" disabled={i === ordered.length - 1} onclick={() => moveField(f.key, 1)}>↓</button>
              </span>
            </div>
            <div class="mt-2 flex flex-wrap gap-4">
              <label class="inline-flex items-center gap-2"><input type="checkbox" checked={!s.hidden} onchange={(e) => setField(f.key, { hidden: !e.currentTarget.checked })} /> Ask it</label>
              <label class="inline-flex items-center gap-2"><input type="checkbox" checked={!!s.required} onchange={(e) => setField(f.key, { required: e.currentTarget.checked })} /> Required</label>
            </div>
            {#if !s.hidden}
              <div class="mt-2 grid gap-2 sm:grid-cols-3">
                <input class="input" aria-label="Label for {f.label}" placeholder={f.label} value={s.label ?? ''} oninput={(e) => setField(f.key, { label: e.currentTarget.value })} />
                <input class="input" aria-label="Help for {f.label}" placeholder="Help" value={s.help ?? ''} oninput={(e) => setField(f.key, { help: e.currentTarget.value })} />
                <input class="input" aria-label="Default for {f.label}" placeholder="Default, as {'{user}'}" value={s.default ?? ''} oninput={(e) => setField(f.key, { default: e.currentTarget.value })} />
              </div>
            {/if}
          </div>
        {/each}
      </fieldset>
    {/if}
    <fieldset class="space-y-2">
      <legend class="label">Questions of its own</legend>
      <p class="text-xs text-zinc-600 dark:text-zinc-400">Recorded in no field; used in names and text, as {'{what}'}.</p>
      {#each step.questions ?? [] as q, i (i)}
        <div class="grid gap-2 sm:grid-cols-[8rem_1fr_7rem_auto_auto]" role="group" aria-label="Question {i + 1}">
          <input class="input font-mono" aria-label="Key of question {i + 1}" placeholder="what" value={q.key} oninput={(e) => setQuestion(i, { key: e.currentTarget.value })} />
          <input class="input" aria-label="Question {i + 1}" placeholder="What is it?" value={q.label} oninput={(e) => setQuestion(i, { label: e.currentTarget.value })} />
          <select class="input" aria-label="Type of question {i + 1}" value={q.type || 'text'} onchange={(e) => setQuestion(i, { type: e.currentTarget.value as Question['type'] })}>
            <option value="text">Text</option>
            <option value="number">Number</option>
            <option value="date">Date</option>
          </select>
          <label class="inline-flex items-center gap-1"><input type="checkbox" checked={!!q.required} onchange={(e) => setQuestion(i, { required: e.currentTarget.checked })} /> Required</label>
          <button type="button" class="btn-sm" aria-label="Remove question {i + 1}" onclick={() => (step.questions = (step.questions ?? []).filter((_, j) => j !== i))}>Remove</button>
        </div>
      {/each}
      <button type="button" class="btn-secondary" onclick={() => (step.questions = [...(step.questions ?? []), { key: '', label: '' }])}>Add a question</button>
    </fieldset>
  {:else if step.kind === 'find'}
    <fieldset>
      <legend class="label">Find it by</legend>
      {#each [['scan', 'Scanning its code'], ['search', 'Search'], ['list', 'A list to pick from']] as [by, label] (by)}
        <label class="mr-4 inline-flex items-center gap-2">
          <input type="checkbox" checked={step.find_by?.includes(by as 'scan')} onchange={(e) => toggleFindBy(by as 'scan', e.currentTarget.checked)} />
          {label}
        </label>
      {/each}
    </fieldset>
    <div class="grid gap-3 sm:grid-cols-2">
      <div>
        <label class="label" for="{p}-filed">Only what a flow filed</label>
        <select id="{p}-filed" class="input" bind:value={step.filed_by}>
          <option value="">— anything in its categories —</option>
          {#each flows as f (f.id)}<option value={f.id}>{f.name}</option>{/each}
        </select>
      </div>
      <div>
        <label class="label" for="{p}-match">Only where a field is</label>
        <div class="flex gap-2">
          <select
            id="{p}-match"
            class="input"
            value={step.match?.field ?? ''}
            onchange={(e) => (step.match = e.currentTarget.value ? { field: e.currentTarget.value, value: step.match?.value ?? '{user}' } : null)}
          >
            <option value="">—</option>
            {#each fields as f (f.key)}<option value={f.key}>{f.label}</option>{/each}
          </select>
          {#if step.match}
            <input class="input" aria-label="The value it matches" placeholder={'{user}'} value={step.match.value} oninput={(e) => (step.match = { field: step.match!.field, value: e.currentTarget.value })} />
          {/if}
        </div>
      </div>
    </div>
  {:else if step.kind === 'create'}
    <div>
      <label class="label" for="{p}-name">Its name</label>
      <input id="{p}-name" class="input" maxlength="255" placeholder={'{owner}: {what}'} bind:value={step.name} />
    </div>
    <div>
      <label class="label" for="{p}-where">Put it</label>
      <select
        id="{p}-where"
        class="input"
        value={step.location?.mode ?? ''}
        onchange={(e) => (step.location = e.currentTarget.value ? { mode: e.currentTarget.value as 'fixed' | 'scan' } : null)}
      >
        <option value="">Nowhere in particular</option>
        <option value="fixed">In one place</option>
        <option value="scan">Where they scan</option>
      </select>
    </div>
    {#if step.location?.mode === 'fixed'}
      <LocationPicker id="{p}-place" value={step.location.asset_id ? { id: step.location.asset_id, name: 'The place chosen' } : null} onchange={(place) => (step.location = { mode: 'fixed', asset_id: place?.id })} />
    {/if}
  {:else if step.kind === 'label'}
    <div class="grid gap-3 sm:grid-cols-3">
      <div>
        <label class="label" for="{p}-mode">Its label</label>
        <select id="{p}-mode" class="input" value={step.label?.mode ?? 'show'} onchange={(e) => (step.label = { ...(step.label ?? {}), mode: e.currentTarget.value as 'show' })}>
          <option value="show">Show it on screen</option>
          <option value="queue">Add it to the print queue</option>
          <option value="print">Print it</option>
        </select>
      </div>
      {#if step.label?.mode !== 'show'}
        <div>
          <label class="label" for="{p}-dest">On</label>
          <select id="{p}-dest" class="input" value={step.label?.destination ?? ''} onchange={(e) => (step.label = { mode: 'show', ...step.label, destination: e.currentTarget.value })}>
            <option value="">{step.label?.mode === 'queue' ? 'Where its template suits' : 'Choose…'}</option>
            {#each destinations as d (d.value)}<option value={d.value}>{d.label}</option>{/each}
          </select>
        </div>
      {/if}
      <div>
        <label class="label" for="{p}-template">Template</label>
        <select id="{p}-template" class="input" value={step.label?.template_id ?? ''} onchange={(e) => (step.label = { mode: 'show', ...step.label, template_id: e.currentTarget.value })}>
          <option value="">Its category's</option>
          {#each templates as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
        </select>
      </div>
    </div>
  {:else if step.kind === 'change'}
    <fieldset class="space-y-2">
      <legend class="label">In order</legend>
      {#each step.changes ?? [] as c, i (i)}
        <div class="flex flex-wrap items-center gap-2" role="group" aria-label="Change {i + 1}">
          <select class="input w-auto" aria-label="Change {i + 1}" value={c.kind} onchange={(e) => setChange(i, { kind: e.currentTarget.value as ChangeKind })}>
            {#each changeKinds as [kind, label] (kind)}<option value={kind}>{label}</option>{/each}
          </select>
          {#if c.kind === 'condition'}
            <select class="input w-auto" aria-label="Condition for change {i + 1}" value={c.condition ?? ''} onchange={(e) => setChange(i, { condition: e.currentTarget.value })}>
              <option value="">Choose…</option>
              <option value="ok">Working</option>
              <option value="needs_maintenance">Needs maintenance</option>
              <option value="out_of_service">Out of service</option>
            </select>
            <input class="input w-auto" aria-label="Note for change {i + 1}" placeholder="Note" value={c.note ?? ''} oninput={(e) => setChange(i, { note: e.currentTarget.value })} />
          {:else if c.kind === 'category'}
            <select class="input w-auto" aria-label="Category for change {i + 1}" value={c.category_id ?? ''} onchange={(e) => setChange(i, { category_id: e.currentTarget.value })}>
              <option value="">Choose…</option>
              {#each categories as cat (cat.id)}<option value={cat.id}>{cat.name}</option>{/each}
            </select>
          {:else if c.kind === 'hook'}
            <select class="input w-auto" aria-label="Hook for change {i + 1}" value={c.hook ?? ''} onchange={(e) => setChange(i, { hook: e.currentTarget.value })}>
              <option value="">Choose…</option>
              {#each hooks as a (a.key)}<option value={a.key}>{a.label}</option>{/each}
            </select>
          {:else if c.kind === 'fields'}
            {#each fields as f (f.key)}
              <input
                class="input w-auto"
                aria-label="{f.label}, for change {i + 1}"
                placeholder={f.label}
                value={c.fields?.[f.key] ?? ''}
                oninput={(e) => {
                  const next = { ...(c.fields ?? {}) };
                  if (e.currentTarget.value) next[f.key] = e.currentTarget.value;
                  else delete next[f.key];
                  setChange(i, { fields: next });
                }}
              />
            {/each}
          {/if}
          <button type="button" class="btn-sm" aria-label="Remove change {i + 1}" onclick={() => (step.changes = (step.changes ?? []).filter((_, j) => j !== i))}>Remove</button>
          {#if c.kind === 'place'}
            <div class="w-full">
              <LocationPicker id="{p}-change-{i}" value={c.asset_id ? { id: c.asset_id, name: 'The place chosen' } : null} onchange={(place) => setChange(i, { asset_id: place?.id })} />
            </div>
          {/if}
        </div>
      {/each}
      <button type="button" class="btn-secondary" onclick={() => (step.changes = [...(step.changes ?? []), { kind: 'archive' }])}>Add a change</button>
    </fieldset>
  {/if}

  {#if step.kind !== 'done'}
    <div class="flex flex-wrap items-center gap-2">
      <label class="inline-flex items-center gap-2"><input type="checkbox" checked={!!step.when} onchange={(e) => setWhen(e.currentTarget.checked)} /> Only when</label>
      {#if step.when}
        <input class="input w-36 font-mono" aria-label="The value it depends on" value={step.when.value} oninput={(e) => (step.when = { ...step.when!, value: e.currentTarget.value })} />
        <select class="input w-auto" aria-label="How" value={step.when.op} onchange={(e) => (step.when = { ...step.when!, op: e.currentTarget.value as 'is' })}>
          <option value="is">is</option>
          <option value="is_not">is not</option>
          <option value="set">is given</option>
          <option value="unset">is not given</option>
        </select>
        {#if step.when.op === 'is' || step.when.op === 'is_not'}
          {#if step.when.value === 'category_id'}
            <select class="input w-auto" aria-label="The category" value={step.when.to ?? ''} onchange={(e) => (step.when = { ...step.when!, to: e.currentTarget.value })}>
              {#each categories as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
            </select>
          {:else}
            <input class="input w-36" aria-label="The value" value={step.when.to ?? ''} oninput={(e) => (step.when = { ...step.when!, to: e.currentTarget.value })} />
          {/if}
        {/if}
      {/if}
    </div>
  {/if}
</div>
