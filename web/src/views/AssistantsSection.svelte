<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import type { APIToken } from '../lib/models';
  import { assistantSetup } from '../lib/assistants';
  import ConfirmButton from '../components/ConfirmButton.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';

  // Personal tokens, for assistants such as Claude (docs/access.md, docs/mcp.md): each
  // acts as you, with whatever you may do at the time.

  let tokens = $state<APIToken[]>([]);
  let mcp = $state(false);
  let baseURL = $state('');
  let loading = $state(true);
  let error = $state('');
  let status = $state('');

  let creating = $state(false);
  let name = $state('');
  let lifetime = $state('90');
  let made = $state<{ name: string; secret: string } | null>(null);
  let copied = $state('');

  const lifetimes: [string, string][] = [
    ['30', '30 days'],
    ['90', '90 days'],
    ['365', '1 year'],
    ['', 'Never'],
  ];
  const when = (iso: string | null) => (iso ? new Date(iso).toLocaleDateString() : '');
  // A grant from signing in renews while used, until its refresh expires.
  const until = (t: APIToken) => t.refresh_expires_at ?? t.expires_at;
  const active = $derived(tokens.filter((t) => !t.revoked_at && !(until(t) && new Date(until(t)!) <= new Date())));
  const setup = $derived(made ? assistantSetup(baseURL, made.secret) : []);

  async function load() {
    try {
      const [own, info] = await Promise.all([
        request<{ tokens: APIToken[] }>('GET', '/auth/tokens').then((r) => r.tokens ?? []),
        request<{ mcp?: boolean; base_url?: string }>('GET', '/info'),
      ]);
      tokens = own;
      mcp = info.mcp !== false;
      baseURL = info.base_url || window.location.origin;
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  async function create(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    const expires_at = lifetime ? new Date(Date.now() + Number(lifetime) * 86_400_000).toISOString() : null;
    try {
      const res = await request<{ token: APIToken; secret: string }>('POST', '/auth/tokens', { name, expires_at });
      made = { name: res.token.name, secret: res.secret };
      copied = '';
      creating = false;
      name = '';
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function copy(label: string, text: string) {
    try {
      await navigator.clipboard.writeText(text);
      copied = label;
    } catch {
      error = 'Could not copy; select the text and copy it by hand.';
    }
  }

  async function revoke(t: APIToken) {
    error = status = '';
    try {
      await request('DELETE', `/auth/tokens/${encodeURIComponent(t.id)}`);
      status = `Revoked ${t.name}.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<section class="card space-y-3" aria-labelledby="assistants-heading">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <h2 id="assistants-heading" class="text-lg font-semibold">Assistants</h2>
    {#if !creating && mcp}<button type="button" class="btn-secondary" onclick={() => ((creating = true), (made = null))}>New assistant</button>{/if}
  </div>
  <p class="text-sm">
    An assistant, such as Claude, can work with BAM! for you: find things, add them, lend them, print their labels. It acts as
    you, with whatever you may do at the time, and what it changes is recorded as yours. It cannot change your password or make
    tokens. Revoke it here whenever you like.
  </p>
  {#if !loading && !mcp}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">This server has assistants turned off.</p>
  {/if}
  <ErrorMessage {error} />
  {#if status}<p class="text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

  {#if made}
    <div class="space-y-3 rounded border border-amber-400 p-3" aria-labelledby="made-assistant-heading">
      <h3 id="made-assistant-heading" class="font-semibold">Connect {made.name}</h3>
      <p class="text-sm">Copy what your assistant needs now: its token will not be shown again. Anyone holding it can act as you.</p>
      {#each setup as s (s.label)}
        <div>
          <p class="label">{s.label}</p>
          <p class="mb-1 text-xs text-zinc-600 dark:text-zinc-400">{s.hint}</p>
          <!-- Focusable, so the keyboard can scroll a long line. -->
          <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
          <pre class="overflow-x-auto rounded bg-zinc-100 p-2 text-xs dark:bg-zinc-800" aria-label={s.label} tabindex="0">{s.text}</pre>
          <button type="button" class="btn-secondary mt-1" onclick={() => copy(s.label, s.text)}>{copied === s.label ? 'Copied' : 'Copy'}</button>
        </div>
      {/each}
      <button type="button" class="btn-secondary" onclick={() => (made = null)}>Done</button>
    </div>
  {/if}

  {#if creating}
    <form class="space-y-3" onsubmit={create}>
      <div class="grid gap-3 sm:grid-cols-2">
        <div>
          <label class="label" for="assistant-name">Name</label>
          <input id="assistant-name" class="input" required maxlength="255" placeholder="Claude on my laptop" bind:value={name} />
        </div>
        <div>
          <label class="label" for="assistant-lifetime">Expires after</label>
          <select id="assistant-lifetime" class="input" bind:value={lifetime}>
            {#each lifetimes as [value, label] (value)}<option {value}>{label}</option>{/each}
          </select>
        </div>
      </div>
      <div class="flex gap-2">
        <button type="submit" class="btn-primary">Create assistant</button>
        <button type="button" class="btn-secondary" onclick={() => (creating = false)}>Cancel</button>
      </div>
    </form>
  {/if}

  {#if loading}
    <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading assistants…</p>
  {:else if active.length === 0}
    <p class="text-sm text-zinc-600 dark:text-zinc-400">No assistants connected.</p>
  {:else}
    <ul class="divide-y divide-zinc-200 text-sm dark:divide-zinc-800">
      {#each active as t (t.id)}
        <li class="flex flex-wrap items-center justify-between gap-2 py-2">
          <span>
            <strong>{t.name}</strong>
            <span class="block text-xs text-zinc-600 dark:text-zinc-400">
              {t.oauth_client ? 'Signed in' : 'Made'} {when(t.created_at)}; {t.last_used_at ? `last used ${when(t.last_used_at)}` : 'never used'};
              {t.oauth_client ? `renews while used, until ${when(until(t))} at the latest` : t.expires_at ? `expires ${when(t.expires_at)}` : 'never expires'}
            </span>
          </span>
          <ConfirmButton label="Revoke" question="Revoke {t.name}? It stops working at once." onconfirm={() => revoke(t)} />
        </li>
      {/each}
    </ul>
  {/if}
</section>
