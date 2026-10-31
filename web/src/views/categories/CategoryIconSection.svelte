<!--
  A category's icon (README §3.7): what it shows, its own or inherited;
  and, for those who manage categories, its isolex source to write, checked
  as it is typed, previewed in light and dark beside its subcategories,
  published or kept as a draft with its problems, with its history. A tab
  for each rendition: its icon on screen, and on labels, previewed as it
  prints.
-->
<script lang="ts">
  import { iconset } from '../../lib/iconset.svelte';
  import { onMount } from 'svelte';
  import { ApiError, request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { Category, CategoryIcon, IconCheck, IconDiag, IconLibraryFile, IconRevision, IconSave, Rendition } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import { outline, type Problem } from '../../lib/isolex/syntax';
  import type { Vocabulary } from '../../lib/isolex/complete';
  import { iconNamed, type IrDocument } from '../../lib/isolex/ir';
  import type { IconColor } from '../../lib/isolex/palette';
  import { boxSource, copiedSource, extendingSource, iconFile, iconGroup, shownIcon, splitDiags, suggestIconName, validIconName } from '../../lib/icons';
  import IconSourceEditor from '../../components/IconSourceEditor.svelte';
  import IsoIcon from '../../components/IsoIcon.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import IconParams from './IconParams.svelte';
  import IconPaletteStyle from '../../components/IconPaletteStyle.svelte';
  import LabelIconPreview from './LabelIconPreview.svelte';

  interface Props {
    category: Category;
    categories: Category[];
    /** Whether the source in the editor differs from what is saved. */
    unsaved?: boolean;
  }

  let { category, categories, unsaved = $bindable(false) }: Props = $props();

  const id = $derived(category.id);
  let loading = $state(true);
  let error = $state('');
  let message = $state('');
  let available = $state(false);
  let build = $state<IrDocument | null>(null);
  let palette = $state<IconColor[]>([]);
  let icons = $state<CategoryIcon[]>([]);
  let library = $state<IconLibraryFile[]>([]);

  let editing = $state(false);
  let text = $state('');
  let name = $state('');
  let problems = $state<Problem[]>([]);
  let elsewhere = $state<(IconDiag & { severity: 'error' | 'warning' })[]>([]);
  let preview = $state<IrDocument | null>(null);
  let checking = $state(false);
  let saving = $state(false);
  let copyFrom = $state('');
  let renameTo = $state('');
  let revisions = $state<IconRevision[] | null>(null);
  let refusal = $state<IconDiag[]>([]);

  /** Which of its icons the section shows: on screen, or on labels. */
  let rendition = $state<Rendition>('screen');
  const renditions: { value: Rendition; label: string }[] = [
    { value: 'screen', label: 'On screen' },
    { value: 'label', label: 'On labels' },
  ];
  /** A category icon route, for the rendition shown. */
  const route = (rest = '') => `/categories/${encodeURIComponent(id)}/icon${rest}${rendition === 'screen' ? '' : `?rendition=${rendition}`}`;
  const renditionOf = (i: CategoryIcon) => i.rendition ?? 'screen';

  const own = $derived(icons.find((i) => i.category_id === id && renditionOf(i) === rendition) ?? null);
  const screenOwn = $derived(icons.find((i) => i.category_id === id && renditionOf(i) === 'screen') ?? null);
  const savedText = $derived(own?.draft ?? own?.source ?? '');
  const canEdit = $derived(session.can('categories.manage') && available);
  const shown = $derived(shownIcon(categories, icons, id, rendition));
  const parentShown = $derived(category.parent_id ? shownIcon(categories, icons, category.parent_id, rendition) : null);
  const nameOf = (catId: string) => categories.find((c) => c.id === catId)?.name ?? 'another category';
  const file = $derived(iconFile(name));
  const doc = $derived(preview ?? build);
  const children = $derived(categories.filter((c) => c.parent_id === id));
  const withIcons = $derived(icons.filter((i) => i.source !== null && !(i.category_id === id && renditionOf(i) === rendition)));
  const iconKey = (i: CategoryIcon) => `${i.category_id}/${renditionOf(i)}`;
  const errorCount = $derived(problems.filter((p) => p.severity === 'error').length + elsewhere.filter((d) => d.severity === 'error').length);

  $effect(() => {
    unsaved = editing && text !== savedText;
  });

  /** What completion knows: the palette, and what every other source defines. */
  const vocabulary = $derived<Vocabulary>({
    tokens: [...palette.map((c) => c.token), 'category'],
    definitions: [
      ...icons.filter((i) => !(i.category_id === id && renditionOf(i) === rendition) && i.source).flatMap((i) => outline(i.source!)),
      ...library.filter((f) => f.source).flatMap((f) => outline(f.source!)),
    ],
    icons: build?.icons.map((i) => i.name) ?? [],
  });

  /** Variants and other icons this source draws besides the category's. */
  const alsoDrawn = $derived.by(() => {
    if (!doc) return [];
    const definedElsewhere = new Set(vocabulary.definitions.map((d) => d.name));
    return doc.icons.filter((i) => i.name !== name && i.name.startsWith(name + '-') && !definedElsewhere.has(i.name));
  });

  onMount(() => {
    void load();
  });

  async function load() {
    error = '';
    try {
      const [status, ir, pal, list, lib] = await Promise.all([
        request<{ available: boolean }>('GET', '/icons'),
        request<IrDocument>('GET', '/icons/ir'),
        request<{ palette: IconColor[] }>('GET', '/icons/palette'),
        request<{ icons: CategoryIcon[] }>('GET', '/category-icons'),
        request<{ files: IconLibraryFile[] }>('GET', '/icon-library'),
      ]);
      available = status.available;
      build = ir;
      palette = pal.palette ?? [];
      icons = list.icons ?? [];
      library = lib.files ?? [];
      if (!editing) reset();
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }

  /** The editor as saved: the draft and its errors, or what is published. */
  function reset() {
    text = savedText;
    // a label icon is named for the screen icon, by convention
    const base = rendition === 'label' ? `${screenOwn?.icon_name ?? category.name}-label` : category.name;
    name = own?.icon_name ?? suggestIconName(base, icons.map((i) => i.icon_name));
    renameTo = name;
    const split = splitDiags(iconFile(name), own?.draft ? own.draft_errors : [], []);
    problems = split.here;
    elsewhere = split.elsewhere;
    preview = null;
  }

  /** Shows another rendition's icon, leaving the one being edited. */
  function show(next: Rendition) {
    if (next === rendition) return;
    rendition = next;
    editing = false;
    revisions = null;
    refusal = [];
    copyFrom = '';
    error = message = '';
    reset();
  }

  /** Arrow keys move between the tabs, as a tab list's do. */
  function tabKey(event: KeyboardEvent) {
    const at = renditions.findIndex((r) => r.value === rendition);
    const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const next = renditions[(at + step + renditions.length) % renditions.length]!;
    show(next.value);
    document.getElementById(`icon-tab-${next.value}`)?.focus();
  }

  function start(source: string) {
    text = source;
    editing = true;
    message = '';
  }

  // Checked as it is typed, a moment after the typing stops.
  let timer: ReturnType<typeof setTimeout> | undefined;
  let asked = 0;
  $effect(() => {
    const [t, n, on] = [text, name, editing && canEdit];
    clearTimeout(timer);
    if (!on || !t.trim() || !validIconName(n)) return;
    timer = setTimeout(() => void check(t, n), 500);
    return () => clearTimeout(timer);
  });

  async function check(source: string, iconName: string) {
    const seq = ++asked;
    checking = true;
    try {
      const { check } = await request<{ check: IconCheck }>('POST', route('/check'), { icon_name: iconName, source });
      if (seq !== asked) return;
      const split = splitDiags(iconFile(iconName), check.errors, check.warnings);
      problems = split.here;
      elsewhere = split.elsewhere;
      preview = check.ir ?? null;
    } catch (err) {
      if (seq === asked) error = messageOf(err);
    } finally {
      if (seq === asked) checking = false;
    }
  }

  async function save() {
    if (!validIconName(name)) {
      error = "An icon's name is 1 to 64 letters, digits, -, _ and ., not starting with . or bam-.";
      return;
    }
    saving = true;
    error = message = '';
    try {
      const out = await request<{ icon: CategoryIcon; save: IconSave }>('PUT', route(), { icon_name: name, source: text });
      icons = [...icons.filter((i) => !(i.category_id === id && renditionOf(i) === rendition)), out.icon];
      const split = splitDiags(file, out.save.errors, out.save.warnings);
      problems = split.here;
      elsewhere = split.elsewhere;
      if (out.save.published) {
        message = 'Published.';
        build = await request<IrDocument>('GET', '/icons/ir');
        preview = null;
      } else {
        const n = out.save.errors.length;
        message = `Kept as a draft: ${n} problem${n === 1 ? '' : 's'} to fix before it is published. ${own?.source ? 'The published icon is still the one shown.' : ''}`.trim();
      }
    } catch (err) {
      error = messageOf(err);
    } finally {
      saving = false;
    }
  }

  async function discard() {
    error = message = '';
    try {
      await request('DELETE', route('/draft'));
      editing = false;
      await load();
      void iconset.refresh();
      reset();
      message = own ? 'The draft is discarded.' : 'The draft is discarded; this category has no icon of its own.';
    } catch (err) {
      error = messageOf(err);
    }
  }

  function cancel() {
    editing = false;
    reset();
  }

  /** A refusal's errors, which say what still uses what was to go. */
  function refused(err: unknown) {
    error = messageOf(err);
    const details = err instanceof ApiError ? (err.body as { details?: IconDiag[] } | undefined)?.details : undefined;
    refusal = Array.isArray(details) ? details : [];
  }

  async function rename(event: SubmitEvent) {
    event.preventDefault();
    error = message = '';
    refusal = [];
    try {
      await request('POST', route('/rename'), { icon_name: renameTo });
      await load();
      void iconset.refresh();
      reset();
      message = `Renamed ${renameTo}.`;
    } catch (err) {
      refused(err);
    }
  }

  async function remove() {
    error = message = '';
    refusal = [];
    try {
      await request('DELETE', route());
      editing = false;
      await load();
      void iconset.refresh();
      reset();
      message = 'The icon is deleted.';
    } catch (err) {
      refused(err);
    }
  }

  async function history(event: Event) {
    if (!(event.currentTarget as HTMLDetailsElement).open || revisions) return;
    try {
      revisions = (await request<{ revisions: IconRevision[] }>('GET', route('/revisions'))).revisions ?? [];
    } catch (err) {
      error = messageOf(err);
    }
  }

  /** Which category a build's file is, for a link to it. */
  function fileCategory(f: string): Category | undefined {
    const icon = icons.find((i) => iconFile(i.icon_name) === f);
    return icon ? categories.find((c) => c.id === icon.category_id) : undefined;
  }

</script>

<IconPaletteStyle {palette} />

{#snippet tile(iconName: string, height: number, theme: 'light' | 'dark' | 'page', label: string, color: string)}
  {@const icon = iconNamed(doc, iconName)}
  <!-- light or dark whatever the page is, to show both; or as the page is -->
  <div
    class="grid place-items-center rounded border {theme === 'dark'
      ? 'iso-dark border-zinc-700 bg-zinc-950'
      : theme === 'light'
        ? 'iso-light border-zinc-200 bg-white'
        : 'border-zinc-200 bg-white dark:border-zinc-700 dark:bg-zinc-950'}"
    style="min-width: {height + 16}px; min-height: {height + 16}px"
  >
    {#if icon}<IsoIcon {icon} {height} {label} categoryColor={color} />{:else}<span class="p-2 text-xs text-zinc-500">none</span>{/if}
  </div>
{/snippet}

<section class="mt-6" aria-labelledby="icon-heading">
  <h2 id="icon-heading" class="mb-1 text-lg font-semibold">Icon</h2>
  <div role="tablist" aria-label="Where the icon is drawn" class="mb-3 flex gap-1 border-b border-zinc-200 dark:border-zinc-700">
    {#each renditions as r (r.value)}
      <button
        type="button"
        role="tab"
        id="icon-tab-{r.value}"
        aria-selected={rendition === r.value}
        aria-controls="icon-panel"
        tabindex={rendition === r.value ? 0 : -1}
        class="-mb-px min-h-11 border-b-2 px-3 py-1.5 text-sm {rendition === r.value
          ? 'border-zinc-900 font-medium dark:border-zinc-100'
          : 'border-transparent text-zinc-600 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100'}"
        onclick={() => show(r.value)}
        onkeydown={tabKey}>{r.label}</button
      >
    {/each}
  </div>
  <div role="tabpanel" id="icon-panel" aria-labelledby="icon-tab-{rendition}">
  {#if rendition === 'label' && !loading}
    <p class="mb-2 max-w-2xl text-sm text-zinc-600 dark:text-zinc-400">
      Labels print in black and white, most often on small thermal tape, where shaded faces turn to noise: a label icon is best flat line art. Without one, labels print the screen icon.
    </p>
  {/if}
  {#if loading}
    <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
  {:else}
    {#if error}
      <div role="alert" class="mb-2 text-sm text-red-700 dark:text-red-400">
        <p>{error}</p>
        {#if refusal.length}
          <ul class="mt-1 list-disc pl-5">
            {#each refusal as d, k (k)}
              {@const owner = fileCategory(d.file)}
              <li>
                {#if owner}<a class="underline" href="/categories/{encodeURIComponent(owner.id)}">{owner.name}</a>{:else}<span class="font-mono">{d.file}</span>{/if}, line {d.line}: {d.message}
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    {/if}
    {#if message}<p role="status" class="mb-2 text-sm text-green-700 dark:text-green-400">{message}</p>{/if}

    {#if !editing}
      <div class="card flex max-w-2xl flex-wrap items-center gap-4">
        {#if shown && iconNamed(build, shown.name)}
          {@render tile(shown.name, 64, 'page', `${category.name}'s icon`, category.color_hex)}
        {/if}
        <div class="min-w-0 flex-1 text-sm">
          {#if own?.source}
            <p>Its own {rendition === 'label' ? 'label icon' : 'icon'}, <span class="font-mono">{own.icon_name}</span>.</p>
          {:else if shown && rendition === 'label' && shown.rendition === 'screen'}
            <p>
              No label icon here or above, so labels print {shown.from === id ? 'its' : `${nameOf(shown.from)}'s`} screen icon,
              <span class="font-mono">{shown.name}</span>.
            </p>
          {:else if shown}
            <p>The {rendition === 'label' ? 'label icon' : 'icon'} of <a class="underline" href="/categories/{encodeURIComponent(shown.from)}">{nameOf(shown.from)}</a>, which it inherits.</p>
          {:else}
            <p>No icon: it shows none, and neither does anything above it.</p>
          {/if}
          {#if own?.draft}
            <p class="mt-1 text-amber-900 dark:text-amber-200">
              <span aria-hidden="true">⚠ </span>A draft is waiting, with {own.draft_errors.length} problem{own.draft_errors.length === 1 ? '' : 's'}.
            </p>
          {/if}
          {#if !available}
            <p class="mt-1 text-zinc-600 dark:text-zinc-400">This server cannot compile icons, so they cannot change here.</p>
          {/if}
        </div>
        {#if canEdit && own}
          <button type="button" class="btn-secondary" onclick={() => start(savedText)}>{own.draft ? 'Edit the draft' : 'Edit the icon'}</button>
        {/if}
      </div>

      {#if own?.source}
        {#key rendition}
          <IconParams categoryId={id} {rendition} iconName={own.icon_name} definitions={[...vocabulary.definitions, ...outline(own.source)]} canEdit={session.can('categories.manage') && available} />
        {/key}
      {/if}

      {#if canEdit && !own}
        <div class="mt-3 max-w-2xl">
          <h3 class="text-sm font-medium">Draw it {rendition === 'label' ? 'a label icon' : 'an icon'} of its own</h3>
          <div class="mt-2 flex flex-wrap items-center gap-2">
            {#if rendition === 'label' && screenOwn?.source}
              <button type="button" class="btn-secondary" onclick={() => start(extendingSource(name, screenOwn.icon_name))}>Extend its screen icon</button>
            {/if}
            {#if parentShown}
              <button type="button" class="btn-secondary" onclick={() => start(extendingSource(name, parentShown.name))}>Extend {nameOf(parentShown.from)}'s {parentShown.rendition === 'label' ? 'label icon' : 'icon'}</button>
            {/if}
            <button type="button" class="btn-secondary" onclick={() => start(boxSource(name, iconGroup(categories, id)))}>Start from a box</button>
            {#if withIcons.length}
              <label class="sr-only" for="icon-copy">Copy another category's icon</label>
              <select id="icon-copy" class="input w-auto" bind:value={copyFrom}>
                <option value="">Copy another category's…</option>
                {#each withIcons as i (iconKey(i))}<option value={iconKey(i)}>{nameOf(i.category_id)}{renditionOf(i) === 'label' ? ', on labels' : ''}</option>{/each}
              </select>
              <button
                type="button"
                class="btn-secondary"
                disabled={!copyFrom}
                onclick={() => {
                  const from = withIcons.find((i) => iconKey(i) === copyFrom)!;
                  start(copiedSource(from.source!, from.icon_name, name));
                }}>Copy</button
              >
            {/if}
          </div>
        </div>
      {/if}
    {:else}
      <div class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <div class="min-w-0">
          {#if own}
            <p class="mb-2 text-sm">Icon <span class="font-mono">{own.icon_name}</span></p>
          {:else}
            <div class="mb-3 max-w-sm">
              <label class="label" for="icon-name">Its name</label>
              <input id="icon-name" class="input font-mono" bind:value={name} aria-describedby="icon-name-help" aria-invalid={!validIconName(name)} />
              <p id="icon-name-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">How other icons name it. Letters, digits, -, _ and .; it can be renamed later.</p>
            </div>
          {/if}
          <IconSourceEditor bind:value={text} {problems} {vocabulary} label="{category.name}'s {rendition === 'label' ? 'label icon' : 'icon'} source" readonly={!canEdit} />
          {#if elsewhere.length}
            <div class="mt-3 text-sm">
              <h3 class="font-medium">Problems in other icons, caused by this</h3>
              <ul class="mt-1 list-disc pl-5">
                {#each elsewhere as d, k (k)}
                  {@const owner = fileCategory(d.file)}
                  <li>
                    {#if owner}<a class="underline" href="/categories/{encodeURIComponent(owner.id)}">{owner.name}</a>{:else}<span class="font-mono">{d.file}</span>{/if}, line {d.line}: {d.message}
                  </li>
                {/each}
              </ul>
            </div>
          {/if}
          <div class="mt-4 flex flex-wrap items-center gap-2">
            <button type="button" class="btn-primary" disabled={saving || !text.trim()} onclick={save}>{saving ? 'Saving…' : 'Save'}</button>
            <button type="button" class="btn-secondary" onclick={cancel}>Close</button>
            {#if own?.draft}
              <ConfirmButton label="Discard the draft" question={own.source ? 'Go back to the published icon?' : 'Discard it? This category has no published icon.'} onconfirm={discard} />
            {/if}
            <span class="text-sm text-zinc-600 dark:text-zinc-400" role="status">{checking ? 'Checking…' : errorCount ? `${errorCount} problem${errorCount === 1 ? '' : 's'}` : text.trim() ? 'No problems' : ''}</span>
          </div>
        </div>

        <aside aria-label="Preview" class="space-y-4">
          <div>
            <h3 class="mb-2 text-sm font-medium">Preview</h3>
            {#if iconNamed(doc, name)}
              <div class="flex gap-2">
                {@render tile(name, 96, 'light', `${name}, light`, category.color_hex)}
                {@render tile(name, 96, 'dark', `${name}, dark`, category.color_hex)}
              </div>
              <div class="mt-2 flex items-center gap-2 text-xs text-zinc-600 dark:text-zinc-400">
                {@render tile(name, 24, 'light', `${name}, small, light`, category.color_hex)}
                {@render tile(name, 24, 'dark', `${name}, small, dark`, category.color_hex)}
                <span>As in lists</span>
              </div>
              {#if !preview && errorCount}
                <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">The published icon: this source does not compile yet.</p>
              {/if}
              {#if rendition === 'label' && doc}
                <div class="mt-3"><LabelIconPreview {doc} {name} {palette} categoryColor={category.color_hex} /></div>
              {/if}
            {:else}
              <p class="text-sm text-zinc-600 dark:text-zinc-400">{errorCount ? 'Nothing to show until it compiles.' : 'Nothing drawn yet.'}</p>
            {/if}
          </div>
          {#if alsoDrawn.length}
            <div>
              <h3 class="mb-2 text-sm font-medium">Its variants</h3>
              <ul class="flex flex-wrap gap-3">
                {#each alsoDrawn as v (v.name)}
                  <li class="text-center text-xs">
                    {@render tile(v.name, 40, 'page', v.name, category.color_hex)}
                    <span class="font-mono">{v.name.slice(name.length + 1)}</span>
                  </li>
                {/each}
              </ul>
            </div>
          {/if}
          {#if children.length}
            <div>
              <h3 class="mb-2 text-sm font-medium">Its subcategories</h3>
              <ul class="flex flex-wrap gap-3">
                {#each children as c (c.id)}
                  {@const shownHere = shownIcon(categories, icons, c.id, rendition)}
                  <li class="text-center text-xs">
                    {#if shownHere}{@render tile(shownHere.name, 40, 'page', `${c.name}'s icon`, c.color_hex)}{/if}
                    <a class="underline" href="/categories/{encodeURIComponent(c.id)}">{c.name}</a>
                    {#if shownHere && shownHere.from !== c.id}<span class="block text-zinc-600 dark:text-zinc-400">inherited</span>{/if}
                  </li>
                {/each}
              </ul>
            </div>
          {/if}
        </aside>
      </div>

      {#if own?.source}
        <div class="mt-6 grid max-w-2xl gap-4">
          <details ontoggle={history}>
            <summary class="cursor-pointer text-sm font-medium">History</summary>
            {#if revisions === null}
              <p class="mt-1 text-sm text-zinc-600 dark:text-zinc-400" role="status">Loading…</p>
            {:else}
              <ul class="mt-2 space-y-1 text-sm">
                {#each revisions as r, k (r.id)}
                  <li class="flex flex-wrap items-center gap-2">
                    <time datetime={r.created_at}>{new Date(r.created_at).toLocaleString()}</time>
                    {#if k === 0}<span class="text-zinc-600 dark:text-zinc-400">published now</span>{/if}
                    <button type="button" class="btn-sm" onclick={() => start(r.source)}>Load into the editor</button>
                  </li>
                {/each}
              </ul>
            {/if}
          </details>
          {#if canEdit}
            <form class="flex flex-wrap items-end gap-2" onsubmit={rename}>
              <div>
                <label class="label" for="icon-rename">Rename it</label>
                <input id="icon-rename" class="input font-mono" bind:value={renameTo} aria-describedby="icon-rename-help" />
              </div>
              <button class="btn-secondary" disabled={!validIconName(renameTo) || renameTo === own.icon_name}>Rename</button>
              <p id="icon-rename-help" class="w-full text-xs text-zinc-600 dark:text-zinc-400">Its source must already draw it by the new name, and nothing else may still use the old one.</p>
            </form>
            <div>
              <ConfirmButton label="Delete the icon" question="Delete this category's {rendition === 'label' ? 'label icon' : 'icon'} and its history?" onconfirm={remove} />
            </div>
          {/if}
        </div>
      {/if}
    {/if}
  {/if}
  </div>
</section>
