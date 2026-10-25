<!--
  The rest of the lexicon block bam writes (README §3.7): the stroke
  widths allowed, the tokens only a grant may paint with, and the
  compiler's limits. Settings no icon could keep to are refused.
-->
<script lang="ts">
  import { ApiError, request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Category, CategoryIcon, IconDiag } from '../../lib/models';
  import { iconLimits, parseWidths } from '../../lib/icons';
  import IconDiagList from '../../components/IconDiagList.svelte';

  interface IconSettings {
    stroke_widths: number[];
    reserved: string[];
    limits: Record<string, number>;
  }
  interface Props {
    settings: IconSettings;
    tokens: string[];
    canManage: boolean;
    categories: Category[];
    icons: CategoryIcon[];
    onsaved: () => void;
  }
  let { settings, tokens, canManage, categories, icons, onsaved }: Props = $props();

  let widths = $state('');
  let reserved = $state<string[]>([]);
  // each limit: '' for the compiler's default, 'none' for no limit, or a number
  let limits = $state<Record<string, string>>({});
  let error = $state('');
  let refusal = $state<IconDiag[]>([]);
  let status = $state('');

  $effect(() => {
    widths = settings.stroke_widths.join(', ');
    reserved = [...settings.reserved];
    limits = Object.fromEntries(iconLimits.map((l) => {
      const v = settings.limits[l.key];
      return [l.key, v === undefined ? '' : v === 0 ? 'none' : String(v)];
    }));
  });

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    refusal = [];
    const parsed = parseWidths(widths);
    if ('error' in parsed) {
      error = `Stroke widths: ${parsed.error}.`;
      return;
    }
    const out: Record<string, number> = {};
    for (const l of iconLimits) {
      const v = limits[l.key] ?? '';
      if (v === 'none') out[l.key] = 0;
      else if (v !== '') out[l.key] = Number(v);
    }
    try {
      await request('PUT', '/icons/settings', { stroke_widths: parsed.widths, reserved, limits: out });
      status = 'The settings are saved.';
      onsaved();
    } catch (err) {
      error = messageOf(err);
      const details = err instanceof ApiError ? (err.body as { details?: unknown })?.details : undefined;
      if (Array.isArray(details)) refusal = details as IconDiag[];
    }
  }
</script>

<section class="card max-w-3xl" aria-labelledby="settings-heading">
  <h2 id="settings-heading" class="text-lg font-semibold">Settings</h2>
  <p class="mb-3 text-sm text-zinc-600 dark:text-zinc-400">What every icon is held to.</p>
  <form class="space-y-4" onsubmit={save}>
    <div>
      <label class="label" for="icon-widths">Stroke widths</label>
      <input id="icon-widths" class="input max-w-sm font-mono" bind:value={widths} disabled={!canManage} aria-describedby="icon-widths-help" />
      <p id="icon-widths-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">The widths lines may be drawn at, such as 1.2, 1.5. Empty for any.</p>
    </div>
    <fieldset>
      <legend class="label">Reserved colors</legend>
      <p class="mb-1 text-xs text-zinc-600 dark:text-zinc-400">Only an icon that grants itself one, saying why, may paint with it: for a warning red, say.</p>
      <div class="flex flex-wrap gap-x-4 gap-y-1">
        {#each tokens as t (t)}
          <label class="flex items-center gap-1 text-sm font-mono"><input type="checkbox" value={t} bind:group={reserved} disabled={!canManage} />{t}</label>
        {/each}
      </div>
    </fieldset>
    <fieldset>
      <legend class="label">Limits</legend>
      <p class="mb-1 text-xs text-zinc-600 dark:text-zinc-400">How much the compiler does before it stops an icon. Empty for its default.</p>
      <div class="grid gap-2 sm:grid-cols-2">
        {#each iconLimits as l (l.key)}
          <div>
            <label class="block text-sm" for="limit-{l.key}">{l.label}</label>
            <div class="flex items-center gap-2">
              <input
                id="limit-{l.key}"
                class="input-sm w-32 tabular-nums"
                inputmode="numeric"
                pattern="[0-9]*|none"
                placeholder={l.default.toLocaleString()}
                value={limits[l.key] === 'none' ? '' : limits[l.key]}
                disabled={!canManage || limits[l.key] === 'none'}
                oninput={(e) => (limits[l.key] = (e.currentTarget as HTMLInputElement).value)}
              />
              <label class="flex items-center gap-1 text-xs">
                <input type="checkbox" checked={limits[l.key] === 'none'} disabled={!canManage} onchange={(e) => (limits[l.key] = (e.currentTarget as HTMLInputElement).checked ? 'none' : '')} />
                No limit
              </label>
            </div>
          </div>
        {/each}
      </div>
    </fieldset>
    {#if error}
      <div role="alert" class="text-sm text-red-700 dark:text-red-400">
        <p>{error}</p>
        {#if refusal.length}<IconDiagList diags={refusal} {categories} {icons} />{/if}
      </div>
    {/if}
    {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
    {#if canManage}<button class="btn-primary">Save the settings</button>{/if}
  </form>
</section>
