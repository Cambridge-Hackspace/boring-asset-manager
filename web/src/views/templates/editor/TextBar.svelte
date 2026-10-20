<script lang="ts">
  import { withProp, type TemplateElement } from '../../../lib/template';
  import { useEditor } from './editor.svelte';
  import NumberField from './NumberField.svelte';

  // The text bar, across the top while text is typed on the label, as a
  // word processor's: font, size, bold, alignment, wrapping, how it fits its
  // box, and fields to put in. Focus going here leaves the text being typed;
  // buttons keep it there.
  let { index, placeholderList }: { index: number; placeholderList: string[] } = $props();

  const editor = useEditor();
  const e = $derived(editor.doc.elements[index]);

  function set(next: TemplateElement) {
    editor.update(index, next, 'Change the text');
  }
  /** Keeps the focus in the text being typed, as a click on a button would take it. */
  const keep = (event: MouseEvent) => event.preventDefault();

  function insert(field: string) {
    const area = document.querySelector<HTMLTextAreaElement>('[data-text-editor] textarea');
    const text = e?.content_binding ?? '';
    const at = area?.selectionStart ?? text.length;
    const end = area?.selectionEnd ?? at;
    if (!e) return;
    set({ ...e, content_binding: text.slice(0, at) + field + text.slice(end) });
    queueMicrotask(() => {
      area?.focus();
      area?.setSelectionRange(at + field.length, at + field.length);
    });
  }
  const aligns = [
    ['left', 'Align left'],
    ['center', 'Center'],
    ['right', 'Align right'],
  ] as const;
</script>

{#if e}
  <div class="flex flex-wrap items-center gap-2" role="toolbar" aria-label="Text" data-text-bar>
    <select class="input-sm w-auto" aria-label="Font" value={e.font_family ?? 'sans'} onchange={(ev) => set({ ...e, font_family: ev.currentTarget.value as 'sans' | 'mono' })}>
      <option value="sans">Sans</option>
      <option value="mono">Monospace</option>
    </select>
    {#if e.fit === 'fill'}
      <div class="w-24"><NumberField id="text-bar-min" label="From" title="The least it takes" value={e.min_font_size} min={1} step={0.5} unit="pt" onchange={(n) => set({ ...e, min_font_size: n })} /></div>
      <div class="w-24"><NumberField id="text-bar-max" label="to" title="The most it takes" value={e.max_font_size ?? 200} min={1} max={200} step={0.5} unit="pt" onchange={(n) => set(withProp(e, 'max_font_size', n >= 200 ? undefined : n))} /></div>
    {:else}
      <div class="w-24"><NumberField id="text-bar-size" label="Size" value={e.font_size} min={1} max={200} step={0.5} unit="pt" onchange={(n) => set({ ...e, font_size: n, ...(e.min_font_size && e.min_font_size > n ? { min_font_size: n } : {}) })} /></div>
    {/if}
    <button type="button" class="btn-sm font-bold" aria-pressed={e.font_weight === 'bold'} aria-label="Bold" onmousedown={keep} onclick={() => set({ ...e, font_weight: e.font_weight === 'bold' ? 'normal' : 'bold' })}>B</button>
    <div class="flex" role="group" aria-label="Alignment">
      {#each aligns as [value, label] (value)}
        <button type="button" class="btn-sm rounded-none first:rounded-l last:rounded-r" aria-pressed={(e.align ?? 'left') === value} aria-label={label} onmousedown={keep} onclick={() => set({ ...e, align: value })}>
          {value === 'left' ? '⇤' : value === 'center' ? '↔' : '⇥'}
        </button>
      {/each}
    </div>
    <button type="button" class="btn-sm" aria-pressed={e.wrap ?? false} onmousedown={keep} onclick={() => set({ ...e, wrap: !e.wrap })}>Wrap</button>
    <button
      type="button"
      class="btn-sm"
      aria-pressed={e.fit === 'fill'}
      title="Grow or shrink the text to fill its box"
      onmousedown={keep}
      onclick={() => set(e.fit === 'fill' ? withProp(withProp(e, 'fit', undefined), 'max_font_size', undefined) : withProp(e, 'fit', 'fill'))}>Fill the box</button
    >
    <select
      class="input-sm w-auto"
      aria-label="Insert a field"
      value=""
      onchange={(ev) => {
        const field = ev.currentTarget.value;
        ev.currentTarget.value = '';
        if (field) insert(field);
      }}
    >
      <option value="">Insert a field…</option>
      {#each placeholderList as p (p)}<option value={p}>{p}</option>{/each}
    </select>
  </div>
{/if}
