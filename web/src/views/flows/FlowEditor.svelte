<script lang="ts">
  // /flows/new and /flows/:slug/edit: a flow's steps as cards, in order,
  // each with its settings, beside a preview of the step chosen as a phone
  // shows it (README §6.7), laid out as form builders lay out questions.
  import { onMount } from 'svelte';
  import { ApiError, request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { navigate } from '../../lib/router.svelte';
  import type { AssetAction, Category, EffectiveField, Field, Printer, PrinterPool, TemplateSummary } from '../../lib/models';
  import type { Definition, Flow, FlowProblem, Starter, StepKind } from '../../lib/flows';
  import { addStep, addableKinds, checkFlow, kindHelp, kindLabels, moveStep, problemsOf, slugFrom } from '../../lib/flowEditor';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
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
  let def = $state<Definition>({ version: 1, category_ids: [], steps: [{ id: 'done', kind: 'done', title: 'Done' }] });
  let selected = $state(0);
  let adding = $state<StepKind>('form');

  // A new flow starts from a starter, over the categories chosen.
  let starters = $state<Starter[]>([]);
  let starterKey = $state('leave');

  let allCategories = $state<Category[]>([]);
  let flows = $state<Flow[]>([]);
  let templates = $state<{ id: string; name: string }[]>([]);
  let destinations = $state<{ value: string; label: string }[]>([]);
  let actions = $state<{ key: string; label: string }[]>([]);
  let fields = $state<Field[]>([]);

  const flowCategories = $derived(def.category_ids.map((id) => allCategories.find((c) => c.id === id)).filter((c): c is Category => !!c));

  onMount(async () => {
    try {
      const [cats, list] = await Promise.all([
        request<{ categories: Category[] }>('GET', '/categories').then((r) => r.categories ?? []),
        request<{ flows: Flow[] }>('GET', '/flows').then((r) => r.flows ?? []),
      ]);
      allCategories = cats;
      flows = list;
      // Extras: the editor works without them, offering less.
      const [tpls, printers, pools, acts] = await Promise.all([
        request<{ templates: TemplateSummary[] }>('GET', '/templates').then((r) => r.templates ?? [], () => []),
        request<{ printers: Printer[] }>('GET', '/printers').then((r) => r.printers ?? [], () => []),
        request<{ pools: PrinterPool[] }>('GET', '/printer-pools').then((r) => r.pools ?? [], () => []),
        request<{ actions: AssetAction[] }>('GET', '/actions').then((r) => r.actions ?? [], () => []),
      ]);
      templates = tpls.map((t) => ({ id: t.id, name: t.name }));
      destinations = [...printers.map((p) => ({ value: `printer:${p.id}`, label: p.name })), ...pools.map((p) => ({ value: `pool:${p.id}`, label: `${p.name} (pool)` }))];
      actions = acts.map((a) => ({ key: a.key, label: a.label }));
      if (editing) {
        const f = (await request<{ flow: Flow }>('GET', `/flows/${encodeURIComponent(editing)}`)).flow;
        name = f.name;
        slug = f.slug;
        slugTouched = true;
        enabled = f.enabled;
        def = f.definition;
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
      },
    );
  });

  $effect(() => {
    if (!slugTouched) slug = slugFrom(name);
  });

  function toggleCategory(id: string, on: boolean) {
    const set = new Set(def.category_ids);
    if (on) set.add(id);
    else set.delete(id);
    def.category_ids = allCategories.map((c) => c.id).filter((c) => set.has(c));
  }

  async function useStarter() {
    error = '';
    try {
      starters = (await request<{ starters: Starter[] }>('GET', `/flow-starters?category_ids=${def.category_ids.map(encodeURIComponent).join(',')}`)).starters ?? [];
      const s = starters.find((x) => x.key === starterKey);
      if (!s) return;
      def = { ...s.definition, category_ids: [...def.category_ids] };
      if (!name) name = s.name;
      selected = 0;
    } catch (err) {
      error = messageOf(err);
    }
  }

  function add() {
    def.steps = addStep(def.steps, adding);
    selected = Math.max(0, def.steps.length - 2);
  }

  function remove(i: number) {
    def.steps = def.steps.filter((_, j) => j !== i);
    selected = Math.min(selected, def.steps.length - 1);
  }

  function move(i: number, by: -1 | 1) {
    const before = def.steps;
    def.steps = moveStep(def.steps, i, by);
    if (def.steps !== before && selected === i) selected = i + by;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    saved = error = '';
    problems = checkFlow(def);
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
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">{editing ? `Edit ${name || 'flow'}` : 'New flow'}</h1>
  {#if editing}<a class="btn-secondary" href="/flows/{encodeURIComponent(editing)}/poster">Poster</a>{/if}
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
              <span class="text-zinc-600 dark:text-zinc-400">/f/</span>
              <input id="flow-slug" class="input font-mono" required maxlength="64" bind:value={slug} oninput={() => (slugTouched = true)} />
            </div>
          </div>
        </div>
        <div>
          <label class="label" for="flow-description">One line for its poster</label>
          <input id="flow-description" class="input" maxlength="255" placeholder="Scan to leave something here, with your name on it." bind:value={def.description} />
        </div>
        <fieldset>
          <legend class="label">Files into, or takes from</legend>
          <div class="flex flex-wrap gap-x-4">
            {#each allCategories as c (c.id)}
              <label class="inline-flex items-center gap-2 text-sm">
                <input type="checkbox" checked={def.category_ids.includes(c.id)} onchange={(e) => toggleCategory(c.id, e.currentTarget.checked)} />
                {c.name}
              </label>
            {/each}
          </div>
        </fieldset>
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

      <ol class="space-y-3" aria-label="Steps">
        {#each def.steps as step, i (i)}
          {@const own = problemsOf(problems, step.id)}
          <li>
            <article class="card space-y-3 {selected === i ? 'ring-2 ring-accent-600' : ''}" aria-labelledby="step-{i}-heading" onfocusin={() => (selected = i)}>
              <header class="flex flex-wrap items-center justify-between gap-2">
                <h3 id="step-{i}-heading" class="font-semibold">
                  {i + 1}. {kindLabels[step.kind]}{#if step.title}: {step.title}{/if}
                </h3>
                <span class="flex gap-1">
                  <button type="button" class="btn-sm" aria-pressed={selected === i} aria-label="Preview step {i + 1}" onclick={() => (selected = i)}>Preview</button>
                  <button type="button" class="btn-sm" aria-label="Move step {i + 1} up" disabled={i === 0 || step.kind === 'done'} onclick={() => move(i, -1)}>↑</button>
                  <button type="button" class="btn-sm" aria-label="Move step {i + 1} down" disabled={step.kind === 'done' || def.steps[i + 1]?.kind === 'done'} onclick={() => move(i, 1)}>↓</button>
                  {#if step.kind !== 'done'}<button type="button" class="btn-sm" aria-label="Remove step {i + 1}" onclick={() => remove(i)}>Remove</button>{/if}
                </span>
              </header>
              <p class="text-xs text-zinc-600 dark:text-zinc-400">{kindHelp[step.kind]}</p>
              <FlowStepEditor prefix="step-{i}" bind:step={() => def.steps[i]!, (v) => (def.steps[i] = v)} categories={flowCategories} {fields} flows={flows.filter((f) => f.slug !== editing)} {destinations} {templates} {actions} />
              {#each own as m, j (j)}<p class="error-text" role="alert">{m}</p>{/each}
            </article>
          </li>
        {/each}
      </ol>

      <div class="flex flex-wrap items-end gap-2">
        <div>
          <label class="label" for="flow-add">Add a step</label>
          <select id="flow-add" class="input" bind:value={adding}>
            {#each addableKinds as k (k)}<option value={k}>{kindLabels[k]}</option>{/each}
          </select>
        </div>
        <button type="button" class="btn-secondary" onclick={add}>Add</button>
      </div>

      <div class="flex gap-2">
        <button type="submit" class="btn-primary">Save flow</button>
        <a class="btn-secondary" href="/flows">Back to flows</a>
      </div>
    </div>

    <aside class="lg:sticky lg:top-4 lg:self-start" aria-label="Preview">
      <FlowPreview step={def.steps[selected] ?? null} flowName={name} categories={flowCategories} {fields} />
    </aside>
  </form>
{/if}
