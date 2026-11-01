<script lang="ts">
  // The settings of one step of a flow (README §6.7), as its card in the
  // editor shows them.
  import { untrack } from 'svelte';
  import type { Category, Field } from '../../lib/models';
  import type { Change, ChangeKind, FieldValue, Flow, Option, Question, Step } from '../../lib/flows';
  import { optionLabel, questionTypes } from '../../lib/flows';
  import { kindLabels, setsOn, setsOnText, stepsUsing, valuesBefore, type AvailableValue } from '../../lib/flowEditor';
  import { keyFrom, valueFrom } from '../../lib/keys';
  import DerivedKey from '../../components/DerivedKey.svelte';
  import LocationPicker from '../../components/LocationPicker.svelte';
  import TemplateInput from '../../components/TemplateInput.svelte';

  let {
    prefix,
    step = $bindable(),
    steps = [],
    at = 0,
    categories,
    categoryIDs,
    fields,
    byCategory = [],
    flows,
    destinations,
    templates,
    hooks,
  }: {
    /** Unique on the page, for the ids of its controls. */
    prefix: string;
    step: Step;
    /** The flow's steps, this one among them at at: those after it may use its questions. */
    steps?: Step[];
    at?: number;
    /** The flow's categories. */
    categories: Category[];
    /** The flow's categories, by ID, as its definition lists them (those of categories when not given). */
    categoryIDs?: string[];
    /** The fields an asset in any of the flow's categories records. */
    fields: Field[];
    /** The fields each of the flow's categories records, for Set fields to choose from. */
    byCategory?: { category: Category; fields: Field[] }[];
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
    ['hook', 'Trigger a hook'],
  ];

  // Form: each question, by a number of its own, so that removing or moving
  // one leaves the others' keys as they were: those it came with are not
  // made from their text, as later steps may use them; those added here
  // are, until changed. Its choices are numbered the same way.
  const came = untrack(() => (step.questions ?? []).map((q) => q.key));
  const qids = $state(came.map((_, i) => i));
  let nextQ = came.length;
  const oids = $state<Record<number, number[]>>({});
  let nextO = 0;
  const cameOptions = new Set<number>();
  untrack(() =>
    (step.questions ?? []).forEach((q, i) => {
      oids[i] = (q.options ?? []).map(() => {
        cameOptions.add(nextO);
        return nextO++;
      });
    }),
  );

  function setQuestion(i: number, change: Partial<Question>) {
    const list = [...(step.questions ?? [])];
    list[i] = { ...list[i]!, ...change };
    step.questions = list;
  }
  function addQuestion() {
    qids.push(nextQ);
    oids[nextQ++] = [];
    step.questions = [...(step.questions ?? []), { key: '', label: '', type: 'text' }];
  }
  function removeQuestion(i: number) {
    qids.splice(i, 1);
    step.questions = (step.questions ?? []).filter((_, j) => j !== i);
  }
  function moveQuestion(i: number, by: -1 | 1) {
    const j = i + by;
    const list = [...(step.questions ?? [])];
    if (j < 0 || j >= list.length) return;
    [list[i], list[j]] = [list[j]!, list[i]!];
    [qids[i], qids[j]] = [qids[j]!, qids[i]!];
    step.questions = list;
  }
  function setType(i: number, type: Question['type']) {
    const q = step.questions![i]!;
    const change: Partial<Question> = { type };
    if (type === 'choice' && !q.options?.length) {
      change.options = [{ value: '', label: '' }];
      oids[qids[i]!] = [nextO++];
    }
    if (type !== 'choice') change.options = undefined;
    // A default that suited the old type may not suit the new.
    if (type === 'yes_no' || type === 'choice' || type === 'photo') change.default = '';
    setQuestion(i, change);
  }
  function setOption(i: number, j: number, change: Partial<Option>) {
    const options = [...(step.questions![i]!.options ?? [])];
    options[j] = { ...options[j]!, ...change };
    setQuestion(i, { options });
  }
  function addOption(i: number) {
    (oids[qids[i]!] ??= []).push(nextO++);
    setQuestion(i, { options: [...(step.questions![i]!.options ?? []), { value: '', label: '' }] });
  }
  function removeOption(i: number, j: number) {
    oids[qids[i]!]?.splice(j, 1);
    setQuestion(i, { options: (step.questions![i]!.options ?? []).filter((_, k) => k !== j) });
  }

  const asValue = (key: string) => `{${key}}`;
  // What renaming a question's key breaks: the later steps that use it.
  function renameWarning(key: string): string {
    const later = steps.slice(at + 1);
    const using = stepsUsing(later, key);
    if (!using.length) return '';
    const named = using.map((s) => `step ${at + 2 + later.indexOf(s)} (${kindLabels[s.kind]}${s.title ? `: ${s.title}` : ''})`);
    const list = named.length > 1 ? `${named.slice(0, -1).join(', ')} and ${named.at(-1)}` : named[0]!;
    const many = using.length > 1;
    return `${list.charAt(0).toUpperCase()}${list.slice(1)} ${many ? 'use' : 'uses'} {${key}}: change it there too, or ${many ? 'they' : 'it'} will name nothing.`;
  }

  // Set fields: its rows, the fields to choose from by category, and the
  // photo questions asked before it.
  const fieldOf = (key: string) => fields.find((f) => f.key === key);
  function setRow(i: number, change: Partial<FieldValue>) {
    const list = [...(step.fields ?? [])];
    list[i] = { ...list[i]!, ...change };
    step.fields = list;
  }
  function moveRow(i: number, by: -1 | 1) {
    const j = i + by;
    const list = [...(step.fields ?? [])];
    if (j < 0 || j >= list.length) return;
    [list[i], list[j]] = [list[j]!, list[i]!];
    step.fields = list;
  }
  // A choice field offers its choices as fixed values.
  function fixedValues(key: string): { value: string; label: string }[] {
    const f = fieldOf(key);
    if (f?.validation_type === 'select') return (f.options?.choices ?? []).map((c) => ({ value: c.value, label: c.label }));
    return [];
  }
  const photoQuestions = $derived(
    steps
      .slice(0, at)
      .flatMap((s) => (s.kind === 'form' ? (s.questions ?? []) : []))
      .filter((q) => q.type === 'photo'),
  );
  const placement = $derived(step.kind === 'set_fields' ? setsOnText[setsOn(steps, at)] : '');

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

  // The values this step can name, in its templates and its Only when: those
  // every run has, and those the steps before it set (#169).
  const categoryNames = $derived(Object.fromEntries(categories.map((c) => [c.id, c.name])));
  const available = $derived(valuesBefore({ category_ids: categoryIDs ?? categories.map((c) => c.id), steps }, at, fields, categoryNames));

  // Only when: a condition on a value set before it, compared with what
  // suits that value.
  const depends = $derived(step.when ? available.find((v) => v.key === step.when!.value) : undefined);
  function firstTo(v: AvailableValue | undefined): string {
    switch (v?.input.kind) {
      case 'category':
        return (v.key === 'category' ? categories[0]?.name : categories[0]?.id) ?? '';
      case 'choices':
        return v.input.options[0]?.value ?? '';
    }
    return '';
  }
  function setWhen(on: boolean) {
    if (!on) {
      step.when = null;
      return;
    }
    // The category, if chosen before it; else the latest value a step sets.
    const v = available.find((a) => a.key === 'category_id') ?? [...available].reverse().find((a) => a.from !== 'Every run') ?? available[0];
    step.when = { value: v?.key ?? 'category_id', op: 'is', to: firstTo(v) };
  }
  function setWhenValue(key: string) {
    step.when = { ...step.when!, value: key, to: firstTo(available.find((a) => a.key === key)) };
  }
  // What the value is compared with, as a list: its choices, and what it
  // is now when that is not among them.
  const comparedOptions = $derived.by((): { value: string; label: string }[] => {
    const v = depends;
    const list =
      v?.input.kind === 'category'
        ? categories.map((c) => ({ value: v.key === 'category' ? c.name : c.id, label: c.name }))
        : v?.input.kind === 'choices'
          ? v.input.options
          : [];
    const to = step.when?.to ?? '';
    return to && !list.some((o) => o.value === to) ? [{ value: to, label: to }, ...list] : list;
  });
</script>

<div class="space-y-3 text-sm">
  {#if step.kind !== 'label' && step.kind !== 'change' && step.kind !== 'set_fields'}
    <div class="sm:max-w-[50%]">
      <label class="label" for="{p}-title">Heading</label>
      <TemplateInput id="{p}-title" name="Heading" labelled values={available} bind:value={step.title} />
    </div>
  {/if}
  {#if step.kind === 'confirm' || step.kind === 'done' || step.kind === 'form' || step.kind === 'find'}
    <div>
      <label class="label" for="{p}-text">Text</label>
      <TemplateInput
        id="{p}-text"
        name="Text"
        labelled
        values={available}
        placeholder={step.kind === 'done' ? '{asset} is filed.' : ''}
        describedby="{p}-text-help"
        bind:value={step.text}
      />
      <p id="{p}-text-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        May name values set before it, as {'{owner}'} or {'{asset}'}: type {'{'} or press {'{ }'} to choose one.
      </p>
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
    <fieldset class="space-y-2">
      <legend class="label">Questions</legend>
      <p class="text-xs text-zinc-600 dark:text-zinc-400">
        Each answer is a value later steps use, as {'{what}'}: a Set fields step puts it into a field.
      </p>
      {#each step.questions ?? [] as q, i (qids[i] ?? -1 - i)}
        {@const qid = qids[i] ?? -1}
        {@const n = i + 1}
        <div class="space-y-2 rounded border border-zinc-200 p-2 dark:border-zinc-800" role="group" aria-label="Question {n}">
          <div class="grid items-start gap-2 sm:grid-cols-[1fr_9rem]">
            <div>
              <input class="input" aria-label="Question {n}" placeholder="What is it?" value={q.label} oninput={(e) => setQuestion(i, { label: e.currentTarget.value })} />
              <DerivedKey
                id="{p}-question-{qid}-key"
                bind:value={() => q.key, (key) => setQuestion(i, { key })}
                from={q.label}
                follows={qid >= came.length}
                derive={keyFrom}
                what="key of question {n}"
                lead="Used as"
                format={asValue}
                warning={renameWarning(came[qid] ?? '')}
                pattern="[a-z][a-z0-9_]*"
                maxlength={64}
              />
            </div>
            <select class="input" aria-label="Type of question {n}" value={q.type || 'text'} onchange={(e) => setType(i, e.currentTarget.value as Question['type'])}>
              {#each questionTypes as [t, name] (t)}<option value={t}>{name}</option>{/each}
            </select>
          </div>
          {#if q.type === 'choice'}
            <fieldset class="space-y-2 pl-2">
              <legend class="text-xs font-semibold">Its choices</legend>
              {#each q.options ?? [] as o, j (oids[qid]?.[j] ?? -1 - j)}
                {@const oid = oids[qid]?.[j] ?? -1}
                <div class="flex flex-wrap items-start gap-2">
                  <div class="flex-1">
                    <input class="input" aria-label="Choice {j + 1} of question {n}" placeholder="Oak" value={o.label ?? ''} oninput={(e) => setOption(i, j, { label: e.currentTarget.value })} />
                    <DerivedKey
                      id="{p}-question-{qid}-choice-{oid}"
                      bind:value={() => o.value, (value) => setOption(i, j, { value })}
                      from={o.label ?? ''}
                      follows={!cameOptions.has(oid)}
                      derive={valueFrom}
                      what="value of choice {j + 1} of question {n}"
                      lead="Answered as"
                      maxlength={255}
                    />
                  </div>
                  <button type="button" class="btn-secondary" aria-label="Remove choice {j + 1} of question {n}" onclick={() => removeOption(i, j)}>Remove</button>
                </div>
              {/each}
              <button type="button" class="btn-secondary" onclick={() => addOption(i)}>Add a choice</button>
            </fieldset>
          {/if}
          <div class="grid gap-2 sm:grid-cols-2">
            <input class="input" aria-label="Help for question {n}" placeholder="Help" value={q.help ?? ''} oninput={(e) => setQuestion(i, { help: e.currentTarget.value })} />
            {#if q.type === 'yes_no' || q.type === 'choice'}
              <select class="input" aria-label="Default for question {n}" value={q.default ?? ''} onchange={(e) => setQuestion(i, { default: e.currentTarget.value })}>
                <option value="">No default</option>
                {#if q.type === 'yes_no'}
                  <option value="yes">Yes</option>
                  <option value="no">No</option>
                {:else}
                  {#each (q.options ?? []).filter((o) => o.value) as o (o.value)}<option value={o.value}>{optionLabel(o)}</option>{/each}
                {/if}
              </select>
            {:else if q.type !== 'photo'}
              <TemplateInput
                id="{p}-question-{qid}-default"
                name="Default for question {n}"
                values={available}
                placeholder="Default, as {'{user}'}"
                bind:value={() => q.default ?? '', (v) => setQuestion(i, { default: v })}
              />
            {/if}
          </div>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <label class="inline-flex min-h-11 items-center gap-2"><input type="checkbox" checked={!!q.required} onchange={(e) => setQuestion(i, { required: e.currentTarget.checked })} /> Required</label>
            <span class="flex gap-1">
              <button type="button" class="btn-secondary px-0" aria-label="Move question {n} up" disabled={i === 0} onclick={() => moveQuestion(i, -1)}>↑</button>
              <button type="button" class="btn-secondary px-0" aria-label="Move question {n} down" disabled={i === (step.questions ?? []).length - 1} onclick={() => moveQuestion(i, 1)}>↓</button>
              <button type="button" class="btn-secondary" aria-label="Remove question {n}" onclick={() => removeQuestion(i)}>Remove</button>
            </span>
          </div>
        </div>
      {/each}
      <button type="button" class="btn-secondary" onclick={addQuestion}>Add a question</button>
    </fieldset>
  {:else if step.kind === 'set_fields'}
    <p class="font-semibold" id="{p}-placement">{placement}</p>
    <fieldset class="space-y-2" aria-describedby="{p}-placement">
      <legend class="label">Fields</legend>
      <p class="text-xs text-zinc-600 dark:text-zinc-400">
        Each value may be an answer, as {'{until}'}, a value every run has, as {'{today}'} or {'{user}'}, fixed text, or a mix.
      </p>
      {#each step.fields ?? [] as row, i (i)}
        {@const n = i + 1}
        {@const fixed = fixedValues(row.field)}
        <div class="grid items-start gap-2 rounded border border-zinc-200 p-2 sm:grid-cols-[1fr_1fr_auto] dark:border-zinc-800" role="group" aria-label="Field {n}">
          <select class="input" aria-label="Field {n}" value={row.field} onchange={(e) => setRow(i, { field: e.currentTarget.value })}>
            <option value="">Choose a field…</option>
            {#if row.field && !fieldOf(row.field)}<option value={row.field}>{row.field}</option>{/if}
            {#each byCategory as g (g.category.id)}
              <optgroup label={g.category.name}>
                {#each g.fields as f (f.key)}<option value={f.key}>{f.label}</option>{/each}
              </optgroup>
            {/each}
          </select>
          <div>
            <TemplateInput
              id="{p}-row-{i}-value"
              name="Value of field {n}"
              values={available}
              placeholder={fieldOf(row.field)?.validation_type === 'date' ? '{today}' : '{user}'}
              list={fixed.length ? `${p}-row-${i}-fixed` : undefined}
              bind:value={() => row.value, (v) => setRow(i, { value: v ?? '' })}
            />
            {#if fixed.length}
              <datalist id="{p}-row-{i}-fixed">
                {#each fixed as v (v.value)}<option value={v.value} label={v.label}></option>{/each}
              </datalist>
              <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Its choices: {fixed.map((v) => v.value).join(', ')}</p>
            {/if}
          </div>
          <span class="flex gap-1">
            <button type="button" class="btn-secondary px-0" aria-label="Move field {n} up" disabled={i === 0} onclick={() => moveRow(i, -1)}>↑</button>
            <button type="button" class="btn-secondary px-0" aria-label="Move field {n} down" disabled={i === (step.fields ?? []).length - 1} onclick={() => moveRow(i, 1)}>↓</button>
            <button type="button" class="btn-secondary" aria-label="Remove field {n}" onclick={() => (step.fields = (step.fields ?? []).filter((_, j) => j !== i))}>Remove</button>
          </span>
        </div>
      {/each}
      <button type="button" class="btn-secondary" onclick={() => (step.fields = [...(step.fields ?? []), { field: '', value: '' }])}>Add a field</button>
    </fieldset>
    <div class="sm:max-w-[50%]">
      <label class="label" for="{p}-photo">Its photo</label>
      <select id="{p}-photo" class="input" value={step.photo ?? ''} onchange={(e) => (step.photo = e.currentTarget.value || undefined)}>
        <option value="">None</option>
        {#each photoQuestions as q (q.key)}<option value={`{${q.key}}`}>{q.label || q.key}</option>{/each}
      </select>
    </div>
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
        <div class="flex flex-wrap gap-2">
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
            <TemplateInput
              id="{p}-match-value"
              name="The value it matches"
              class="min-w-48 flex-1"
              values={available}
              placeholder={'{user}'}
              bind:value={() => step.match?.value ?? '', (v) => (step.match = { field: step.match!.field, value: v ?? '' })}
            />
          {/if}
        </div>
      </div>
    </div>
  {:else if step.kind === 'create'}
    <div>
      <label class="label" for="{p}-name">Its name</label>
      <TemplateInput id="{p}-name" name="Its name" labelled values={available} placeholder={'{owner}: {what}'} bind:value={step.name} />
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
            <TemplateInput
              id="{p}-change-{i}-note"
              name="Note for change {i + 1}"
              class="min-w-48 flex-1"
              values={available}
              placeholder="Note"
              bind:value={() => c.note ?? '', (v) => setChange(i, { note: v })}
            />
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
      <p class="text-xs text-zinc-600 dark:text-zinc-400">To set its fields, add a Set fields step after the step that finds or files it.</p>
    </fieldset>
  {/if}

  {#if step.kind !== 'done'}
    <div class="flex flex-wrap items-center gap-2">
      <label class="inline-flex items-center gap-2"><input type="checkbox" checked={!!step.when} onchange={(e) => setWhen(e.currentTarget.checked)} /> Only when</label>
      {#if step.when}
        <select
          class="input w-auto"
          aria-label="The value it depends on"
          aria-invalid={depends ? undefined : true}
          aria-describedby={depends ? undefined : `${p}-when-unknown`}
          value={step.when.value}
          onchange={(e) => setWhenValue(e.currentTarget.value)}
        >
          {#if !depends}<option value={step.when.value}>{`{${step.when.value}}`}</option>{/if}
          {#each available as v (v.key)}<option value={v.key}>{v.label} {`{${v.key}}`}</option>{/each}
        </select>
        <select class="input w-auto" aria-label="How" value={step.when.op} onchange={(e) => (step.when = { ...step.when!, op: e.currentTarget.value as 'is' })}>
          <option value="is">is</option>
          <option value="is_not">is not</option>
          <option value="set">is given</option>
          <option value="unset">is not given</option>
        </select>
        {#if step.when.op === 'is' || step.when.op === 'is_not'}
          {#if depends?.input.kind === 'category'}
            <select class="input w-auto" aria-label="The category" value={step.when.to ?? ''} onchange={(e) => (step.when = { ...step.when!, to: e.currentTarget.value })}>
              {#each comparedOptions as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
            </select>
          {:else if depends?.input.kind === 'choices'}
            <select class="input w-auto" aria-label="The choice" value={step.when.to ?? ''} onchange={(e) => (step.when = { ...step.when!, to: e.currentTarget.value })}>
              {#each comparedOptions as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
            </select>
          {:else if depends?.input.kind === 'date'}
            <input type="date" class="input w-auto" aria-label="The date" value={step.when.to ?? ''} oninput={(e) => (step.when = { ...step.when!, to: e.currentTarget.value })} />
          {:else}
            <input class="input w-36" aria-label="The value" value={step.when.to ?? ''} oninput={(e) => (step.when = { ...step.when!, to: e.currentTarget.value })} />
          {/if}
        {/if}
      {/if}
    </div>
    {#if step.when && !depends}
      <p id="{p}-when-unknown" class="error-text">{`{${step.when.value}}`} is not set before this step: choose a value an earlier step sets.</p>
    {/if}
    {#if depends?.note}<p class="text-xs text-zinc-600 dark:text-zinc-400">{depends.note}</p>{/if}
  {/if}
</div>
