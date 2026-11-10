<!--
  /assistant-settings: the assistant beside the page (docs/assistant.md): its
  provider, key, limits and how long conversations are kept, and the guides
  that start conversations with a purpose.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { assistantApi, panel, type AssistantSettings, type Guide } from '../../lib/assistant.svelte';
  import type { Category, RelationType } from '../../lib/models';
  import CategoryPicker from '../../components/CategoryPicker.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let settings = $state<AssistantSettings | null>(null);
  // What the form holds, apart from what was read, so a change shows.
  let form = $state({
    enabled: false,
    provider: 'anthropic',
    endpoint: '',
    model: '',
    name: '',
    max_tokens: 16000,
    max_steps: 20,
    max_tools: 128,
    person_messages: 100,
    person_tokens: 2000000,
    instance_messages: 1000,
    instance_tokens: 20000000,
    keep_days: 30,
  });
  let apiKey = $state('');
  let clearKey = $state(false);
  let acknowledge = $state(false);
  let error = $state('');
  let status = $state('');
  let busy = $state(false);

  let guides = $state<Guide[]>([]);
  let categories = $state<Category[]>([]);
  let linkTypes = $state<RelationType[]>([]);
  let guideError = $state('');
  let guideStatus = $state('');
  // The guide being edited: its address, "new" for a new one, or null.
  let editing = $state<string | null>(null);
  let draft = $state({ slug: '', name: '', purpose: '', enabled: true, category_ids: [] as string[], link_type_ids: [] as string[], questions: '' });

  const fixed = (k: string) => !!settings?.fixed?.[k];
  const turningOn = $derived(!!settings && form.enabled && !settings.enabled);
  // What turning it on means, for the provider and model in the form rather
  // than those saved: the server says it of what it holds.
  const disclosure = $derived.by(() => {
    if (!settings) return '';
    let text = settings.disclosure;
    if (settings.endpoint && form.endpoint) text = text.split(settings.endpoint).join(form.endpoint);
    if (settings.model && form.model) text = text.split(settings.model).join(form.model);
    return text;
  });

  function show(s: AssistantSettings) {
    settings = s;
    const { enabled, provider, endpoint, model, name, max_tokens, max_steps, max_tools, person_messages, person_tokens, instance_messages, instance_tokens, keep_days } = s;
    form = { enabled, provider, endpoint, model, name, max_tokens, max_steps, max_tools, person_messages, person_tokens, instance_messages, instance_tokens, keep_days };
    apiKey = '';
    clearKey = false;
    acknowledge = false;
  }

  async function loadGuides() {
    guides = await assistantApi.guides();
  }

  onMount(async () => {
    try {
      show(await assistantApi.settings());
      const [g, c, r] = await Promise.all([
        assistantApi.guides(),
        request<{ categories: Category[] }>('GET', '/categories'),
        request<{ relation_types: RelationType[] }>('GET', '/relation-types'),
      ]);
      guides = g;
      categories = c.categories ?? [];
      linkTypes = r.relation_types ?? [];
    } catch (err) {
      error = messageOf(err);
    }
  });

  // A provider chosen afresh takes its own address and model, unless they
  // were changed from the last one's.
  function chooseProvider(p: string) {
    if (!settings) return;
    const was = form.provider;
    if (!form.endpoint || form.endpoint === settings.default_endpoints[was]) form.endpoint = settings.default_endpoints[p] ?? '';
    if (!form.model || form.model === settings.default_models[was]) form.model = settings.default_models[p] ?? '';
    form.provider = p;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    busy = true;
    try {
      const body: Record<string, unknown> = { ...form, acknowledge, clear_key: clearKey };
      for (const k of ['max_tokens', 'max_steps', 'max_tools', 'person_messages', 'person_tokens', 'instance_messages', 'instance_tokens', 'keep_days']) {
        body[k] = Number(body[k]);
      }
      if (apiKey.trim()) body.api_key = apiKey.trim();
      show(await assistantApi.saveSettings(body));
      status = 'Saved.';
      // The header offers the assistant, or stops, at once.
      void panel.load();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function edit(g: Guide | null) {
    guideError = guideStatus = '';
    editing = g ? g.slug : 'new';
    draft = g
      ? { slug: g.slug, name: g.name, purpose: g.purpose, enabled: g.enabled, category_ids: [...g.category_ids], link_type_ids: [...g.link_type_ids], questions: g.questions.join('\n') }
      : { slug: '', name: '', purpose: '', enabled: true, category_ids: [], link_type_ids: [], questions: '' };
  }

  async function saveGuide(event: SubmitEvent) {
    event.preventDefault();
    guideError = guideStatus = '';
    try {
      const g = await assistantApi.saveGuide(editing === 'new' ? null : editing, {
        ...draft,
        questions: draft.questions.split('\n').map((q) => q.trim()).filter(Boolean),
      });
      await loadGuides();
      editing = null;
      guideStatus = `Saved ${g.name}.`;
    } catch (err) {
      guideError = messageOf(err);
    }
  }

  async function removeGuide(g: Guide) {
    guideError = guideStatus = '';
    try {
      await assistantApi.deleteGuide(g.slug);
      await loadGuides();
      guideStatus = `Deleted ${g.name}.`;
    } catch (err) {
      guideError = messageOf(err);
    }
  }

  type Limit = 'person_messages' | 'person_tokens' | 'instance_messages' | 'instance_tokens' | 'keep_days' | 'max_tokens' | 'max_steps' | 'max_tools';
  const limits: { key: Limit; label: string; hint: string }[] = [
    { key: 'person_messages', label: 'Messages a day, each person', hint: '0 for no limit' },
    { key: 'person_tokens', label: 'Tokens a day, each person', hint: '0 for no limit' },
    { key: 'instance_messages', label: 'Messages a day, everyone', hint: '0 for no limit' },
    { key: 'instance_tokens', label: 'Tokens a day, everyone', hint: '0 for no limit' },
    { key: 'keep_days', label: 'Keep conversations, days', hint: 'after their last message' },
    { key: 'max_tokens', label: 'Most tokens a reply', hint: '' },
    { key: 'max_steps', label: 'Most steps a message', hint: 'calls to the model' },
    { key: 'max_tools', label: 'Most tools offered', hint: '0 for all the person may use' },
  ];
</script>

<PageHeader title="Assistant" lede="An assistant beside every page, using BAM!'s own tools as the person signed in. Those holding Use the assistant see it." />

<section class="card mb-6 max-w-3xl space-y-4" aria-labelledby="assistant-model-heading">
  <h2 id="assistant-model-heading" class="text-lg font-semibold">The model</h2>
  <ErrorMessage {error} />
  {#if settings}
    <form class="space-y-4" onsubmit={save}>
      <label class="flex items-start gap-2 text-sm">
        <input class="mt-1" type="checkbox" bind:checked={form.enabled} disabled={fixed('enabled')} />
        <span>The assistant is on{#if fixed('enabled')} (set in the configuration){/if}</span>
      </label>
      {#if turningOn}
        <div class="notice-strong block space-y-2" role="note">
          <p class="text-sm">{disclosure}</p>
          <label class="flex items-start gap-2 text-sm">
            <input class="mt-1" type="checkbox" bind:checked={acknowledge} required />
            <span>I have read this, and people here may know it</span>
          </label>
        </div>
      {/if}
      <div class="grid gap-3 sm:grid-cols-2">
        <div>
          <label class="label" for="assistant-provider">Provider</label>
          <select id="assistant-provider" class="input" value={form.provider} disabled={fixed('provider')} onchange={(e) => chooseProvider((e.currentTarget as HTMLSelectElement).value)}>
            <option value="anthropic">Anthropic (Messages API)</option>
            <option value="openai">Chat completions with tools (OpenAI and compatible)</option>
          </select>
          {#if fixed('provider')}<p class="text-xs text-zinc-600 dark:text-zinc-400">Set in the configuration</p>{/if}
        </div>
        <div>
          <label class="label" for="assistant-name">Name</label>
          <input id="assistant-name" class="input" bind:value={form.name} disabled={fixed('name')} maxlength="255" />
          {#if fixed('name')}<p class="text-xs text-zinc-600 dark:text-zinc-400">Set in the configuration</p>{/if}
        </div>
        <div>
          <label class="label" for="assistant-endpoint">Endpoint</label>
          <input id="assistant-endpoint" class="input" type="url" bind:value={form.endpoint} disabled={fixed('endpoint')} />
          {#if fixed('endpoint')}<p class="text-xs text-zinc-600 dark:text-zinc-400">Set in the configuration</p>{/if}
        </div>
        <div>
          <label class="label" for="assistant-model">Model</label>
          <input id="assistant-model" class="input" bind:value={form.model} disabled={fixed('model')} />
          {#if fixed('model')}<p class="text-xs text-zinc-600 dark:text-zinc-400">Set in the configuration</p>{/if}
        </div>
        <div class="sm:col-span-2">
          <label class="label" for="assistant-key">Key</label>
          <input
            id="assistant-key"
            class="input"
            type="password"
            autocomplete="off"
            bind:value={apiKey}
            disabled={fixed('api_key') || clearKey}
            placeholder={settings.key_set ? 'Key set: leave blank to keep it' : 'None set'}
          />
          <p class="text-xs text-zinc-600 dark:text-zinc-400">
            {#if fixed('api_key')}Set in the configuration.{:else if settings.key_set}Key set. It is never shown again.{:else}Written here, and never shown again.{/if}
          </p>
          {#if settings.key_set && !fixed('api_key')}
            <label class="mt-1 flex items-start gap-2 text-sm">
              <input class="mt-1" type="checkbox" bind:checked={clearKey} />
              <span>Remove the key</span>
            </label>
          {/if}
        </div>
      </div>
      <fieldset class="grid gap-3 sm:grid-cols-2">
        <legend class="mb-2 text-sm font-semibold">Limits</legend>
        {#each limits as l (l.key)}
          <div>
            <label class="label" for="assistant-{l.key}">{l.label}</label>
            <input id="assistant-{l.key}" class="input" type="number" min="0" bind:value={form[l.key]} disabled={fixed(l.key)} />
            <p class="text-xs text-zinc-600 dark:text-zinc-400">{fixed(l.key) ? 'Set in the configuration' : l.hint}</p>
          </div>
        {/each}
      </fieldset>
      <div class="flex flex-wrap items-center gap-3">
        <button type="submit" class="btn-primary" disabled={busy}>Save</button>
        {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
      </div>
    </form>
  {:else if !error}
    <p class="text-sm" role="status">Loading…</p>
  {/if}
</section>

<section class="card max-w-3xl space-y-4" aria-labelledby="guides-heading">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <h2 id="guides-heading" class="text-lg font-semibold">Guides</h2>
    {#if editing === null}<button type="button" class="btn-secondary" onclick={() => edit(null)}>New guide</button>{/if}
  </div>
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    A guide starts a conversation with a purpose, such as recording the equipment in an area, and makes sure its questions are answered.
  </p>
  <ErrorMessage error={guideError} />
  {#if guideStatus}<p role="status" class="text-sm text-green-700 dark:text-green-400">{guideStatus}</p>{/if}

  {#if editing !== null}
    <form class="space-y-3 rounded border border-zinc-300 p-3 dark:border-zinc-700" onsubmit={saveGuide} aria-label={editing === 'new' ? 'New guide' : 'Edit guide'}>
      <div class="grid gap-3 sm:grid-cols-2">
        <div>
          <label class="label" for="guide-name">Name</label>
          <input id="guide-name" class="input" bind:value={draft.name} required maxlength="255" />
        </div>
        <div>
          <label class="label" for="guide-slug">Address</label>
          <input id="guide-slug" class="input font-mono" bind:value={draft.slug} required pattern="[a-z0-9][a-z0-9_\-]*" maxlength="64" />
          <p class="text-xs text-zinc-600 dark:text-zinc-400">At /g/{draft.slug || '…'}: lowercase letters, digits, - and _</p>
        </div>
      </div>
      <div>
        <label class="label" for="guide-purpose">Purpose</label>
        <textarea id="guide-purpose" class="input" rows="4" bind:value={draft.purpose} required maxlength="4000"></textarea>
      </div>
      <CategoryPicker id="guide-categories" label="Categories it works with" {categories} bind:value={draft.category_ids} />
      {#if linkTypes.length}
        <fieldset>
          <legend class="label">Link types it works with</legend>
          <div class="flex flex-wrap gap-x-4">
            {#each linkTypes as t (t.id)}
              <label class="flex min-h-11 items-center gap-2 text-sm">
                <input type="checkbox" value={t.id} bind:group={draft.link_type_ids} />
                {t.name}
              </label>
            {/each}
          </div>
        </fieldset>
      {/if}
      <div>
        <label class="label" for="guide-questions">Questions to answer, one a line</label>
        <textarea id="guide-questions" class="input" rows="4" bind:value={draft.questions}></textarea>
      </div>
      <label class="flex items-start gap-2 text-sm">
        <input class="mt-1" type="checkbox" bind:checked={draft.enabled} />
        <span>Offered to those using the assistant</span>
      </label>
      <div class="flex flex-wrap gap-2">
        <button type="submit" class="btn-secondary">Save guide</button>
        <button type="button" class="btn-secondary" onclick={() => (editing = null)}>Cancel</button>
      </div>
    </form>
  {/if}

  {#if guides.length}
    <ul class="divide-y divide-zinc-200 dark:divide-zinc-800">
      {#each guides as g (g.id)}
        <li class="flex flex-wrap items-center justify-between gap-2 py-2">
          <div class="min-w-0">
            <p class="font-medium">{g.name}{#if !g.enabled}<span class="chip-muted ml-2">Off</span>{/if}</p>
            <p class="text-xs text-zinc-600 dark:text-zinc-400">{g.questions.length} questions · /g/{g.slug}</p>
          </div>
          <div class="flex flex-wrap gap-2">
            <a class="btn-secondary" href="/g/{encodeURIComponent(g.slug)}">Open</a>
            <a class="btn-secondary" href="/assistant/guides/{encodeURIComponent(g.slug)}/poster">Poster</a>
            <button type="button" class="btn-secondary" onclick={() => edit(g)}>Edit</button>
            <ConfirmButton label="Delete" question="Delete the guide {g.name}?" onconfirm={() => removeGuide(g)} />
          </div>
        </li>
      {/each}
    </ul>
  {:else if editing === null}
    <p class="text-sm">No guides yet.</p>
  {/if}
</section>
