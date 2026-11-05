<script lang="ts">
  import { stack } from '../../lib/stack';
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { AssetHook, Field, IncomingAction, IncomingAsset, IncomingHook, Role, RoleAssignment, Scope } from '../../lib/models';
  import type { Flow } from '../../lib/flows';
  import { describeAssignment } from '../../lib/permissions';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';
  import RolePicker from '../../components/RolePicker.svelte';

  // Incoming hooks (docs/hooks.md#incoming-hooks): URLs that let something
  // outside BAM! act on it. Their secrets are shown once, as tokens' are.

  let { params: _params }: { params: Record<string, string> } = $props();

  let incoming = $state<IncomingHook[]>([]);
  let roles = $state<Role[]>([]);
  let scopes = $state<Scope[]>([]);
  let hooks = $state<AssetHook[]>([]);
  let flows = $state<Flow[]>([]);
  let fields = $state<Field[]>([]);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');

  // The hook being made (id '') or changed, or null.
  let editing = $state<string | null>(null);
  let name = $state('');
  let enabled = $state(true);
  let auth = $state<'bearer' | 'hmac'>('bearer');
  let action = $state<IncomingAction>('hook');
  let hookKey = $state('');
  let flowId = $state('');
  let fieldRows = $state<{ field: string; path: string }[]>([]);
  let asset = $state<IncomingAsset>({ by: 'code', path: 'code' });
  let rateLimit = $state(60);
  let assigned = $state<RoleAssignment[]>([]);

  // The secret of the hook just made or given a new one, shown once.
  let made = $state<{ name: string; secret: string; url: string; auth: string } | null>(null);
  let copied = $state(false);

  const actions: [IncomingAction, string][] = [
    ['hook', 'Trigger a hook'],
    ['quantity', 'Record a use or restock'],
    ['fields', 'Set fields'],
    ['flow', 'Run a flow'],
  ];
  const actionLabel = (a: IncomingAction) => actions.find(([v]) => v === a)?.[1] ?? a;
  const roleName = (id: string) => roles.find((r) => r.id === id)?.name ?? id;
  const scopeName = (id: string) => scopes.find((s) => s.id === id)?.name ?? id;
  const described = (a: RoleAssignment) => describeAssignment(roleName(a.role_id), a.scope_ids.map(scopeName));
  const when = (iso: string | null) => (iso ? new Date(iso).toLocaleString() : '');

  function what(h: IncomingHook): string {
    switch (h.action) {
      case 'hook':
        return `Trigger ${hooks.find((k) => k.key === h.hook)?.label ?? h.hook}`;
      case 'flow':
        return `Run ${flows.find((f) => f.id === h.flow_id)?.name ?? 'a flow'}`;
      default:
        return actionLabel(h.action);
    }
  }

  function found(a: IncomingAsset): string {
    switch (a.by) {
      case 'fixed':
        return 'One asset';
      case 'id':
        return `By ID at ${a.path}`;
      case 'code':
        return `By code at ${a.path}`;
      case 'field':
        return `By ${a.field} at ${a.path}`;
      default:
        return 'None';
    }
  }

  async function load() {
    try {
      [incoming, roles, scopes, hooks, flows, fields] = await Promise.all([
        request<{ incoming_hooks: IncomingHook[] }>('GET', '/incoming').then((r) => r.incoming_hooks ?? []),
        // Extra: each needs a permission of its own; without one, its choices are empty.
        request<{ roles: Role[] }>('GET', '/roles').then((r) => (r.roles ?? []).filter((role) => !role.public), () => []),
        request<{ scopes: Scope[] }>('GET', '/scopes').then((r) => r.scopes ?? [], () => []),
        request<{ hooks: AssetHook[] }>('GET', '/hooks').then((r) => r.hooks ?? [], () => []),
        request<{ flows: Flow[] }>('GET', '/flows').then((r) => r.flows ?? [], () => []),
        request<{ fields: Field[] }>('GET', '/fields').then((r) => r.fields ?? [], () => []),
      ]);
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  function startCreate() {
    editing = '';
    made = null;
    name = '';
    enabled = true;
    auth = 'bearer';
    action = 'hook';
    hookKey = hooks[0]?.key ?? '';
    flowId = flows[0]?.id ?? '';
    fieldRows = [];
    asset = { by: 'code', path: 'code' };
    rateLimit = 60;
    assigned = [];
  }

  function startEdit(h: IncomingHook) {
    editing = h.id;
    made = null;
    name = h.name;
    enabled = h.enabled;
    auth = h.auth;
    action = h.action;
    hookKey = h.hook ?? '';
    flowId = h.flow_id ?? '';
    fieldRows = Object.entries(h.fields ?? {}).map(([field, path]) => ({ field, path }));
    asset = { ...h.asset };
    rateLimit = h.rate_limit;
    assigned = h.roles.map((a) => ({ ...a, scope_ids: [...a.scope_ids] }));
  }

  function body(newSecret = false) {
    const mapped = Object.fromEntries(fieldRows.filter((r) => r.field && r.path).map((r) => [r.field, r.path]));
    return {
      name,
      enabled,
      auth,
      action,
      hook: action === 'hook' ? hookKey : '',
      flow_id: action === 'flow' ? flowId : '',
      fields: action === 'fields' ? mapped : {},
      asset,
      rate_limit: Number(rateLimit),
      roles: assigned,
      new_secret: newSecret,
    };
  }

  function showSecret(res: { incoming_hook: IncomingHook; secret?: string }) {
    if (res.secret) {
      made = { name: res.incoming_hook.name, secret: res.secret, url: res.incoming_hook.url, auth: res.incoming_hook.auth };
      copied = false;
    }
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    try {
      const res = editing
        ? await request<{ incoming_hook: IncomingHook; secret?: string }>('PUT', `/incoming/${encodeURIComponent(editing)}`, body())
        : await request<{ incoming_hook: IncomingHook; secret?: string }>('POST', '/incoming', body());
      showSecret(res);
      if (!res.secret) status = `Saved ${res.incoming_hook.name}.`;
      editing = null;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function newSecret(h: IncomingHook) {
    error = status = '';
    startEdit(h);
    editing = null;
    try {
      const res = await request<{ incoming_hook: IncomingHook; secret?: string }>('PUT', `/incoming/${encodeURIComponent(h.id)}`, body(true));
      showSecret(res);
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function remove(h: IncomingHook) {
    error = status = '';
    try {
      await request('DELETE', `/incoming/${encodeURIComponent(h.id)}`);
      status = `Deleted ${h.name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      copied = true;
    } catch {
      error = 'Could not copy; select it and copy it by hand.';
    }
  }

  function setBy(by: IncomingAsset['by']) {
    asset = by === 'fixed' ? { by, asset_id: '' } : by === 'field' ? { by, field: fields[0]?.key ?? '', path: asset.path || 'value' } : by ? { by, path: asset.path || by } : { by };
  }
</script>

<PageHeader title="Incoming hooks">
  {#snippet lede()}
    An incoming hook is a URL that lets something outside BAM! act on it without an API client: a button on a cabinet, a Home Assistant
    automation, a form on another site, or a scale. Each triggers a hook, records a use or restock, sets fields, or runs a flow, on the asset
    it finds, as the roles it is given. Its changes are recorded under its name (<a class="underline" href="/help/hooks">Hooks</a>).
  {/snippet}
  {#snippet actions()}
    {#if editing === null}<button type="button" class="btn-primary" onclick={startCreate}>New incoming hook</button>{/if}
  {/snippet}
</PageHeader>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if made}
  <section class="card mb-6 max-w-2xl space-y-2 border-amber-400" aria-labelledby="made-heading">
    <h2 id="made-heading" class="text-lg font-semibold">Secret for {made.name}</h2>
    <p class="text-sm">
      Copy it now: it will not be shown again. {made.auth === 'hmac'
        ? 'The sender signs each request with it, as BAM-Signature.'
        : 'The sender sends it as Authorization: Bearer <secret>.'}
    </p>
    <label class="sr-only" for="made-secret">The new secret</label>
    <input id="made-secret" class="input font-mono" readonly value={made.secret} onfocus={(e) => e.currentTarget.select()} />
    <p class="text-sm">It is posted to <code class="break-all">{made.url}</code></p>
    <div class="flex gap-2">
      <button type="button" class="btn-secondary" onclick={() => copy(made!.secret)}>{copied ? 'Copied' : 'Copy'}</button>
      <button type="button" class="btn-secondary" onclick={() => (made = null)}>Done</button>
    </div>
  </section>
{/if}

{#if editing !== null}
  <form class="card mb-6 max-w-2xl space-y-4" onsubmit={save}>
    <h2 class="text-lg font-semibold">{editing ? `Change ${name}` : 'New incoming hook'}</h2>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="incoming-name">Name</label>
        <input id="incoming-name" class="input" required maxlength="255" placeholder="Scale on shelf 3" bind:value={name} />
      </div>
      <div>
        <label class="label" for="incoming-auth">Authentication</label>
        <select id="incoming-auth" class="input" bind:value={auth}>
          <option value="bearer">A secret sent as a bearer token</option>
          <option value="hmac">Requests signed with a secret</option>
        </select>
      </div>
      <div>
        <label class="label" for="incoming-action">What it does</label>
        <select id="incoming-action" class="input" bind:value={action}>
          {#each actions as [value, label] (value)}<option {value}>{label}</option>{/each}
        </select>
      </div>
      {#if action === 'hook'}
        <div>
          <label class="label" for="incoming-hook">Hook</label>
          <select id="incoming-hook" class="input" required bind:value={hookKey}>
            {#each hooks as h (h.id)}<option value={h.key}>{h.label}</option>{/each}
          </select>
        </div>
      {:else if action === 'flow'}
        <div>
          <label class="label" for="incoming-flow">Flow</label>
          <select id="incoming-flow" class="input" required bind:value={flowId}>
            {#each flows as f (f.id)}<option value={f.id}>{f.name}</option>{/each}
          </select>
        </div>
      {/if}
    </div>

    {#if action === 'fields'}
      <fieldset class="space-y-2">
        <legend class="label">Fields to set</legend>
        <p class="text-sm text-zinc-600 dark:text-zinc-400">
          Each from a path into the request's JSON, such as <code>data.grams</code>. With none, the request's own <code>field_values</code> are set.
        </p>
        {#each fieldRows as row, i (i)}
          <div class="flex gap-2">
            <label class="sr-only" for="incoming-field-{i}">Field {i + 1}</label>
            <select id="incoming-field-{i}" class="input" bind:value={row.field}>
              {#each fields as f (f.id)}<option value={f.key}>{f.label}</option>{/each}
            </select>
            <label class="sr-only" for="incoming-field-path-{i}">Path for field {i + 1}</label>
            <input id="incoming-field-path-{i}" class="input font-mono" placeholder="data.grams" bind:value={row.path} />
            <button type="button" class="btn-secondary" onclick={() => (fieldRows = fieldRows.filter((_, j) => j !== i))}>Remove</button>
          </div>
        {/each}
        <button type="button" class="btn-secondary" onclick={() => (fieldRows = [...fieldRows, { field: fields[0]?.key ?? '', path: '' }])}>Add a field</button>
      </fieldset>
    {/if}

    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="incoming-by">How the asset is found</label>
        <select id="incoming-by" class="input" value={asset.by} onchange={(e) => setBy(e.currentTarget.value as IncomingAsset['by'])}>
          <option value="code">By a code in the request</option>
          <option value="id">By an asset ID in the request</option>
          <option value="field">By a field's value in the request</option>
          <option value="fixed">Always the same asset</option>
          {#if action === 'flow'}<option value="">None: the flow finds or files it</option>{/if}
        </select>
      </div>
      {#if asset.by === 'fixed'}
        <div>
          <label class="label" for="incoming-asset">Asset ID</label>
          <input id="incoming-asset" class="input font-mono" required bind:value={asset.asset_id} />
        </div>
      {:else if asset.by}
        {#if asset.by === 'field'}
          <div>
            <label class="label" for="incoming-asset-field">Field</label>
            <select id="incoming-asset-field" class="input" bind:value={asset.field}>
              {#each fields as f (f.id)}<option value={f.key}>{f.label}</option>{/each}
            </select>
          </div>
        {/if}
        <div>
          <label class="label" for="incoming-path">Where it is in the request</label>
          <input id="incoming-path" class="input font-mono" required placeholder="data.code" bind:value={asset.path} />
        </div>
      {/if}
      <div>
        <label class="label" for="incoming-rate">Requests a minute, at most</label>
        <input id="incoming-rate" class="input" type="number" min="1" max="600" required bind:value={rateLimit} />
      </div>
    </div>
    <label class="inline-flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={enabled} /> On</label>
    <RolePicker id="incoming-roles" legend="Acts as" {roles} {scopes} bind:value={assigned} />
    <div class="flex gap-2">
      <button type="submit" class="btn-primary">{editing ? 'Save' : 'Create incoming hook'}</button>
      <button type="button" class="btn-secondary" onclick={() => (editing = null)}>Cancel</button>
    </div>
  </form>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading incoming hooks…</p>
{:else if incoming.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No incoming hooks yet.</p>
{:else}
  <div class="overflow-x-auto">
    <table class="w-full text-left text-sm" use:stack>
      <thead class="border-b border-zinc-300 dark:border-zinc-700">
        <tr>
          <th scope="col" class="py-2 pr-4">Name</th>
          <th scope="col" class="py-2 pr-4">Does</th>
          <th scope="col" class="py-2 pr-4">Finds</th>
          <th scope="col" class="py-2 pr-4">Acts as</th>
          <th scope="col" class="py-2 pr-4">Last used</th>
          <th scope="col" class="py-2"><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#each incoming as h (h.id)}
          <tr class="border-b border-zinc-200 dark:border-zinc-800">
            <td class="py-2 pr-4">
              <span class="font-medium">{h.name}</span>
              {#if !h.enabled}<span class="block text-xs text-zinc-600 dark:text-zinc-400">Off</span>{/if}
              {#if !h.has_secret}<span class="block text-xs text-amber-700 dark:text-amber-400">No secret: give it a new one</span>{/if}
              <code class="block text-xs break-all text-zinc-600 dark:text-zinc-400">{h.url}</code>
            </td>
            <td class="py-2 pr-4">{what(h)}<span class="block text-xs text-zinc-600 dark:text-zinc-400">{h.auth === 'hmac' ? 'Signed' : 'Bearer secret'}, {h.rate_limit} a minute</span></td>
            <td class="py-2 pr-4">{found(h.asset)}</td>
            <td class="py-2 pr-4">{h.roles.map(described).join(', ') || 'None'}</td>
            <td class="py-2 pr-4 whitespace-nowrap">{when(h.last_used_at) || 'Never'}</td>
            <td class="py-2 text-right whitespace-nowrap">
              <button type="button" class="btn-secondary" onclick={() => startEdit(h)}>Edit</button>
              <ConfirmButton label="New secret" question="Give {h.name} a new secret? The old one stops working at once." onconfirm={() => newSecret(h)} />
              <ConfirmButton label="Delete" question="Delete {h.name}? Its URL stops working at once." onconfirm={() => remove(h)} />
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}
