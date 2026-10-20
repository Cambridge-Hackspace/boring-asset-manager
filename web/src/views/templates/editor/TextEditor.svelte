<script lang="ts">
  import { tick } from 'svelte';
  import { PRINTED_FONT } from '../../../lib/live/client';
  import { useEditor } from './editor.svelte';

  // Typing on the label, as Krita's text tool: a box over the text, turned
  // with it, in the font labels are printed in and at the size it is drawn
  // at (shrunk to fit, or filling its box), lined up and centered down its
  // box as it prints. The text is taken out of the label drawn while it is
  // typed, so it is not shown twice. Fields are typed in braces. Escape, or
  // a click elsewhere, finishes; finished empty, it comes off.
  let { index, scale, ox, oy, onfinish }: { index: number; scale: number; ox: number; oy: number; onfinish: () => void } = $props();

  const editor = useEditor();
  const doc = $derived(editor.doc);
  const e = $derived(doc.elements[index]);
  let area = $state<HTMLTextAreaElement | null>(null);

  // The size it is drawn at, in points, in screen pixels.
  const points = $derived(editor.drawnSizes[index] ?? e?.font_size ?? 12);
  const px = $derived((points / 72) * (doc.units === 'in' ? 1 : 25.4) * scale);
  const family = $derived(`"${e?.font_family === 'mono' ? PRINTED_FONT.mono : PRINTED_FONT.sans}", ${e?.font_family === 'mono' ? 'monospace' : 'sans-serif'}`);

  let begun = false;
  function input(event: Event & { currentTarget: HTMLTextAreaElement }) {
    const el = doc.elements[index];
    if (!el) return;
    // One step in the history for all the typing.
    if (!begun) {
      editor.begin('Type');
      begun = true;
    }
    editor.doc.elements[index] = { ...el, content_binding: event.currentTarget.value };
  }

  function keydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      onfinish();
    }
  }

  $effect(() => {
    void tick().then(() => area?.focus());
  });
</script>

{#if e}
  <div
    class="text-edit absolute flex items-center"
    style="left:{ox + e.x * scale}px;top:{oy + e.y * scale}px;width:{e.width * scale}px;height:{e.height * scale}px;{e.rotation ? `transform:rotate(${e.rotation}deg)` : ''}"
    data-text-editor
  >
    <textarea
      bind:this={area}
      aria-label="Text of {e.content_binding || 'the new text'}"
      class="block w-full resize-none overflow-hidden border-0 bg-transparent p-0 outline-none"
      class:nowrap={!e.wrap}
      rows={Math.max(1, (e.content_binding ?? '').split('\n').length)}
      spellcheck="false"
      style:font-family={family}
      style:font-weight={e.font_weight === 'bold' ? '700' : '400'}
      style:font-size="{px}px"
      style:line-height="normal"
      style:text-align={e.align ?? 'left'}
      style:color={e.fill === 'white' ? 'white' : 'black'}
      style:text-shadow={e.fill === 'white' ? '0 0 2px black' : 'none'}
      value={e.content_binding ?? ''}
      oninput={input}
      onkeydown={keydown}
      onblur={(ev) => {
        // Into the text bar, it is still being typed.
        if (!(ev.relatedTarget instanceof Element && ev.relatedTarget.closest('[data-text-bar]'))) onfinish();
      }}
    ></textarea>
  </div>
{/if}

<style>
  .text-edit {
    outline: 1px dashed rgb(37 99 235);
    outline-offset: 1px;
  }
  textarea.nowrap {
    white-space: pre;
  }
</style>
