<!--
  The palette (README §3.7): the tokens icons may paint with, each with a
  light and a dark color. Taking away a token an icon uses is refused,
  and the refusal says which.
-->
<script lang="ts">
  import { ApiError, request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Category, CategoryIcon, IconDiag } from '../../lib/models';
  import type { IconColor } from '../../lib/isolex/palette';
  import IconDiagList from '../../components/IconDiagList.svelte';

  interface Props {
    palette: IconColor[];
    canManage: boolean;
    categories: Category[];
    icons: CategoryIcon[];
    onsaved: () => void;
  }
  let { palette, canManage, categories, icons, onsaved }: Props = $props();

  let rows = $state<IconColor[]>([]);
  let error = $state('');
  let refusal = $state<IconDiag[]>([]);
  let status = $state('');
  let saving = $state(false);

  $effect(() => {
    rows = palette.map((c) => ({ ...c }));
  });

  const changed = $derived(JSON.stringify(rows) !== JSON.stringify(palette));
  const colorPattern = '#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})';
  const hex = /^#(?:[0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/iu;
  // A color input takes #rrggbb only.
  const six = (c: string) => (/^#[0-9a-f]{6}$/iu.test(c) ? c : /^#[0-9a-f]{3}$/iu.test(c) ? '#' + [...c.slice(1)].map((x) => x + x).join('') : c.slice(0, 7));

  function move(i: number, by: number) {
    const j = i + by;
    if (j < 0 || j >= rows.length) return;
    [rows[i], rows[j]] = [rows[j]!, rows[i]!];
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    refusal = [];
    saving = true;
    try {
      await request('PUT', '/icons/palette', { palette: rows });
      status = 'The palette is saved.';
      onsaved();
    } catch (err) {
      error = messageOf(err);
      const details = err instanceof ApiError ? (err.body as { details?: unknown })?.details : undefined;
      if (Array.isArray(details) && details.every((d) => typeof d === 'object' && d && 'file' in d)) refusal = details as IconDiag[];
    } finally {
      saving = false;
    }
  }
</script>

<section class="card max-w-3xl" aria-labelledby="palette-heading">
  <h2 id="palette-heading" class="text-lg font-semibold">Palette</h2>
  <p class="mb-3 text-sm text-zinc-600 dark:text-zinc-400">
    The colors icons paint with, by name, in light and in dark mode. Icons name a color, never paint one of their own; <span class="font-mono">category</span> paints in each category's own color.
  </p>
  <form onsubmit={save}>
    <table class="w-full text-sm">
      <thead>
        <tr class="text-left">
          <th scope="col" class="pb-1 font-medium">Token</th>
          <th scope="col" class="pb-1 font-medium">Light</th>
          <th scope="col" class="pb-1 font-medium">Dark</th>
          {#if canManage}<th scope="col" class="pb-1"><span class="sr-only">Order and removal</span></th>{/if}
        </tr>
      </thead>
      <tbody>
        {#each rows as row, i (i)}
          <tr>
            <td class="py-1 pr-2">
              {#if canManage}
                <input class="input-sm font-mono" aria-label="Token {i + 1}" bind:value={row.token} required pattern="[a-z][a-z0-9\-]*" maxlength="64" />
              {:else}<span class="font-mono">{row.token}</span>{/if}
            </td>
            {#each ['light', 'dark'] as const as mode (mode)}
              <td class="py-1 pr-2">
                <div class="flex items-center gap-1">
                  <span class="inline-block h-6 w-6 shrink-0 rounded border border-zinc-300 dark:border-zinc-600" style="background-color: {hex.test(row[mode]) ? row[mode] : 'transparent'}" aria-hidden="true"></span>
                  {#if canManage}
                    <input type="color" class="h-7 w-9 cursor-pointer rounded border border-zinc-300 bg-transparent dark:border-zinc-600" aria-label="{row.token || 'Token ' + (i + 1)}, {mode}, chosen" value={six(row[mode])} oninput={(e) => (row[mode] = (e.currentTarget as HTMLInputElement).value)} />
                    <input class="input-sm w-24 font-mono" aria-label="{row.token || 'Token ' + (i + 1)}, {mode}" bind:value={row[mode]} required pattern={colorPattern} />
                  {:else}<span class="font-mono">{row[mode]}</span>{/if}
                </div>
              </td>
            {/each}
            {#if canManage}
              <td class="py-1 whitespace-nowrap">
                <button type="button" class="icon-btn" aria-label="Move {row.token} up" disabled={i === 0} onclick={() => move(i, -1)}>↑</button>
                <button type="button" class="icon-btn" aria-label="Move {row.token} down" disabled={i === rows.length - 1} onclick={() => move(i, 1)}>↓</button>
                <button type="button" class="icon-btn" aria-label="Remove {row.token}" onclick={() => rows.splice(i, 1)}>✕</button>
              </td>
            {/if}
          </tr>
        {/each}
      </tbody>
    </table>
    {#if error}
      <div role="alert" class="mt-2 text-sm text-red-700 dark:text-red-400">
        <p>{error}</p>
        {#if refusal.length}<IconDiagList diags={refusal} {categories} {icons} />{/if}
      </div>
    {/if}
    {#if status}<p role="status" class="mt-2 text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
    {#if canManage}
      <div class="mt-3 flex flex-wrap gap-2">
        <button type="button" class="btn-secondary" onclick={() => rows.push({ token: '', light: '#000000', dark: '#ffffff' })}>Add a token</button>
        <button class="btn-primary" disabled={saving || !changed}>{saving ? 'Saving…' : 'Save the palette'}</button>
        {#if changed}<button type="button" class="btn-secondary" onclick={() => (rows = palette.map((c) => ({ ...c })))}>Undo changes</button>{/if}
      </div>
    {/if}
  </form>
</section>
