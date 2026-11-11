<script lang="ts">
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { calibrationText, scrapText } from '../../lib/lasers';
  import type { LaserCameraState, LaserJobAnswer } from '../../lib/models';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // A laser's camera (docs/printing.md#camera-alignment): engraving the
  // calibration marks on scrap (a job like any other, framed and started on
  // the job's card by a person at the machine), finding them at the scrap's
  // height, and the instance's limits, for an admin.
  let {
    printerID,
    materialID,
    canManage,
    onprepared,
    oncalibrated,
  }: {
    printerID: string;
    materialID: string;
    canManage: boolean;
    onprepared: (a: LaserJobAnswer) => void | Promise<void>;
    oncalibrated: () => void | Promise<void>;
  } = $props();

  let camera = $state<LaserCameraState | null>(null);
  let z = $state(3);
  let busy = $state(false);
  let error = $state('');
  let notice = $state('');
  let maxError = $state(0.5);
  let tolerance = $state(1);

  const base = $derived(`/printers/${encodeURIComponent(printerID)}`);

  async function load() {
    error = '';
    try {
      camera = await request<LaserCameraState>('GET', `${base}/calibration`);
      maxError = camera.max_error_mm;
      tolerance = camera.height_tolerance_mm;
      if (camera.calibration) z = camera.calibration.z_mm;
    } catch (err) {
      error = messageOf(err);
    }
  }

  $effect(() => {
    void printerID;
    void load();
  });

  async function run(what: () => Promise<void>) {
    error = notice = '';
    busy = true;
    try {
      await what();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  const prepareMarks = () =>
    run(async () => {
      await onprepared(await request<LaserJobAnswer>('POST', `${base}/calibration/marks`, { material: materialID }));
      notice = 'The marks are prepared: frame them on the scrap, then start them. Leave the scrap where it is.';
    });

  const calibrate = () =>
    run(async () => {
      await request('POST', `${base}/calibration`, { z_mm: Number(z) });
      await load();
      notice = 'Calibrated. Measure a workpiece at this height to place a label on it.';
      await oncalibrated();
    });

  const forget = () =>
    run(async () => {
      await request('DELETE', `${base}/calibration`);
      await load();
      notice = 'Calibration forgotten.';
    });

  const saveSettings = (event: SubmitEvent) => {
    event.preventDefault();
    return run(async () => {
      await request('PUT', '/settings/lasers', { max_error_mm: Number(maxError), height_tolerance_mm: Number(tolerance) });
      await load();
      notice = 'Saved the camera’s limits.';
    });
  };
</script>

{#if camera?.camera}
  <section class="card space-y-3" aria-labelledby="camera-heading">
    <h2 id="camera-heading" class="text-lg font-semibold">Camera</h2>
    <p class="text-sm" data-testid="calibration">{calibrationText(camera.calibration)}</p>
    <ErrorMessage {error} />
    {#if notice}<p class="text-sm text-green-700 dark:text-green-400" role="status">{notice}</p>{/if}
    <ol class="list-decimal space-y-3 pl-5 text-sm">
      <li class="space-y-2">
        <p>{camera.layout ? scrapText(camera.layout) : 'Lay light scrap on the bed'}, choose its material above, and engrave the marks on it.</p>
        <button type="button" class="btn-secondary" disabled={busy || !materialID} onclick={prepareMarks}>Prepare the marks</button>
      </li>
      <li class="space-y-2">
        <p>With the scrap where it was engraved, find the marks. The camera sees the bed differently at each height, so give the height of the scrap’s surface above the bed.</p>
        <div class="flex flex-wrap items-end gap-2">
          <div>
            <label class="label" for="calibration-z">Surface height (mm)</label>
            <input id="calibration-z" class="input w-28" type="number" min="0" max="500" step="0.1" bind:value={z} />
          </div>
          <button type="button" class="btn-secondary" disabled={busy} onclick={calibrate}>Find the marks</button>
        </div>
      </li>
    </ol>
    {#if camera.calibration}
      <button type="button" class="btn-secondary" disabled={busy} onclick={forget}>Forget the calibration</button>
    {/if}
    {#if canManage}
      <form class="space-y-2 border-t border-zinc-200 pt-3 dark:border-zinc-700" onsubmit={saveSettings} aria-labelledby="camera-limits-heading">
        <h3 id="camera-limits-heading" class="font-medium">Limits, for every laser</h3>
        <div class="grid gap-3 sm:grid-cols-2">
          <div>
            <label class="label" for="camera-max-error">Largest calibration error (mm)</label>
            <input id="camera-max-error" class="input" type="number" min="0.01" max="10" step="0.01" required bind:value={maxError} />
          </div>
          <div>
            <label class="label" for="camera-tolerance">Height tolerance (mm)</label>
            <input id="camera-tolerance" class="input" type="number" min="0" max="50" step="0.1" required bind:value={tolerance} />
          </div>
        </div>
        <button type="submit" class="btn-secondary" disabled={busy}>Save the limits</button>
      </form>
    {/if}
  </section>
{/if}
