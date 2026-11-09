<script lang="ts" module>
  import type { HookTrigger } from '../lib/models';

  /** The events about an asset a hook can fire on, as people read them. */
  export const hookEvents: { kind: string; label: string }[] = [
    { kind: 'asset.created', label: 'Added' },
    { kind: 'asset.edited', label: 'Edited' },
    { kind: 'asset.restored', label: 'Restored from the archive' },
    { kind: 'asset.quantity_changed', label: 'Its quantity changes' },
    { kind: 'asset.running_low', label: 'Runs low' },
    { kind: 'asset.restocked', label: 'Restocked' },
    { kind: 'asset.condition_changed', label: 'Its condition changes' },
    { kind: 'asset.checked_out', label: 'Lent' },
    { kind: 'asset.checked_in', label: 'Returned' },
    { kind: 'asset.overdue', label: 'Its loan becomes overdue' },
    { kind: 'asset.flow_overdue', label: 'Past the date a flow left it until' },
    { kind: 'asset.label_printed', label: 'Its label is printed' },
    { kind: 'asset.review_due', label: 'Its review falls due' },
  ];

  /** What fires a hook, in a few words, for the list of hooks. */
  export function describeTriggers(triggers: HookTrigger[]): string {
    return triggers
      .map((t) => {
        switch (t.type) {
          case 'button':
            return 'A button';
          case 'event': {
            const names = t.kinds.map((k) => hookEvents.find((e) => e.kind === k)?.label.toLowerCase() ?? k);
            return `When ${names.join(', ')}`;
          }
          case 'schedule':
            return `On a schedule, ${t.cron}${t.time_zone ? ` (${t.time_zone})` : ' (UTC)'}`;
        }
      })
      .join('; ');
  }
</script>

<script lang="ts">
  import type { Field } from '../lib/models';

  // The triggers of the hook being edited: a button, any number of event
  // triggers, and any number of schedules (docs/hooks.md).
  let { triggers = $bindable(), fields }: { triggers: HookTrigger[]; fields: Field[] } = $props();

  const button = $derived(triggers.some((t) => t.type === 'button'));
  const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';

  function setButton(on: boolean) {
    triggers = on ? [{ type: 'button' }, ...triggers.filter((t) => t.type !== 'button')] : triggers.filter((t) => t.type !== 'button');
  }
  function add(t: HookTrigger) {
    triggers = [...triggers, t];
  }
  function remove(i: number) {
    triggers = triggers.filter((_, j) => j !== i);
  }
  function toggleKind(i: number, kind: string, on: boolean) {
    const t = triggers[i];
    if (t?.type !== 'event') return;
    t.kinds = on ? [...t.kinds, kind] : t.kinds.filter((k) => k !== kind);
  }
</script>

<fieldset class="space-y-3">
  <legend class="label">Fired by</legend>
  <label class="flex items-center gap-2 text-sm">
    <input type="checkbox" checked={button} onchange={(e) => setButton(e.currentTarget.checked)} />
    A button people press on assets
  </label>

  {#each triggers as t, i (i)}
    {#if t.type === 'event'}
      <fieldset class="rounded border border-zinc-300 p-3 dark:border-zinc-600">
        <legend class="px-1 text-sm font-semibold">When an asset is…</legend>
        <div class="grid gap-1 sm:grid-cols-2">
          {#each hookEvents as e (e.kind)}
            <label class="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={t.kinds.includes(e.kind)} onchange={(ev) => toggleKind(i, e.kind, ev.currentTarget.checked)} />
              {e.label}
            </label>
          {/each}
        </div>
        <div class="mt-2 flex flex-wrap items-end gap-2">
          <div>
            <label class="label" for="cond-field-{i}">Only when</label>
            <select
              id="cond-field-{i}"
              class="input"
              value={t.condition?.field ?? ''}
              onchange={(e) => {
                const field = e.currentTarget.value;
                t.condition = field ? { field, equals: t.condition?.equals ?? '' } : null;
              }}
            >
              <option value="">Always</option>
              <option value="condition">Its condition</option>
              {#each fields as f (f.key)}<option value={f.key}>{f.label}</option>{/each}
            </select>
          </div>
          {#if t.condition}
            <div>
              <label class="label" for="cond-value-{i}">is</label>
              {#if t.condition.field === 'condition'}
                <select id="cond-value-{i}" class="input" bind:value={t.condition.equals}>
                  <option value="ok">OK</option>
                  <option value="needs_maintenance">Needs maintenance</option>
                  <option value="out_of_service">Out of service</option>
                </select>
              {:else}
                <input id="cond-value-{i}" class="input" maxlength="255" bind:value={t.condition.equals} />
              {/if}
            </div>
          {/if}
          <div>
            <label class="label" for="interval-{i}">At most once every (minutes)</label>
            <input
              id="interval-{i}"
              class="input w-28"
              type="number"
              min="0"
              max="10080"
              value={Math.round((t.interval_seconds ?? 0) / 60)}
              oninput={(e) => (t.interval_seconds = Math.max(0, Math.round(Number(e.currentTarget.value) || 0)) * 60)}
            />
          </div>
        </div>
        <button type="button" class="btn-secondary mt-2" onclick={() => remove(i)}>Remove these events</button>
      </fieldset>
    {:else if t.type === 'schedule'}
      <fieldset class="rounded border border-zinc-300 p-3 dark:border-zinc-600">
        <legend class="px-1 text-sm font-semibold">On a schedule</legend>
        <div class="flex flex-wrap items-end gap-2">
          <div>
            <label class="label" for="cron-{i}">When</label>
            <input id="cron-{i}" class="input font-mono" required aria-describedby="cron-help-{i}" bind:value={t.cron} />
          </div>
          <div>
            <label class="label" for="zone-{i}">Time zone</label>
            <input id="zone-{i}" class="input" placeholder="UTC" bind:value={t.time_zone} />
          </div>
        </div>
        <p id="cron-help-{i}" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
          Minute, hour, day of the month, month and day of the week, as in cron: <code>0 9 * * 1</code> is 9:00 on Mondays.
        </p>
        <p class="mt-2 text-sm">For the assets it is offered on that are…</p>
        <div class="flex flex-wrap items-end gap-3">
          <label class="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={t.filter?.running_low ?? false} onchange={(e) => (t.filter = { ...t.filter, running_low: e.currentTarget.checked })} />
            Running low
          </label>
          <div>
            <label class="label" for="loan-{i}">Loans</label>
            <select id="loan-{i}" class="input" value={t.filter?.loan ?? ''} onchange={(e) => (t.filter = { ...t.filter, loan: e.currentTarget.value as '' | 'open' | 'overdue' })}>
              <option value="">Either way</option>
              <option value="open">Lent out</option>
              <option value="overdue">Overdue</option>
            </select>
          </div>
          <div>
            <label class="label" for="condition-{i}">In condition</label>
            <select id="condition-{i}" class="input" value={t.filter?.condition ?? ''} onchange={(e) => (t.filter = { ...t.filter, condition: e.currentTarget.value })}>
              <option value="">Any</option>
              <option value="ok">OK</option>
              <option value="needs_maintenance">Needs maintenance</option>
              <option value="out_of_service">Out of service</option>
              <option value="problem">Any problem</option>
            </select>
          </div>
        </div>
        <button type="button" class="btn-secondary mt-2" onclick={() => remove(i)}>Remove this schedule</button>
      </fieldset>
    {/if}
  {/each}

  <div class="flex flex-wrap gap-2">
    <button type="button" class="btn-secondary" onclick={() => add({ type: 'event', kinds: [] })}>Add events</button>
    <button type="button" class="btn-secondary" onclick={() => add({ type: 'schedule', cron: '0 9 * * 1', time_zone: browserZone, filter: {} })}>Add a schedule</button>
  </div>
</fieldset>
