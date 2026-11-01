<script lang="ts">
  // /flows/new and /flows/:slug/edit: a flow's steps in order, one open at a
  // time with its settings, beside a preview of that step as a phone shows
  // it (docs/flows.md), laid out as form builders lay out questions.
  import { onMount } from 'svelte';
  import { ApiError, request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { navigate } from '../../lib/router.svelte';
  import type { AssetHook, Category, EffectiveField, Field, Printer, PrinterPool, TemplateSummary } from '../../lib/models';
  import { definitionVersion, type Definition, type Flow, type FlowProblem, type Starter, type StepKind } from '../../lib/flows';
  import {
    addStep,
    addableKinds,
    checkFlow,
    kindHelp,
    kindLabels,
    moveStep,
    previewProgress,
    problemsOf,
    slugFrom,
    withStepIDs,
    type FlowCatalog,
  } from '../../lib/flowEditor';
  import CategoryPicker from '../../components/CategoryPicker.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import MenuButton from '../../components/MenuButton.svelte';
  import FlowStepEditor from './FlowStepEditor.svelte';
  import FlowPreview from './FlowPreview.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const editing = $derived(params.slug ?? '');

  let loading = $state(true);
  let error = $state('');
  let saved = $state('');
  let problems = $state<FlowProblem[]>([]);

  let name = $state('');
  let slug = $state('');
  let slugTouched = $state(false);
  let enabled = $state(true);
  let def = $state<Definition>({ version: definitionVersion, category_ids: [], steps: [{ id: 'done', kind: 'done', title: 'Done' }] });
  let selected = $state(0);

  // A new flow starts from a starter, over the categories chosen.
  let starters = $state<Starter[]>([]);
  let starterKey = $state('leave');

  let allCategories = $state<Category[]>([]);
  let flows = $state<Flow[]>([]);
  let templates = $state<{ id: string; name: string }[]>([]);
  let destinations = $state<{ value: string; label: string }[]>([]);
  let hooks = $state<{ key: string; label: string }[]>([]);
  let fields = $state<Field[]>([]);
  // The fields each category records, own and inherited, and those it
  // requires, for Set fields to choose from and to check.
  let fieldsOf = $state<Record<string, EffectiveField[]>>({});

  const flowCategories = $derived(def.category_ids.map((id) => allCategories.find((c) => c.id === id)).filter((c): c is Category => !!c));
  const byCategory = $derived(
    flowCategories.map((category) => {
      const list = fieldsOf[category.id] ?? [];
      return { category, fields: list.map((ef) => ef.field), required: list.filter((ef) => ef.is_required).map((ef) => ef.field.key) };
    }),
  );
  // Whether the fields of each of the flow's categories have come, so Set
  // fields can say there are none only once it knows.
  const fieldsKnown = $derived(def.category_ids.every((id) => id in fieldsOf));
  const catalog = $derived<FlowCatalog>({
    fields,
    required: Object.fromEntries(Object.entries(fieldsOf).map(([id, list]) => [id, list.filter((ef) => ef.is_required).map((ef) => ef.field.key)])),
    names: Object.fromEntries(flowCategories.map((c) => [c.id, c.name])),
  });

  onMount(async () => {
    try {
      const [cats, list] = await Promise.all([
        request<{ categories: Category[] }>('GET', '/categories').then((r) => r.categories ?? []),
        request<{ flows: Flow[] }>('GET', '/flows').then((r) => r.flows ?? []),
      ]);
      allCategories = cats;
      flows = list;
      // Extras: the editor works without them, offering less.
      const [tpls, printers, pools, hooked] = await Promise.all([
        request<{ templates: TemplateSummary[] }>('GET', '/templates').then((r) => r.templates ?? [], () => []),
        request<{ printers: Printer[] }>('GET', '/printers').then((r) => r.printers ?? [], () => []),
        request<{ pools: PrinterPool[] }>('GET', '/printer-pools').then((r) => r.pools ?? [], () => []),
        request<{ hooks: AssetHook[] }>('GET', '/hooks').then((r) => r.hooks ?? [], () => []),
      ]);
      templates = tpls.map((t) => ({ id: t.id, name: t.name }));
      destinations = [...printers.map((p) => ({ value: `printer:${p.id}`, label: p.name })), ...pools.map((p) => ({ value: `pool:${p.id}`, label: `${p.name} (pool)` }))];
      hooks = hooked.map((a) => ({ key: a.key, label: a.label }));
      if (editing) {
        const f = (await request<{ flow: Flow }>('GET', `/flows/${encodeURIComponent(editing)}`)).flow;
        name = f.name;
        slug = f.slug;
        slugTouched = true;
        enabled = f.enabled;
        def = { ...f.definition, steps: withStepIDs(f.definition.steps ?? []) };
        flows = flows.filter((x) => x.id !== f.id).concat(f);
        if (new URLSearchParams(location.search).has('saved')) {
          saved = `Saved ${f.name}.`;
          history.replaceState(history.state, '', location.pathname);
        }
      }
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  });

  // The fields an asset in any of the flow's categories records.
  $effect(() => {
    const ids = [...def.category_ids];
    void Promise.all(ids.map((id) => request<{ fields: EffectiveField[] }>('GET', `/categories/${encodeURIComponent(id)}/fields`).then((r) => r.fields ?? [], () => []))).then(
      (lists) => {
        const seen = new Map<string, Field>();
        for (const list of lists) for (const ef of list) if (!seen.has(ef.field.key)) seen.set(ef.field.key, ef.field);
        fields = [...seen.values()];
        fieldsOf = Object.fromEntries(ids.map((id, i) => [id, lists[i] ?? []]));
      },
    );
  });

  $effect(() => {
    if (!slugTouched) slug = slugFrom(name);
  });

  async function useStarter() {
    error = '';
    try {
      starters = (await request<{ starters: Starter[] }>('GET', `/flow-starters?category_ids=${def.category_ids.map(encodeURIComponent).join(',')}`)).starters ?? [];
      const s = starters.find((x) => x.key === starterKey);
      if (!s) return;
      def = { ...s.definition, category_ids: [...def.category_ids], steps: withStepIDs(s.definition.steps) };
      if (!name) name = s.name;
      selected = 0;
    } catch (err) {
      error = messageOf(err);
    }
  }

  function add(kind: StepKind) {
    def.steps = addStep(def.steps, kind);
    selected = Math.max(0, def.steps.length - 2);
  }

  function remove(i: number) {
    def.steps = def.steps.filter((_, j) => j !== i);
    selected = Math.min(selected, def.steps.length - 1);
  }

  function move(i: number, by: -1 | 1) {
    const before = def.steps;
    def.steps = moveStep(def.steps, i, by);
    // The open step stays open, wherever it goes.
    if (def.steps === before) return;
    if (selected === i) selected = i + by;
    else if (selected === i + by) selected = i;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    saved = error = '';
    problems = checkFlow(def, catalog);
    if (problems.length) {
      error = 'The flow cannot be saved yet: see what is said beside each step.';
      return;
    }
    const body = { slug, name, enabled, definition: def };
    try {
      const res = editing
        ? await request<{ flow: Flow }>('PUT', `/flows/${encodeURIComponent(editing)}`, body)
        : await request<{ flow: Flow }>('POST', '/flows', body);
      saved = `Saved ${res.flow.name}.`;
      // A new flow, or a new address, opens at its address; it says so there.
      if (res.flow.slug !== editing) navigate(`/flows/${encodeURIComponent(res.flow.slug)}/edit?saved=1`);
    } catch (err) {
      const found = err instanceof ApiError ? (err.body as { problems?: FlowProblem[] } | undefined)?.problems : undefined;
      problems = found ?? [];
      error = found?.length ? 'The flow cannot be saved yet: see what is said beside each step.' : messageOf(err);
    }
  }

  const flowProblems = $derived(problemsOf(problems));
  // Counted as a run counts them, before any answer (#166).
  const shownProgress = $derived(previewProgress(def.steps, selected));
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">{editing ? `Edit ${name || 'flow'}` : 'New flow'}</h1>
  {#if editing}
    <span class="flex gap-2">
      <a class="btn-secondary" href="/flows/{encodeURIComponent(editing)}/poster">Poster</a>
      <a class="btn-secondary" href="/f/{encodeURIComponent(editing)}">Try it</a>
    </span>
  {/if}
</div>
<ErrorMessage {error} />
{#if saved}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{saved}</p>{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else}
  <form class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_24rem]" onsubmit={save}>
    <div class="space-y-4">
      <section class="card space-y-3" aria-labelledby="flow-about">
        <h2 id="flow-about" class="text-lg font-semibold">The flow</h2>
        <div class="grid gap-3 sm:grid-cols-2">
          <div>
            <label class="label" for="flow-name-input">Name</label>
            <input id="flow-name-input" class="input" required maxlength="255" bind:value={name} />
          </div>
          <div>
            <label class="label" for="flow-slug">Address</label>
            <div class="flex items-center gap-1 text-sm">
              <span class="shrink-0 whitespace-nowrap text-zinc-600 dark:text-zinc-400">/f/</span>
              <input id="flow-slug" class="input min-w-0 font-mono" required maxlength="64" bind:value={slug} oninput={() => (slugTouched = true)} />
            </div>
          </div>
        </div>
        <div>
          <label class="label" for="flow-description">One line for its poster</label>
          <input id="flow-description" class="input" maxlength="255" placeholder="Scan to leave something here, with your name on it." bind:value={def.description} />
        </div>
        <div>
          <CategoryPicker id="flow-categories" label="Files into, or takes from" categories={allCategories} bind:value={def.category_ids} />
          <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">A Choose category step offers these as buttons. Open a category with › to choose ones under it.</p>
        </div>
        <div>
          <label class="label" for="flow-until">A date to collect by</label>
          <select id="flow-until" class="input" bind:value={def.leave_until} aria-describedby="flow-until-help">
            <option value="">None</option>
            {#each fields.filter((f) => f.validation_type === 'date') as f (f.key)}<option value={f.key}>{f.label}</option>{/each}
          </select>
          <p id="flow-until-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
            What it files is overdue the day after this date, listed here, and announced once to the channels that follow it.
          </p>
        </div>
        <label class="inline-flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={enabled} /> Turned on: listed, and runnable</label>
        {#each flowProblems as m, i (i)}<p class="error-text">{m}</p>{/each}
        {#if !editing}
          <div class="flex flex-wrap items-end gap-2 border-t border-zinc-200 pt-3 dark:border-zinc-800">
            <div>
              <label class="label" for="flow-starter">Start from</label>
              <select id="flow-starter" class="input" bind:value={starterKey}>
                <option value="leave">Leave something</option>
                <option value="collect">Collect it</option>
              </select>
            </div>
            <button type="button" class="btn-secondary" disabled={!def.category_ids.length} onclick={useStarter}>Use this starter</button>
          </div>
        {/if}
      </section>

      <section aria-labelledby="flow-steps" class="space-y-2">
        <h2 id="flow-steps" class="text-lg font-semibold">Steps</h2>
        <p class="text-sm text-zinc-600 dark:text-zinc-400">What a person sees, one screen at a time. Choose a step to edit it and see it in the preview.</p>
        <ol class="space-y-2" aria-label="Steps">
          {#each def.steps as step, i (i)}
            {@const own = problemsOf(problems, step.id)}
            {@const open = selected === i}
            <li class="rounded border-2 {open ? 'border-zinc-900 dark:border-zinc-300' : step.kind === 'done' ? 'border-dashed border-zinc-400 dark:border-zinc-600' : 'border-zinc-300 dark:border-zinc-700'} bg-white dark:bg-zinc-900">
              <div class="flex items-center gap-3 px-3 py-2 {open ? 'bg-highlight/40 dark:bg-zinc-800' : ''}">
                <span class="w-6 shrink-0 text-center font-mono text-sm text-zinc-600 dark:text-zinc-400" aria-hidden="true">{i + 1}</span>
                <div class="min-w-0 flex-1">
                  <h3 class="font-semibold">
                    <button
                      type="button"
                      class="inline-flex min-h-11 w-full cursor-pointer items-center text-left"
                      aria-expanded={open}
                      aria-controls="step-{i}-body"
                      onclick={() => (selected = i)}
                    >
                      <span class="sr-only">{i + 1}.</span>{' '}{kindLabels[step.kind]}{#if step.title}: {step.title}{/if}
                    </button>
                  </h3>
                  <p class="text-xs text-zinc-600 dark:text-zinc-400">{kindHelp[step.kind]}</p>
                </div>
                {#if step.kind !== 'done'}
                  <span class="flex shrink-0 gap-1">
                    <button type="button" class="btn-secondary px-0" aria-label="Move step {i + 1} up" disabled={i === 0} onclick={() => move(i, -1)}>↑</button>
                    <button type="button" class="btn-secondary px-0" aria-label="Move step {i + 1} down" disabled={def.steps[i + 1]?.kind === 'done'} onclick={() => move(i, 1)}>↓</button>
                  </span>
                {/if}
              </div>
              {#each own as m, j (j)}<p class="error-text px-3 pb-2" role="alert">{m}</p>{/each}
              {#if open}
                <div id="step-{i}-body" class="space-y-3 border-t-2 border-zinc-900 p-4 dark:border-zinc-300">
                  <!-- A starter used while a step is open starts its editor afresh. -->
                  {#key def}
                    <FlowStepEditor
                      prefix="step-{i}"
                      bind:step={() => def.steps[i]!, (v) => (def.steps[i] = v)}
                      steps={def.steps}
                      at={i}
                      categories={flowCategories}
                      categoryIDs={def.category_ids}
                      {fields}
                      {byCategory}
                      {fieldsKnown}
                      flows={flows.filter((f) => f.slug !== editing)}
                      {destinations}
                      {templates}
                      {hooks}
                    />
                  {/key}
                  {#if step.kind !== 'done'}
                    <div class="flex justify-end border-t border-zinc-200 pt-3 dark:border-zinc-800">
                      <button type="button" class="btn-secondary text-red-700 dark:text-red-400" onclick={() => remove(i)}>Remove this step</button>
                    </div>
                  {/if}
                </div>
              {/if}
            </li>
          {/each}
        </ol>
      </section>

      <div class="flex flex-wrap items-center gap-2">
        <MenuButton id="flow-add" label="Add a step" items={addableKinds.map((k) => ({ label: kindLabels[k], description: kindHelp[k], run: () => add(k) }))} />
        <p class="text-xs text-zinc-600 dark:text-zinc-400">It goes in before Done.</p>
      </div>

      <div class="flex flex-wrap items-center justify-between gap-2 border-t-2 border-zinc-900 pt-4 dark:border-zinc-400">
        <a class="inline-flex min-h-11 items-center underline underline-offset-4" href="/flows">Back to flows</a>
        <button type="submit" class="btn-primary">Save flow</button>
      </div>
    </div>

    <aside class="space-y-2 lg:sticky lg:top-4 lg:self-start" aria-label="Preview">
      <p class="font-mono text-xs tracking-wide text-zinc-600 uppercase dark:text-zinc-400">
        Preview ·
        {#if shownProgress}step {shownProgress.step} of {shownProgress.of}{:else if def.steps[selected]?.kind === 'done'}the end{:else}asks nothing, so a run does it and moves on{/if}
      </p>
      <FlowPreview step={def.steps[selected] ?? null} flowName={name} categories={flowCategories} progress={shownProgress} />
    </aside>
  </form>
{/if}
