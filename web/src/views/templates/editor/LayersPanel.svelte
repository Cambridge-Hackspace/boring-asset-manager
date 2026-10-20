<script lang="ts">
  import { tick, untrack } from 'svelte';
  import type { Frame } from '../../../lib/live/client';
  import { bounds, kindName, layerName, type TemplateElement } from '../../../lib/template';
  import { useEditor } from './editor.svelte';
  import Icon from './Icon.svelte';

  // The layers, as Krita's layer docker: the top one first, each with a
  // thumbnail of what it draws, an eye that hides it while editing (it
  // still prints), and a lock. A row selects its element (Shift adds it);
  // dragging a row moves it over or under the others; double-click or F2
  // renames it. Groups are folders, opened and closed, named the same way.
  let { problemElements, frame }: { problemElements: Set<number>; frame: Frame | null } = $props();

  const editor = useEditor();
  const doc = $derived(editor.doc);

  type Row = { kind: 'element'; i: number; e: TemplateElement; depth: number } | { kind: 'group'; name: string; members: number[] };
  let closed = $state<string[]>([]);

  /** Rows top first, a group's members gathered under it where its top one is. */
  const rows = $derived.by((): Row[] => {
    const out: Row[] = [];
    const placed = new Set<string>();
    for (let i = doc.elements.length - 1; i >= 0; i--) {
      const e = doc.elements[i]!;
      if (!e.group) {
        out.push({ kind: 'element', i, e, depth: 0 });
        continue;
      }
      if (placed.has(e.group)) continue;
      placed.add(e.group);
      const members = doc.elements.flatMap((m, k) => (m.group === e.group ? [k] : [])).reverse();
      out.push({ kind: 'group', name: e.group, members });
      if (!closed.includes(e.group)) for (const k of members) out.push({ kind: 'element', i: k, e: doc.elements[k]!, depth: 1 });
    }
    return out;
  });

  let dragging = $state<number | null>(null);
  let over = $state<number | null>(null);
  // The row being renamed: an element's index, or a group's name.
  let renaming = $state<number | string | null>(null);

  function choose(event: MouseEvent, i: number) {
    const sel = editor.selected;
    editor.selected = event.shiftKey ? (sel.includes(i) ? sel.filter((s) => s !== i) : [...sel, i]) : [i];
  }

  function drop(event: DragEvent, i: number) {
    event.preventDefault();
    if (dragging !== null && dragging !== i) editor.moveLayer(dragging, i);
    dragging = over = null;
  }

  async function startRename(which: number | string) {
    renaming = which;
    await tick();
    const input = document.getElementById('layer-rename') as HTMLInputElement | null;
    input?.focus();
    input?.select();
  }

  function finishRename(input: HTMLInputElement, keep: boolean) {
    const which = renaming;
    renaming = null;
    if (!keep || which === null) return;
    if (typeof which === 'number') editor.rename(which, input.value);
    else editor.renameGroup(which, input.value);
  }

  function renameKeys(event: KeyboardEvent & { currentTarget: HTMLInputElement }) {
    if (event.key === 'Enter') finishRename(event.currentTarget, true);
    else if (event.key === 'Escape') {
      event.stopPropagation();
      finishRename(event.currentTarget, false);
    }
  }

  const allHidden = (members: number[]) => members.every((k) => editor.hidden.includes(k));
  const allLocked = (members: number[]) => members.every((k) => doc.elements[k]?.locked);

  // The thumbnails are cut from the live drawing of the label.
  let source = $state<HTMLCanvasElement | null>(null);
  let drawnAt = $state(0);
  $effect(() => {
    const f = frame;
    if (!f) return;
    // Read untracked: this sets them, and must not run again for it.
    untrack(() => {
      const c = source ?? document.createElement('canvas');
      c.width = f.width;
      c.height = f.height;
      c.getContext('2d')?.putImageData(new ImageData(new Uint8ClampedArray(f.pixels), f.width, f.height), 0, 0);
      source = c;
      drawnAt++;
    });
  });

  /** Draws what an element covers on the label into a thumbnail, again as either changes. */
  const thumbnail = (e: TemplateElement) => (canvas: HTMLCanvasElement) => {
    void drawnAt;
    const src = source;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    ctx.fillStyle = 'white';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    if (!src) return;
    const b = bounds(e);
    const kx = src.width / doc.width;
    const ky = src.height / doc.height;
    const sw = Math.max(1, b.width * kx);
    const sh = Math.max(1, b.height * ky);
    const k = Math.min(canvas.width / sw, canvas.height / sh);
    ctx.drawImage(src, b.x * kx, b.y * ky, sw, sh, (canvas.width - sw * k) / 2, (canvas.height - sh * k) / 2, sw * k, sh * k);
  };

  const iconButton = 'icon-btn h-6 w-6';
</script>

{#if rows.length}
  <ol class="-mx-1 space-y-px" aria-label="Layers">
    {#each rows as row (row.kind === 'group' ? `g:${row.name}` : row.i)}
      {#if row.kind === 'group'}
        {@const open = !closed.includes(row.name)}
        {@const selected = row.members.every((k) => editor.selected.includes(k))}
        <li class="flex items-center gap-1 rounded px-1 {selected ? 'bg-blue-100 dark:bg-blue-950' : 'hover:bg-zinc-100 dark:hover:bg-zinc-800'}">
          <button
            type="button"
            class="{iconButton} shrink-0"
            aria-expanded={open}
            aria-label="{open ? 'Close' : 'Open'} group {row.name}"
            onclick={() => (closed = open ? [...closed, row.name] : closed.filter((c) => c !== row.name))}
          >
            <span class={open ? '' : '-rotate-90'}><Icon name="chevron" size={14} /></span>
          </button>
          {#if renaming === row.name}
            <input id="layer-rename" class="input-sm flex-1" aria-label="Name of group {row.name}" value={row.name} onkeydown={renameKeys} onblur={(ev) => finishRename(ev.currentTarget, true)} />
          {:else}
            <button
              type="button"
              class="min-w-0 flex-1 truncate py-1 text-left text-xs font-medium"
              aria-pressed={selected}
              title="Double-click to rename"
              onclick={() => (editor.selected = [...row.members])}
              ondblclick={() => startRename(row.name)}
              onkeydown={(ev) => ev.key === 'F2' && startRename(row.name)}
            >
              <Icon name="group" size={12} /> {row.name}
            </button>
          {/if}
          <button
            type="button"
            class={iconButton}
            aria-pressed={allHidden(row.members)}
            aria-label="Hide group {row.name}"
            title="Hide it while editing; it still prints"
            onclick={() => {
              const hide = !allHidden(row.members);
              editor.hidden = hide ? [...new Set([...editor.hidden, ...row.members])] : editor.hidden.filter((k) => !row.members.includes(k));
            }}><Icon name={allHidden(row.members) ? 'eyeOff' : 'eye'} size={14} /></button
          >
          <button
            type="button"
            class={iconButton}
            aria-pressed={allLocked(row.members)}
            aria-label="Lock group {row.name}"
            onclick={() => {
              editor.selected = [...row.members];
              editor.lockSelected();
            }}><Icon name={allLocked(row.members) ? 'lock' : 'unlock'} size={14} /></button
          >
        </li>
      {:else}
        {@const { e, i } = row}
        {@const selected = editor.selected.includes(i)}
        {@const hidden = editor.hidden.includes(i)}
        <li
          draggable={renaming !== i}
          class="flex items-center gap-1 rounded px-1 {row.depth ? 'ml-4' : ''} {selected ? 'bg-blue-100 dark:bg-blue-950' : 'hover:bg-zinc-100 dark:hover:bg-zinc-800'} {over === i ? 'ring-1 ring-blue-600' : ''}"
          ondragstart={(ev) => {
            dragging = i;
            ev.dataTransfer?.setData('text/plain', String(i));
          }}
          ondragover={(ev) => {
            ev.preventDefault();
            over = i;
          }}
          ondragleave={() => (over = over === i ? null : over)}
          ondrop={(ev) => drop(ev, i)}
          ondragend={() => (dragging = over = null)}
        >
          <canvas {@attach thumbnail(e)} width="36" height="24" class="h-6 w-9 shrink-0 rounded-sm border border-zinc-300 dark:border-zinc-600 {hidden ? 'opacity-30' : ''}" aria-hidden="true"></canvas>
          {#if renaming === i}
            <input id="layer-rename" class="input-sm flex-1" aria-label="Name of {layerName(e)}" value={e.name ?? ''} placeholder={kindName(e)} onkeydown={renameKeys} onblur={(ev) => finishRename(ev.currentTarget, true)} />
          {:else}
            <button
              type="button"
              class="min-w-0 flex-1 truncate py-1 text-left text-xs {problemElements.has(i) ? 'text-red-700 dark:text-red-400' : ''} {hidden ? 'text-zinc-600 italic dark:text-zinc-400' : ''}"
              aria-pressed={selected}
              title="Double-click to rename"
              onclick={(ev) => choose(ev, i)}
              ondblclick={() => startRename(i)}
              onkeydown={(ev) => ev.key === 'F2' && startRename(i)}
            >
              {layerName(e)}
            </button>
          {/if}
          <button
            type="button"
            class="{iconButton} {hidden ? '' : 'opacity-40 hover:opacity-100'}"
            aria-pressed={hidden}
            aria-label="Hide {layerName(e)}"
            title={hidden ? 'Hidden while editing; it still prints' : 'Hide it while editing; it still prints'}
            onclick={() => editor.toggleHidden(i)}><Icon name={hidden ? 'eyeOff' : 'eye'} size={14} /></button
          >
          <button
            type="button"
            class="{iconButton} {e.locked ? '' : 'opacity-40 hover:opacity-100'}"
            aria-pressed={e.locked ?? false}
            aria-label="Lock {layerName(e)}"
            title={e.locked ? 'Locked: it cannot be taken on the label' : 'Lock it, so it cannot be taken on the label by accident'}
            onclick={() => editor.toggleLock(i)}><Icon name={e.locked ? 'lock' : 'unlock'} size={14} /></button
          >
        </li>
      {/if}
    {/each}
  </ol>
  {#if editor.hidden.length}
    <p class="mt-1 text-xs text-amber-900 dark:text-amber-200" role="status">
      {editor.hidden.length === 1 ? 'One element is' : `${editor.hidden.length} elements are`} hidden while you edit. Hidden elements still print.
    </p>
  {/if}
{:else}
  <p class="text-xs text-zinc-600 dark:text-zinc-400">The label is empty.</p>
{/if}
