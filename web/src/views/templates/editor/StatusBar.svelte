<script lang="ts">
  import type { LiveState } from '../../../lib/live/client';
  import { calibrate, forget } from '../../../lib/screen';
  import { useEditor } from './editor.svelte';
  import { parseZoom } from './view';

  // Along the bottom, as in Krita: where the pointer is on the label, what
  // can be done now (the modifiers, mostly), how the label is drawn, and the
  // zoom: a percentage to type, a slider, and print size, the label at its
  // real size on this screen.
  let { liveState }: { liveState: LiveState } = $props();
  const editor = useEditor();
  const fmt = (v: number) => (editor.doc.units === 'in' ? v.toFixed(3) : v.toFixed(1));

  const percent = $derived(editor.zoom >= 10 ? Math.round(editor.zoom) : Math.round(editor.zoom * 10) / 10);
  let typed = $state<string | null>(null);

  function enterZoom() {
    const p = typed === null ? null : parseZoom(typed);
    typed = null;
    if (p !== null && p > 0) editor.setZoom(p);
  }

  // The slider runs on a logarithmic scale, 5% to 6400%.
  const slider = $derived(Math.log(editor.zoom));

  // Calibrating print size on this screen: a bar of known CSS pixels, measured.
  let dialog = $state<HTMLDialogElement | null>(null);
  const BAR = 300;
  let measured = $state('');
  function saveCalibration(event: SubmitEvent) {
    event.preventDefault();
    const mm = Number(measured);
    if (!(mm > 0)) return;
    calibrate(editor.screen, BAR, mm);
    editor.calibration++;
    dialog?.close();
  }
</script>

<div class="flex min-h-7 flex-wrap items-center gap-x-4 gap-y-1 border-t border-zinc-200 bg-zinc-100 px-3 py-0.5 text-xs text-zinc-700 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300">
  <span class="w-32 shrink-0 tabular-nums">
    {#if editor.pointer}{fmt(editor.pointer.x)}, {fmt(editor.pointer.y)} {editor.doc.units}{/if}
  </span>
  <span class="min-w-0 flex-1 truncate" title={editor.hint}>{editor.hint}</span>
  <span role="status" class="flex shrink-0 items-center gap-1.5">
    {#if liveState === 'loading'}
      <span aria-hidden="true" class="h-3 w-3 animate-spin rounded-full border-2 border-zinc-400 border-t-transparent"></span>
      Loading the live preview
    {:else if liveState === 'failed'}
      <span title="This browser cannot run the live preview: the server draws the label shortly after each change">Live preview unavailable</span>
    {/if}
  </span>
  <div class="flex shrink-0 items-center gap-1.5" role="group" aria-label="Zoom">
    <button
      type="button"
      class="btn-sm"
      aria-pressed={editor.printSize}
      title={editor.printSize ? 'Print size: 100% is the label at its real size on this screen' : 'Pixel size: 100% is one printer dot a screen pixel. Switch to show it at its real size.'}
      onclick={() => editor.setPrintSize(!editor.printSize)}>Print size</button
    >
    {#if editor.printSize}
      <button type="button" class="underline" onclick={() => dialog?.showModal()}>{editor.calibrated ? 'Calibrated' : 'Approximate: calibrate'}</button>
    {/if}
    <label class="sr-only" for="zoom-slider">Zoom slider</label>
    <input
      id="zoom-slider"
      type="range"
      class="w-24"
      min={Math.log(5)}
      max={Math.log(6400)}
      step="0.01"
      value={slider}
      oninput={(e) => editor.setZoom(Math.exp(Number(e.currentTarget.value)))}
    />
    <label class="sr-only" for="zoom-level">Zoom level</label>
    <input
      id="zoom-level"
      class="input-sm w-16 text-right"
      value={typed ?? `${percent}%`}
      oninput={(e) => (typed = e.currentTarget.value)}
      onkeydown={(e) => {
        if (e.key === 'Enter') enterZoom();
        if (e.key === 'Escape') typed = null;
      }}
      onblur={enterZoom}
    />
  </div>
</div>

<dialog bind:this={dialog} class="m-auto rounded-lg border border-zinc-300 bg-white p-4 text-sm text-zinc-900 backdrop:bg-black/40 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" aria-labelledby="calibrate-heading">
  <form class="space-y-3" onsubmit={saveCalibration}>
    <h2 id="calibrate-heading" class="font-semibold">Calibrate print size on this screen</h2>
    <p class="max-w-sm text-xs text-zinc-600 dark:text-zinc-400">
      Browsers do not tell how large their screen is, so measure this bar with a ruler held to the screen. Each screen is calibrated once, and remembered in this browser.
    </p>
    <div class="h-4 bg-zinc-900 dark:bg-zinc-100" style="width:{BAR}px"></div>
    <label class="flex items-center gap-2 text-xs" for="calibrate-mm">
      The bar measures
      <input id="calibrate-mm" class="input-sm w-20" type="number" min="1" step="0.5" bind:value={measured} required />
      mm
    </label>
    <div class="flex gap-2">
      <button type="submit" class="btn-sm">Save</button>
      {#if editor.calibrated}
        <button
          type="button"
          class="btn-sm"
          onclick={() => {
            forget(editor.screen);
            editor.calibration++;
            dialog?.close();
          }}>Forget it</button
        >
      {/if}
      <button type="button" class="btn-sm" onclick={() => dialog?.close()}>Cancel</button>
    </div>
  </form>
</dialog>
