<!--
  /icons (README §3.7): every icon, what needs attention, and what all
  icons are drawn with: the library, the palette and the settings; the
  lexicon as a zip, out and in.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { ApiError, request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Category, CategoryIcon, IconDiag, IconLibraryFile, IconSave } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import type { IrDocument } from '../../lib/isolex/ir';
  import type { IconColor } from '../../lib/isolex/palette';
  import { outline } from '../../lib/isolex/syntax';
  import type { Vocabulary } from '../../lib/isolex/complete';
  import { iconOwner, isWrapper } from '../../lib/icons';
  import { iconset } from '../../lib/iconset.svelte';
  import IsoIcon from '../../components/IsoIcon.svelte';
  import IconPaletteStyle from '../../components/IconPaletteStyle.svelte';
  import IconDiagList from '../../components/IconDiagList.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import LibrarySection from './LibrarySection.svelte';
  import PaletteSection from './PaletteSection.svelte';
  import SettingsSection from './SettingsSection.svelte';
  import StatesSection from './StatesSection.svelte';
  import type { IconStates } from '../../lib/iconStates';

  let { params: _params }: { params: Record<string, string> } = $props();

  interface Status {
    available: boolean;
    isolex_version: string;
    build: { built_at: string; isolex_version: string; warnings: IconDiag[]; icons: { name: string; category: string }[] } | null;
    /** Names assets have as their own icon that the build does not draw. */
    missing_icons?: { name: string; assets: number }[];
  }

  let loading = $state(true);
  let error = $state('');
  let status = $state<Status | null>(null);
  let build = $state<IrDocument | null>(null);
  let palette = $state<IconColor[]>([]);
  let settings = $state<{ stroke_widths: number[]; reserved: string[]; limits: Record<string, number> }>({ stroke_widths: [], reserved: [], limits: {} });
  let icons = $state<CategoryIcon[]>([]);
  let library = $state<IconLibraryFile[]>([]);
  let states = $state<IconStates | null>(null);
  let categories = $state<Category[]>([]);
  let search = $state('');

  const canManage = $derived(session.can('icons.manage') && !!status?.available);

  async function load() {
    try {
      const [s, ir, pal, set, list, lib, cats, st] = await Promise.all([
        request<Status>('GET', '/icons'),
        request<IrDocument>('GET', '/icons/ir'),
        request<{ palette: IconColor[] }>('GET', '/icons/palette'),
        request<{ settings: typeof settings }>('GET', '/icons/settings'),
        request<{ icons: CategoryIcon[] }>('GET', '/category-icons'),
        request<{ files: IconLibraryFile[] }>('GET', '/icon-library'),
        request<{ categories: Category[] }>('GET', '/categories'),
        request<IconStates>('GET', '/icons/states'),
      ]);
      status = s;
      build = ir;
      palette = pal.palette ?? [];
      settings = set.settings;
      icons = list.icons ?? [];
      library = lib.files ?? [];
      categories = cats.categories ?? [];
      states = st;
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);
  // after a change, every page's icons too
  async function changed() {
    await load();
    void iconset.refresh();
  }

  const vocabulary = $derived<Vocabulary>({
    tokens: [...palette.map((c) => c.token), 'category'],
    definitions: [...icons, ...library].flatMap((s) => (s.source ? outline(s.source) : [])),
    icons: build?.icons.map((i) => i.name) ?? [],
  });

  const drafts = $derived(icons.filter((i) => i.draft));
  const libraryDrafts = $derived(library.filter((f) => f.draft));
  const warnings = $derived(status?.build?.warnings ?? []);
  const missing = $derived(status?.missing_icons ?? []);
  // an image of an icon, to download
  const imageUrl = (name: string, format: 'svg' | 'png') => `/api/v1/icons/image?${new URLSearchParams({ name, format, height: '256' })}`;

  /** The gallery: icons by isolex category, as searched. */
  const groups = $derived.by(() => {
    const q = search.trim().toLowerCase();
    const by = new Map<string, NonNullable<IrDocument>['icons']>();
    for (const icon of build?.icons ?? []) {
      if (isWrapper(icon.name)) continue;
      if (q && !icon.name.toLowerCase().includes(q) && !icon.describe.join(' ').toLowerCase().includes(q)) continue;
      by.set(icon.category, [...(by.get(icon.category) ?? []), icon]);
    }
    return [...by.entries()].sort(([a], [b]) => a.localeCompare(b));
  });
  const categoryOf = (id: string) => categories.find((c) => c.id === id);

  // Import: a zip of .ilx files into the library.
  let archive = $state<FileList | undefined>();
  let importing = $state(false);
  let imported = $state('');
  let importError = $state('');
  let importRefusal = $state<IconDiag[]>([]);

  async function importArchive(event: SubmitEvent) {
    event.preventDefault();
    const file = archive?.[0];
    if (!file) return;
    importing = true;
    imported = importError = '';
    importRefusal = [];
    try {
      const form = new FormData();
      form.append('file', file);
      const out = await request<{ save: IconSave; created: number; updated: number; skipped: { path: string; reason: string }[] }>('POST', '/icon-library/import', form);
      const skipped = out.skipped?.length ? ` Skipped ${out.skipped.length}: ${out.skipped.map((s) => `${s.path} (${s.reason})`).join('; ')}.` : '';
      imported = `Imported ${out.created} new file${out.created === 1 ? '' : 's'} and replaced ${out.updated}.${skipped}`;
      await changed();
    } catch (err) {
      importError = messageOf(err);
      const details = err instanceof ApiError ? (err.body as { details?: unknown })?.details : undefined;
      if (Array.isArray(details) && details.every((d) => typeof d === 'object' && d && 'line' in d)) importRefusal = details as IconDiag[];
    } finally {
      importing = false;
    }
  }
</script>

<IconPaletteStyle {palette} />

<h1 class="mb-1 text-2xl font-bold">Icons</h1>
<p class="mb-4 max-w-3xl text-sm text-zinc-600 dark:text-zinc-400">
  Each category's icon is drawn on its own page. Here are all the icons, what needs attention, and what every icon is drawn with. See
  <a class="underline" href="/help/icons">Icons</a> in help.
</p>
<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else if status}
  <div class="space-y-6">
    <section class="card max-w-3xl" aria-labelledby="status-heading">
      <h2 id="status-heading" class="text-lg font-semibold">The build</h2>
      <p class="text-sm">
        {#if status.build}
          {status.build.icons.length} icon{status.build.icons.length === 1 ? '' : 's'}, built {new Date(status.build.built_at).toLocaleString()} by isolex {status.build.isolex_version}.
        {:else}
          Nothing built yet: no category has an icon.
        {/if}
        {#if !status.available}This server cannot compile icons, so they cannot change here.{/if}
      </p>
      <div class="mt-3 flex flex-wrap items-end gap-4">
        <a class="btn-secondary" href="/api/v1/icons/export" download>Export as a zip</a>
        {#if canManage}
          <form class="flex flex-wrap items-end gap-2" onsubmit={importArchive}>
            <div>
              <label class="label" for="icon-archive">Import library files from a zip</label>
              <input id="icon-archive" type="file" accept=".zip,application/zip" class="text-sm" bind:files={archive} />
            </div>
            <button class="btn-secondary" disabled={importing || !archive?.length}>{importing ? 'Importing…' : 'Import'}</button>
          </form>
        {/if}
      </div>
      {#if imported}<p role="status" class="mt-2 text-sm text-green-700 dark:text-green-400">{imported}</p>{/if}
      {#if importError}
        <div role="alert" class="mt-2 text-sm text-red-700 dark:text-red-400">
          <p>{importError}</p>
          {#if importRefusal.length}<IconDiagList diags={importRefusal} {categories} {icons} />{/if}
        </div>
      {/if}
    </section>

    <section class="card max-w-3xl" aria-labelledby="attention-heading">
      <h2 id="attention-heading" class="text-lg font-semibold">Needs attention</h2>
      {#if !drafts.length && !libraryDrafts.length && !warnings.length && !missing.length}
        <p class="text-sm">Nothing: every icon is published as it was last saved, the build has no warnings, and every asset's own icon is drawn.</p>
      {:else}
        <ul class="mt-1 space-y-1 text-sm">
          {#each drafts as d (d.category_id)}
            {@const c = categoryOf(d.category_id)}
            <li>
              <span aria-hidden="true">⚠ </span><a class="underline" href="/categories/{encodeURIComponent(d.category_id)}">{c?.name ?? d.icon_name}</a>:
              a draft{d.source ? '' : ', never published,'} with {d.draft_errors.length} problem{d.draft_errors.length === 1 ? '' : 's'}.
            </li>
          {/each}
          {#each libraryDrafts as f (f.id)}
            <li><span aria-hidden="true">⚠ </span><span class="font-mono">{f.path}</span>, in the library: a draft with {f.draft_errors.length} problem{f.draft_errors.length === 1 ? '' : 's'}.</li>
          {/each}
          {#each missing as m (m.name)}
            <li>
              <span aria-hidden="true">⚠ </span><a class="underline" href="/assets?icon={encodeURIComponent(m.name)}">{m.assets} asset{m.assets === 1 ? '' : 's'}</a>
              {m.assets === 1 ? 'has' : 'have'} the icon <span class="font-mono">{m.name}</span> as {m.assets === 1 ? 'its' : 'their'} own, which nothing draws, so {m.assets === 1 ? 'it shows' : 'they show'} as unknown.
            </li>
          {/each}
        </ul>
        {#if warnings.length}
          <h3 class="mt-3 text-sm font-medium">Warnings</h3>
          <div class="text-sm"><IconDiagList diags={warnings} {categories} {icons} /></div>
        {/if}
      {/if}
    </section>

    <section aria-labelledby="gallery-heading">
      <h2 id="gallery-heading" class="text-lg font-semibold">Every icon</h2>
      {#if build?.icons.length}
        <div class="my-2 max-w-sm">
          <label class="sr-only" for="icon-search">Search icons</label>
          <input id="icon-search" class="input" type="search" placeholder="Search by name or description" bind:value={search} />
        </div>
        {#each groups as [group, list] (group)}
          <h3 class="mt-3 mb-1 text-sm font-medium">{group}</h3>
          <ul class="flex flex-wrap gap-3">
            {#each list as icon (icon.name)}
              {@const owner = iconOwner(icons, icon.name)}
              {@const cat = owner ? categoryOf(owner.category_id) : undefined}
              <li class="w-28 text-center text-xs" title={icon.describe.join(' ')}>
                <div class="grid h-20 place-items-center rounded border border-zinc-200 bg-white dark:border-zinc-700 dark:bg-zinc-950">
                  <IsoIcon {icon} height={56} label={icon.name} categoryColor={cat?.color_hex ?? ''} />
                </div>
                <span class="mt-1 block truncate font-mono">{icon.name}</span>
                <span class="block">
                  <a class="underline" href={imageUrl(icon.name, 'svg')} download="{icon.name}.svg" aria-label="{icon.name} as SVG">SVG</a> ·
                  <a class="underline" href={imageUrl(icon.name, 'png')} download="{icon.name}.png" aria-label="{icon.name} as PNG">PNG</a>
                </span>
                {#if cat}<a class="block truncate underline" href="/categories/{encodeURIComponent(cat.id)}">{cat.name}</a>{/if}
              </li>
            {/each}
          </ul>
        {:else}
          <p class="text-sm">No icon matches.</p>
        {/each}
      {:else}
        <p class="text-sm">No icons yet. Draw one on a category's page.</p>
      {/if}
    </section>

    {#if states}
      <StatesSection {states} names={build?.icons.map((i) => i.name).filter((n) => !isWrapper(n)) ?? []} canManage={session.can('icons.manage')} onsaved={changed} />
    {/if}
    <LibrarySection files={library} {canManage} {vocabulary} {categories} {icons} starter={states?.starter ?? ''} onsaved={changed} />
    <PaletteSection {palette} {canManage} {categories} {icons} onsaved={changed} />
    <SettingsSection {settings} tokens={palette.map((c) => c.token)} {canManage} {categories} {icons} onsaved={changed} />

    <p class="max-w-3xl text-xs text-zinc-600 dark:text-zinc-400">
      Icons are compiled by <a class="underline" href="https://forge.axonibyte.com/axonibyte/isolex">isolex</a>, the work of Axonibyte Innovations, LLC, under the
      <a class="underline" href="https://forge.axonibyte.com/axonibyte/isolex/src/branch/main/LICENSE">Apache License 2.0</a>.
    </p>
  </div>
{/if}
