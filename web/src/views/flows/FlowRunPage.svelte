<script lang="ts">
  // /f/:slug runs a flow, one step at a time (README §6.7): large controls,
  // for a phone or the shop's tablet, an on-screen keyboard or a scanner
  // that types.
  import { onMount, tick } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { formDefaults, inputType, missing, runRequest, runsPath, scannedCode, type Prompt, type RunView } from '../../lib/flows';
  import CodeScanner from '../../components/CodeScanner.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const slug = $derived(params.slug ?? '');

  let view = $state<RunView | null>(null);
  let error = $state('');
  let busy = $state(false);
  let heading: HTMLHeadingElement | undefined = $state();

  // The form's answers, filled from its defaults at each new step.
  let name = $state('');
  let fields = $state<Record<string, string>>({});
  let answers = $state<Record<string, string>>({});
  let photo = $state<File | null>(null);
  // A photo is added once the asset it is of exists.
  let pendingPhoto: File | null = null;
  let code = $state('');
  let search = $state('');

  const prompt = $derived<Prompt | null>(view?.prompt ?? null);
  const flowName = $derived(view?.run.values.flow ?? 'Flow');

  onMount(start);

  async function start() {
    error = '';
    busy = true;
    pendingPhoto = null;
    try {
      // Started from an asset, a flow that begins by finding starts with it,
      // once: starting again is for the next person, and the next thing.
      const from = new URLSearchParams(location.search).get('asset');
      if (from) history.replaceState(history.state, '', location.pathname);
      show(await runRequest('POST', runsPath(slug), from ? { asset_id: from } : undefined));
    } catch (err) {
      view = null;
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function show(next: RunView) {
    const moved = next.prompt.step !== view?.prompt.step || next.run.id !== view?.run.id;
    view = next;
    error = next.error ?? '';
    if (moved) {
      const d = formDefaults(next.prompt);
      fields = d.fields;
      answers = d.answers;
      name = '';
      photo = null;
      code = '';
      search = '';
      await tick();
      heading?.focus();
    }
    if (pendingPhoto && next.prompt.asset) {
      const form = new FormData();
      form.append('file', pendingPhoto);
      pendingPhoto = null;
      try {
        await request('POST', `/assets/${encodeURIComponent(next.prompt.asset.id)}/photos`, form);
      } catch (err) {
        error = `It is filed, but its photo was not added: ${messageOf(err)}`;
      }
    }
  }

  async function answer(body: Record<string, unknown>) {
    if (!view) return;
    busy = true;
    error = '';
    try {
      show(await runRequest('POST', runsPath(slug, view.run.id), { step: view.prompt.step, ...body }));
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function submitForm(event: SubmitEvent) {
    event.preventDefault();
    if (!prompt) return;
    const left = missing(prompt, name, fields, answers);
    if (left.length) {
      error = `${left.join(', ')} ${left.length === 1 ? 'is' : 'are'} required.`;
      return;
    }
    pendingPhoto = photo;
    void answer({ name, fields, answers });
  }

  function submitCode(event: SubmitEvent, as: 'code' | 'place_code') {
    event.preventDefault();
    const c = scannedCode(code);
    if (c) void answer({ [as]: c });
  }

  function scanned(text: string, as: 'code' | 'place_code') {
    const c = scannedCode(text);
    if (c) void answer({ [as]: c });
  }

  async function find(event: SubmitEvent) {
    event.preventDefault();
    if (!view) return;
    busy = true;
    try {
      show(await runRequest('GET', `${runsPath(slug, view.run.id)}?search=${encodeURIComponent(search)}`));
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function cancel() {
    if (view && !view.run.finished) {
      await request('DELETE', runsPath(slug, view.run.id)).catch(() => undefined);
    }
    view = null;
    await start();
  }

  function photoChosen(event: Event) {
    photo = (event.currentTarget as HTMLInputElement).files?.[0] ?? null;
  }
</script>

<div class="mx-auto max-w-xl space-y-5 text-lg">
  <p class="text-sm font-medium tracking-wide text-zinc-600 uppercase dark:text-zinc-400">{flowName}</p>

  {#if !prompt}
    {#if busy}<p role="status">Starting…</p>{/if}
    <ErrorMessage {error} />
    {#if error && !busy}<button type="button" class="flow-button-primary" onclick={start}>Try again</button>{/if}
  {:else}
    <h1 bind:this={heading} tabindex="-1" class="text-3xl font-bold focus:outline-none">
      {prompt.title || (prompt.kind === 'done' ? 'Done' : prompt.kind === 'confirm' ? 'Is this right?' : flowName)}
    </h1>
    {#if prompt.text}<p>{prompt.text}</p>{/if}
    <ErrorMessage {error} />

    {#if prompt.kind === 'choose_category'}
      {#if (prompt.categories ?? []).length > 8}
        <label class="label" for="flow-category">Which kind?</label>
        <select id="flow-category" class="flow-input" onchange={(e) => answer({ category_id: e.currentTarget.value })} disabled={busy}>
          <option value="">Choose…</option>
          {#each prompt.categories ?? [] as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
        </select>
      {:else}
        <ul class="grid gap-3 sm:grid-cols-2">
          {#each prompt.categories ?? [] as c (c.id)}
            <li>
              <button type="button" class="flow-choice" disabled={busy} onclick={() => answer({ category_id: c.id })}>
                <span class="h-6 w-6 shrink-0 rounded-full" style="background-color: {c.color_hex || '#71717a'}" aria-hidden="true"></span>
                {c.name}
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    {:else if prompt.kind === 'form'}
      <form class="space-y-4" onsubmit={submitForm} novalidate>
        {#if prompt.ask_name}
          <div>
            <label class="label" for="flow-name">{prompt.name_label || 'Its name'}</label>
            <input id="flow-name" class="flow-input" required maxlength="255" autocomplete="off" bind:value={name} />
          </div>
        {/if}
        {#each prompt.fields ?? [] as f (f.key)}
          <div>
            <label class="label" for="flow-field-{f.key}">{f.label}{#if f.required}<span aria-hidden="true"> *</span>{/if}</label>
            {#if f.type === 'select'}
              <select id="flow-field-{f.key}" class="flow-input" required={f.required} aria-describedby={f.help ? `flow-help-${f.key}` : undefined} bind:value={fields[f.key]}>
                <option value="">—</option>
                {#each f.options?.choices ?? [] as choice (choice.value)}<option value={choice.value}>{choice.label}</option>{/each}
              </select>
            {:else}
              <input
                id="flow-field-{f.key}"
                class="flow-input"
                type={inputType(f.type)}
                required={f.required}
                autocomplete="off"
                aria-describedby={f.help ? `flow-help-${f.key}` : undefined}
                bind:value={fields[f.key]}
              />
            {/if}
            {#if f.help}<p id="flow-help-{f.key}" class="mt-1 text-sm text-zinc-600 dark:text-zinc-400">{f.help}</p>{/if}
          </div>
        {/each}
        {#each prompt.questions ?? [] as q (q.key)}
          <div>
            <label class="label" for="flow-q-{q.key}">{q.label}{#if q.required}<span aria-hidden="true"> *</span>{/if}</label>
            <input
              id="flow-q-{q.key}"
              class="flow-input"
              type={inputType(q.type)}
              required={q.required}
              autocomplete="off"
              aria-describedby={q.help ? `flow-qhelp-${q.key}` : undefined}
              bind:value={answers[q.key]}
            />
            {#if q.help}<p id="flow-qhelp-{q.key}" class="mt-1 text-sm text-zinc-600 dark:text-zinc-400">{q.help}</p>{/if}
          </div>
        {/each}
        {#if prompt.ask_photo}
          <div>
            <label class="label" for="flow-photo">A photo</label>
            <input id="flow-photo" class="block w-full text-base" type="file" accept="image/jpeg,image/png" capture="environment" onchange={photoChosen} />
          </div>
        {/if}
        <button type="submit" class="flow-button-primary" disabled={busy}>Next</button>
      </form>
    {:else if prompt.kind === 'find'}
      {#if prompt.find_by?.includes('scan')}
        <form class="space-y-3" onsubmit={(e) => submitCode(e, 'code')}>
          <label class="label" for="flow-code">Its code</label>
          <div class="flex gap-2">
            <input id="flow-code" class="flow-input" autocomplete="off" placeholder="Type or scan it" bind:value={code} />
            <button type="submit" class="btn-primary min-h-12 text-lg" disabled={busy || !code.trim()}>Find</button>
          </div>
          <CodeScanner onfound={(text) => scanned(text, 'code')} />
        </form>
      {/if}
      {#if prompt.find_by?.includes('search')}
        <form class="flex gap-2" onsubmit={find} role="search">
          <label class="sr-only" for="flow-search">Search</label>
          <input id="flow-search" class="flow-input" type="search" autocomplete="off" placeholder="Search by name" bind:value={search} />
          <button type="submit" class="btn-secondary min-h-12 text-lg" disabled={busy}>Search</button>
        </form>
      {/if}
      {#if prompt.assets?.length}
        <ul class="space-y-2" aria-label="Found">
          {#each prompt.assets as a (a.id)}
            <li>
              <button type="button" class="flow-choice" disabled={busy} onclick={() => answer({ asset_id: a.id })}>
                <span class="flex flex-col items-start text-left">
                  <span>{a.name}</span>
                  <span class="text-sm text-zinc-600 dark:text-zinc-400">{[a.code, ...a.categories].filter(Boolean).join(' · ')}</span>
                  {#if a.overdue}<span class="text-sm text-amber-800 dark:text-amber-400">Past its date, {a.overdue}</span>{/if}
                </span>
              </button>
            </li>
          {/each}
        </ul>
      {:else if prompt.find_by?.includes('list') || search}
        <p class="text-zinc-600 dark:text-zinc-400">Nothing here to find.</p>
      {/if}
    {:else if prompt.kind === 'create'}
      <form class="space-y-3" onsubmit={(e) => submitCode(e, 'place_code')}>
        <label class="label" for="flow-place">Scan where you put it</label>
        <div class="flex gap-2">
          <input id="flow-place" class="flow-input" autocomplete="off" placeholder="The shelf's code" bind:value={code} />
          <button type="submit" class="btn-primary min-h-12 text-lg" disabled={busy || !code.trim()}>Next</button>
        </div>
        <CodeScanner onfound={(text) => scanned(text, 'place_code')} />
      </form>
    {:else if prompt.kind === 'confirm'}
      {#if prompt.asset}<p class="font-semibold">{prompt.asset.name}</p>{/if}
      {#if prompt.coming?.length}
        <ul class="list-disc space-y-1 pl-6">
          {#each prompt.coming as line, i (i)}<li>{line}</li>{/each}
        </ul>
      {/if}
      <div class="flex flex-wrap gap-3">
        <button type="button" class="flow-button-primary" disabled={busy} onclick={() => answer({})}>Yes, go ahead</button>
        <button type="button" class="flow-button-secondary" disabled={busy} onclick={cancel}>No, start again</button>
      </div>
    {:else if prompt.kind === 'done'}
      {#if prompt.done?.length}
        <ul class="space-y-1" aria-label="What was done">
          {#each prompt.done as d, i (i)}<li>{d.said}</li>{/each}
        </ul>
      {/if}
      {#if prompt.show_label && prompt.asset}
        <figure class="card space-y-2">
          {#if prompt.label_template_id}
            <img
              class="mx-auto max-h-64 bg-white"
              src="/api/v1/assets/{encodeURIComponent(prompt.asset.id)}/label?template_id={encodeURIComponent(prompt.label_template_id)}&format=svg"
              alt="The label for {prompt.asset.name}"
            />
          {/if}
          <figcaption class="text-center">
            {#if prompt.asset.code}Its code: <strong class="font-mono">{prompt.asset.code}</strong>{:else}<a class="underline" href="/assets/{prompt.asset.id}">Open its page</a>{/if}
          </figcaption>
        </figure>
      {/if}
      <button type="button" class="flow-button-primary" onclick={start}>Start again</button>
    {:else}
      <!-- A step that asks nothing, which failed: it can be tried again. -->
      {#if prompt.coming?.length}
        <ul class="list-disc space-y-1 pl-6">
          {#each prompt.coming as line, i (i)}<li>{line}</li>{/each}
        </ul>
      {/if}
      <div class="flex flex-wrap gap-3">
        <button type="button" class="flow-button-primary" disabled={busy} onclick={() => answer({})}>Try again</button>
        <button type="button" class="flow-button-secondary" disabled={busy} onclick={cancel}>Start again</button>
      </div>
    {/if}

    {#if prompt.kind !== 'done' && prompt.kind !== 'confirm'}
      <p><button type="button" class="text-base underline" onclick={cancel}>Start again</button></p>
    {/if}
  {/if}
</div>

