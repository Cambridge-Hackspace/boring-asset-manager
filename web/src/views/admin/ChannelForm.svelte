<script lang="ts">
  import { untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Channel, EventKind, NotifierDriver } from '../../lib/models';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let {
    drivers,
    events,
    channel = null,
    onsaved,
    oncancel,
  }: {
    drivers: NotifierDriver[];
    events: EventKind[];
    /** The channel to edit; null adds one. */
    channel?: Channel | null;
    onsaved: () => void;
    oncancel: () => void;
  } = $props();

  // The form starts from the channel it was opened for and keeps its own state.
  const start = untrack(() => channel);
  let name = $state(start?.name ?? '');
  let driver = $state(start?.driver ?? untrack(() => drivers[0]?.driver ?? ''));
  let options = $state<Record<string, string>>({ ...(start?.options ?? {}) });
  let subscribed = $state<string[]>(start ? [...start.events] : untrack(() => events.map((e) => e.kind)));
  let enabled = $state(start?.enabled ?? true);
  let busy = $state(false);
  let error = $state('');

  const info = $derived(drivers.find((d) => d.driver === driver));
  // Secrets already stored for this driver can be left blank to keep them.
  const keeps = (option: string) => !!start && start.driver === driver && !!start.secrets_set[option];
  const prefix = untrack(() => (start ? `ch-${start.id}` : 'ch-new'));

  function changeDriver() {
    options = {};
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    const sent: Record<string, string> = {};
    for (const o of info?.options ?? []) {
      if (options[o.name]?.trim()) sent[o.name] = options[o.name]!.trim();
    }
    const body = { name, driver, options: sent, enabled, events: subscribed };
    try {
      if (start) await request('PUT', `/notifiers/${encodeURIComponent(start.id)}`, body);
      else await request('POST', '/notifiers', body);
      onsaved();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<form class="card max-w-2xl space-y-4" onsubmit={save} aria-label={start ? `Edit ${start.name}` : 'New channel'}>
  <ErrorMessage {error} />
  <div class="grid gap-4 sm:grid-cols-2">
    <div>
      <label class="label" for="{prefix}-name">Name</label>
      <input id="{prefix}-name" class="input" required maxlength="255" bind:value={name} />
    </div>
    <div>
      <label class="label" for="{prefix}-driver">Service</label>
      <select id="{prefix}-driver" class="input" bind:value={driver} onchange={changeDriver}>
        {#each drivers as d (d.driver)}<option value={d.driver}>{d.label}</option>{/each}
      </select>
    </div>
  </div>

  {#each info?.options ?? [] as o (o.name)}
    <div>
      <label class="label" for="{prefix}-opt-{o.name}">{o.label}{o.required ? '' : ' (optional)'}</label>
      {#if o.multiline}
        <textarea
          id="{prefix}-opt-{o.name}"
          class="input font-mono text-xs"
          rows="4"
          autocomplete="off"
          spellcheck="false"
          required={o.required && !keeps(o.name)}
          placeholder={keeps(o.name) ? 'Set; leave blank to keep it' : ''}
          aria-describedby="{prefix}-opt-{o.name}-help"
          bind:value={options[o.name]}
        ></textarea>
      {:else}
        <input
          id="{prefix}-opt-{o.name}"
          class="input"
          type={o.secret ? 'password' : 'text'}
          autocomplete="off"
          required={o.required && !keeps(o.name)}
          placeholder={keeps(o.name) ? 'Set; leave blank to keep it' : ''}
          aria-describedby="{prefix}-opt-{o.name}-help"
          bind:value={options[o.name]}
        />
      {/if}
      <p id="{prefix}-opt-{o.name}-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        {o.description}{o.secret ? ' It is never shown again once saved.' : ''}
      </p>
    </div>
  {/each}

  <fieldset class="space-y-2">
    <legend class="label">Send a message when</legend>
    {#each events as e (e.kind)}
      <label class="flex items-center gap-2 text-sm"><input type="checkbox" value={e.kind} bind:group={subscribed} /> {e.label}</label>
    {/each}
  </fieldset>
  <label class="flex items-center gap-2 text-sm"><input type="checkbox" bind:checked={enabled} /> Enabled</label>

  <div class="flex flex-wrap gap-2">
    <button type="submit" class="btn-primary" disabled={busy}>{start ? 'Save channel' : 'Create channel'}</button>
    <button type="button" class="btn-secondary" onclick={oncancel}>Cancel</button>
  </div>
</form>
