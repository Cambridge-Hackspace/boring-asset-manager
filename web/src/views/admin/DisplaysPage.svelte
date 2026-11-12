<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { DisplaySettings, DisplayToken, DisplayView } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import { driverName } from '../../lib/stock';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';

  // /displays: the e-paper displays, which are printers (docs/printing.md#displays):
  // what each shows, refreshing it, the tokens of those that fetch their
  // image, and how displays are kept current. Displays are added and changed
  // with the printers.
  let { params: _params }: { params: Record<string, string> } = $props();

  let displays = $state<DisplayView[]>([]);
  let tokens = $state<Record<string, DisplayToken[]>>({});
  let settings = $state<DisplaySettings | null>(null);
  let loading = $state(true);
  let error = $state('');
  let status = $state('');

  // A token just made, shown once: its display, name and address, and for
  // an OpenPaper frame, which sends it as a password, the token and user name.
  let made = $state<{ display: string; name: string; url: string; username?: string; secret: string } | null>(null);
  // paperlesspaper's API key as typed: '' leaves the one set.
  let paperlessKey = $state('');
  let copied = $state(false);
  let tokenNames = $state<Record<string, string>>({});

  const canManage = $derived(session.can('settings.manage'));
  const canPrint = $derived(session.can('labels.print'));
  const moment = (iso: string | null | undefined) => (iso ? new Date(iso).toLocaleString() : '');

  async function load() {
    try {
      const [list, s] = await Promise.all([
        request<{ displays: DisplayView[] }>('GET', '/displays').then((r) => r.displays ?? []),
        request<DisplaySettings>('GET', '/settings/displays'),
      ]);
      displays = list;
      settings = s;
      if (canManage) {
        const pulling = list.filter((d) => d.connection.type === 'pull');
        const lists = await Promise.all(
          pulling.map((d) => request<{ tokens: DisplayToken[] }>('GET', `/displays/${encodeURIComponent(d.id)}/tokens`).then((r) => r.tokens ?? [])),
        );
        tokens = Object.fromEntries(pulling.map((d, i) => [d.id, lists[i]!]));
      }
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  /** How a display is reached, as people say it. */
  function reached(d: DisplayView): string {
    switch (d.connection.type) {
      case 'pull':
        return d.driver === 'trmnl' ? `Fetches its image, as TRMNL ${d.connection.address}` : 'Fetches its image';
      case 'paperlesspaper':
        return `Pushed through paperlesspaper’s cloud, to paper ${d.connection.address}`;
      case 'webhook':
        return 'Pushed through a webhook channel';
      case 'openepaperlink':
        return `Pushed to the access point at ${d.connection.address}${d.connection.relay ? `, through relay ${d.connection.relay}` : ''}`;
    }
    return d.connection.type;
  }

  /** What a display shows, and since when. */
  function showing(d: DisplayView): string {
    const s = d.shown;
    if (!s?.shown_at) return 'Shown nothing yet';
    const what = s.asset_name ? `${s.asset_name}, with ${s.template_name || 'a deleted template'}` : 'A test label';
    return `${what}, since ${moment(s.shown_at)}`;
  }

  async function refresh(d: DisplayView) {
    error = status = '';
    try {
      await request('POST', `/displays/${encodeURIComponent(d.id)}/refresh`);
      status = `${d.name} will be refreshed through the print queue.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function makeToken(event: SubmitEvent, d: DisplayView) {
    event.preventDefault();
    error = status = '';
    const name = (tokenNames[d.id] ?? '').trim() || d.name;
    try {
      const r = await request<{ token: DisplayToken; secret: string; url: string; username?: string }>('POST', `/displays/${encodeURIComponent(d.id)}/tokens`, { name });
      made = { display: d.name, name: r.token.name, url: r.url, username: r.username, secret: r.secret };
      copied = false;
      tokenNames[d.id] = '';
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function revoke(d: DisplayView, t: DisplayToken) {
    error = status = '';
    try {
      await request('DELETE', `/displays/${encodeURIComponent(d.id)}/tokens/${encodeURIComponent(t.id)}`);
      status = `Revoked ${t.name}: ${d.name} can no longer fetch its image with it.`;
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function copy() {
    if (!made) return;
    try {
      await navigator.clipboard.writeText(made.username ? made.secret : made.url);
      copied = true;
    } catch {
      error = 'Could not copy; select the address and copy it by hand.';
    }
  }

  async function saveSettings(event: SubmitEvent, clearKey = false) {
    event.preventDefault();
    if (!settings) return;
    error = status = '';
    try {
      const key = clearKey ? '' : paperlessKey.trim();
      settings = await request<DisplaySettings>('PUT', '/settings/displays', {
        refresh_window_seconds: Number(settings.refresh_window_seconds),
        pull_refresh_seconds: Number(settings.pull_refresh_seconds),
        paperlesspaper_base_url: settings.paperlesspaper_base_url,
        ...(key || clearKey ? { paperlesspaper_api_key: key } : {}),
      });
      paperlessKey = '';
      status = clearKey ? 'Cleared paperlesspaper’s API key.' : 'Saved the display settings.';
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<PageHeader title="Displays">
  {#snippet lede()}
    E-paper displays show a label template as a printed label does, but can change. They are printers: add one under
    <a class="underline" href="/printers">Printers</a>, and print an asset’s label to it. A hook can refresh it when the asset changes
    (<a class="underline" href="/help/displays">Displays</a>).
  {/snippet}
</PageHeader>
<ErrorMessage {error} />
{#if status}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

{#if made}
  <section class="card mb-6 max-w-2xl space-y-2 border-amber-400" aria-labelledby="token-heading">
    <h2 id="token-heading" class="text-lg font-semibold">{made.name}, for {made.display}</h2>
    {#if made.username}
      <p class="text-sm">
        In the frame’s settings, give it this address to fetch its image from, with HTTP authentication: user name <code>{made.username}</code>, and
        this token as the password. The token is shown once: anyone who has it can see what the display shows.
      </p>
      <label class="label" for="token-url">Address</label>
      <input id="token-url" class="input font-mono" readonly value={made.url} onfocus={(e) => e.currentTarget.select()} />
      {#if made.url.length > 128}
        <p class="text-sm text-amber-700 dark:text-amber-400">The frame takes an address of at most 128 characters, and this one is {made.url.length}.</p>
      {/if}
      <label class="label" for="token-secret">Password</label>
      <input id="token-secret" class="input font-mono" readonly value={made.secret} onfocus={(e) => e.currentTarget.select()} />
    {:else}
      <p class="text-sm">
        Give the display this address to fetch its image from. The token in it is shown once: anyone who has it can see what the display shows.
      </p>
      <label class="sr-only" for="token-url">The address the display fetches its image from</label>
      <input id="token-url" class="input font-mono" readonly value={made.url} onfocus={(e) => e.currentTarget.select()} />
    {/if}
    <div class="flex gap-2">
      <button type="button" class="btn-secondary" onclick={copy}>{copied ? 'Copied' : made.username ? 'Copy the password' : 'Copy'}</button>
      <button type="button" class="btn-secondary" onclick={() => (made = null)}>Done</button>
    </div>
  </section>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading displays…</p>
{:else if displays.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No displays yet. Add one under <a class="underline" href="/printers">Printers</a>, with a display’s model.</p>
{:else}
  <ul class="space-y-3">
    {#each displays as d (d.id)}
      <li>
        <article class="card space-y-3" aria-labelledby="display-{d.id}">
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div>
              <h2 id="display-{d.id}" class="font-semibold">{d.name}</h2>
              <p class="text-sm text-zinc-600 dark:text-zinc-400">{d.model} ({driverName(d.driver)}). {reached(d)}.</p>
              <p class="text-sm">{showing(d)}.</p>
              {#if d.shown?.refresh_at}<p class="text-sm text-zinc-600 dark:text-zinc-400">A refresh is due at {moment(d.shown.refresh_at)}.</p>{/if}
              {#if d.connection.type === 'pull'}
                <p class="text-sm text-zinc-600 dark:text-zinc-400">
                  {d.shown?.fetched_at ? `Last fetched its image at ${moment(d.shown.fetched_at)}` : 'Has not fetched its image yet'}; asks again every {d.refresh_seconds} seconds.
                </p>
              {/if}
            </div>
            {#if canPrint && d.shown?.asset_id}
              <button type="button" class="btn-secondary" aria-label="Refresh {d.name} now" onclick={() => refresh(d)}>Refresh now</button>
            {/if}
          </div>

          {#if canManage && d.connection.type === 'pull'}
            <div class="space-y-2 border-t border-zinc-200 pt-3 dark:border-zinc-800">
              <h3 class="text-sm font-semibold">Tokens</h3>
              {#if d.driver === 'trmnl'}
                <p class="text-xs text-zinc-600 dark:text-zinc-400">A TRMNL takes its own token when it sets itself up. Revoke its tokens to set it up again.</p>
              {/if}
              {#if (tokens[d.id] ?? []).length === 0}
                <p class="text-sm text-zinc-600 dark:text-zinc-400">None yet: the display cannot fetch its image.</p>
              {:else}
                <ul class="space-y-1 text-sm">
                  {#each tokens[d.id] ?? [] as t (t.id)}
                    <li class="flex flex-wrap items-center justify-between gap-2">
                      <span>{t.name}, made {moment(t.created_at)}{t.last_used_at ? `, last used ${moment(t.last_used_at)}` : ', not used yet'}</span>
                      <ConfirmButton label="Revoke" confirmLabel="Revoke {t.name}" question="Revoke this token? The display can no longer fetch its image with it." onconfirm={() => revoke(d, t)} />
                    </li>
                  {/each}
                </ul>
              {/if}
              {#if d.driver !== 'trmnl'}
                <form class="flex flex-wrap items-end gap-2" onsubmit={(e) => makeToken(e, d)}>
                  <div>
                    <label class="label" for="token-name-{d.id}">Token name</label>
                    <input id="token-name-{d.id}" class="input" maxlength="255" placeholder={d.name} bind:value={tokenNames[d.id]} />
                  </div>
                  <button type="submit" class="btn-secondary">Make a token</button>
                </form>
              {/if}
            </div>
          {/if}
        </article>
      </li>
    {/each}
  </ul>
{/if}

{#if settings && canManage}
  <form class="card mt-6 max-w-2xl space-y-4" onsubmit={saveSettings} aria-labelledby="display-settings-heading">
    <h2 id="display-settings-heading" class="text-lg font-semibold">Keeping displays current</h2>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="label" for="refresh-window">Wait for the rest of a burst (seconds)</label>
        <input id="refresh-window" class="input" type="number" min="0" max="3600" step="1" required aria-describedby="refresh-window-help" bind:value={settings.refresh_window_seconds} />
        <p id="refresh-window-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
          A refresh waits this long after the first change, so a burst of changes is one refresh. E-paper is slow, and some tags limit refreshes.
        </p>
      </div>
      <div>
        <label class="label" for="pull-refresh">Displays that fetch ask again every (seconds)</label>
        <input id="pull-refresh" class="input" type="number" min="60" max="86400" step="1" required aria-describedby="pull-refresh-help" bind:value={settings.pull_refresh_seconds} />
        <p id="pull-refresh-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Unless a display’s own options say otherwise.</p>
      </div>
    </div>
    <fieldset class="space-y-4">
      <legend class="text-sm font-semibold">paperlesspaper’s cloud</legend>
      <p class="text-xs text-zinc-600 dark:text-zinc-400">For OpenPaper frames pushed through paperlesspaper’s cloud. Make the API key under your account in paperlesspaper’s web app.</p>
      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label class="label" for="paperless-base">API address</label>
          <input id="paperless-base" class="input font-mono" type="url" required bind:value={settings.paperlesspaper_base_url} />
        </div>
        <div>
          <label class="label" for="paperless-key">API key</label>
          <input
            id="paperless-key"
            class="input font-mono"
            type="password"
            autocomplete="off"
            aria-describedby="paperless-key-help"
            placeholder={settings.paperlesspaper_api_key_set ? 'Set: type a new one to replace it' : 'Not set'}
            bind:value={paperlessKey}
          />
          <p id="paperless-key-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Kept sealed with the server’s secret key, and never shown again.</p>
        </div>
      </div>
    </fieldset>
    <div class="flex flex-wrap gap-2">
      <button type="submit" class="btn-primary">Save</button>
      {#if settings.paperlesspaper_api_key_set}
        <button type="button" class="btn-secondary" onclick={(e) => saveSettings(e as unknown as SubmitEvent, true)}>Clear paperlesspaper’s API key</button>
      {/if}
    </div>
  </form>
{/if}
