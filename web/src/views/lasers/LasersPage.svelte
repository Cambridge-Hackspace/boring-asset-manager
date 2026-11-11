<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { request, requestBlob } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { cornerName, describeMaterial, interlockName, jobStateText, lasersOf, placedOf, templateMM, turnOf, underWay } from '../../lib/lasers';
  import type { DriverInfo, LaserJob, LaserJobAnswer, LaserStatus, Material, MeasuredWorkpiece, OneShotModel, Printer, TemplateSummary } from '../../lib/models';
  import type { Corner } from '../../lib/template';
  import { router } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';
  import CameraSection from './CameraSection.svelte';
  import MaterialsSection from './MaterialsSection.svelte';

  // /lasers: engraving a label onto a workpiece, one piece at a time
  // (docs/printing.md#lasers). ?printer=, ?asset= and ?template= choose
  // what the asset's page or the template editor sent. Preparing needs
  // labels.print; framing and starting are for a person at the machine
  // holding lasers.run. A laser with a camera measures the workpiece
  // wherever it lies (docs/printing.md#camera-alignment), and the plan is
  // drawn over its photo for the person to confirm before framing.
  let { params: _params }: { params: Record<string, string> } = $props();

  let lasers = $state<{ printer: Printer; model: OneShotModel }[]>([]);
  let templates = $state<TemplateSummary[]>([]);
  let materials = $state<Material[]>([]);
  let jobs = $state<LaserJob[]>([]);
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');

  let printerID = $state(router.query.get('printer') ?? '');
  let templateID = $state(router.query.get('template') ?? '');
  const assetID = router.query.get('asset') ?? '';
  let assetName = $state('');
  let materialID = $state('');
  let width = $state(0);
  let height = $state(0);
  let x = $state(0);
  let y = $state(0);
  let angle = $state(0);
  let placeByHand = $state(false);
  let labelX = $state(0);
  let labelY = $state(0);
  // '' is as the template says.
  let corner = $state<Corner | ''>('');
  let inset = $state(0);
  // The camera's measure: the workpiece's surface height above the bed,
  // and whether the fields hold what it measured.
  let z = $state(3);
  let fromCamera = $state(false);
  let view = $state('');
  let confirmed = $state(false);

  let answer = $state<LaserJobAnswer | null>(null);
  let status = $state<LaserStatus | null>(null);
  let preview = $state('');
  let busy = $state(false);

  const canPrint = $derived(session.can('labels.print'));
  const canRun = $derived(session.can('lasers.run'));
  const canManage = $derived(session.can('settings.manage'));
  const laser = $derived(lasers.find((l) => l.printer.id === printerID));
  const material = $derived(materials.find((m) => m.id === materialID));
  const job = $derived(answer?.job);
  const held = $derived(!!answer?.held && job?.state === 'prepared');
  // A job placed by the camera is confirmed over the photo before it is framed.
  const needsConfirm = $derived(!!job?.placement.camera && held && !confirmed);
  const moment = (iso: string | null | undefined) => (iso ? new Date(iso).toLocaleString() : '');

  async function load() {
    try {
      const [printers, drivers, tpls] = await Promise.all([
        request<{ printers: Printer[] }>('GET', '/printers').then((r) => r.printers ?? []),
        request<{ drivers: DriverInfo[] }>('GET', '/printers/drivers').then((r) => r.drivers ?? []),
        request<{ templates: TemplateSummary[] }>('GET', '/templates').then((r) => r.templates ?? []),
      ]);
      lasers = lasersOf(printers, drivers);
      templates = tpls;
      if (!laser) printerID = lasers[0]?.printer.id ?? '';
      if (!templates.some((t) => t.id === templateID)) templateID = templates[0]?.id ?? '';
      sizeToTemplate();
      if (assetID) {
        const r = await request<{ asset: { asset?: { name: string }; name?: string } }>('GET', `/assets/${encodeURIComponent(assetID)}`);
        assetName = r.asset?.asset?.name ?? r.asset?.name ?? '';
      }
      await loadLaser();
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }

  /** The chosen laser's materials, history and status. */
  async function loadLaser() {
    if (!printerID) return;
    const id = encodeURIComponent(printerID);
    const [m, j] = await Promise.all([
      request<{ materials: Material[] }>('GET', `/printers/${id}/materials`).then((r) => r.materials ?? []),
      canPrint ? request<{ jobs: LaserJob[] }>('GET', `/printers/${id}/jobs`).then((r) => r.jobs ?? []) : Promise.resolve([]),
    ]);
    materials = m;
    jobs = j;
    if (!materials.some((x) => x.id === materialID)) materialID = materials[0]?.id ?? '';
    if (canPrint) await poll();
  }

  /** The workpiece starts the template's size. */
  function sizeToTemplate() {
    const t = templates.find((t) => t.id === templateID);
    if (!t) return;
    const mm = templateMM(t);
    width = mm.width;
    height = mm.height;
  }

  async function chooseLaser() {
    answer = null;
    status = null;
    preview = '';
    fromCamera = false;
    setView('');
    error = notice = '';
    try {
      await loadLaser();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function showPreview(a: LaserJobAnswer | null) {
    if (preview) URL.revokeObjectURL(preview);
    preview = '';
    if (!a?.held || !a.preview) return;
    try {
      preview = URL.createObjectURL(await requestBlob('GET', a.preview.replace(/^\/api\/v1/, '')));
    } catch {
      preview = '';
    }
  }

  function setView(url: string) {
    if (view) URL.revokeObjectURL(view);
    view = url;
  }

  /** The latest photo of the bed, seen from above; none when the server holds none. */
  async function loadView() {
    try {
      setView(URL.createObjectURL(await requestBlob('GET', `/printers/${encodeURIComponent(printerID)}/camera/view`)));
    } catch {
      setView('');
    }
  }

  /** Photographs the bed and fills the workpiece in as the camera measured it. */
  async function measure() {
    error = notice = '';
    busy = true;
    try {
      const r = await request<{ workpiece: MeasuredWorkpiece }>('POST', `/printers/${encodeURIComponent(printerID)}/camera/measure`, { z_mm: Number(z) });
      const w = r.workpiece;
      [width, height, x, y, angle] = [w.width_mm, w.height_mm, w.x_mm, w.y_mm, w.angle_deg];
      fromCamera = true;
      notice = `Measured: ${w.width_mm} × ${w.height_mm} mm, turned ${w.angle_deg}°. Prepare the job to see the plan over the photo.`;
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
    await loadView();
  }

  async function prepare(event: SubmitEvent) {
    event.preventDefault();
    error = notice = '';
    busy = true;
    try {
      const angled = Number(angle) ? { angle_deg: Number(angle) } : {};
      answer = await request<LaserJobAnswer>('POST', `/printers/${encodeURIComponent(printerID)}/jobs`, {
        template_id: templateID,
        ...(assetID ? { asset_id: assetID } : {}),
        material: materialID,
        workpiece: { width_mm: Number(width), height_mm: Number(height), x_mm: Number(x), y_mm: Number(y), ...angled, ...(fromCamera ? { z_mm: Number(z) } : {}) },
        ...(fromCamera ? { camera: true } : {}),
        ...(placeByHand ? { label_at: { x: Number(labelX), y: Number(labelY) } } : {}),
        ...(!placeByHand && corner ? { anchor: { corner, ...(corner !== 'center' && Number(inset) > 0 ? { inset_mm: Number(inset) } : {}) } } : {}),
      });
      confirmed = false;
      await showPreview(answer);
      notice = 'Prepared. Check the preview, then frame it on the piece and start it at the machine.';
      await loadLaser();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function act(action: 'frame' | 'start' | 'pause' | 'cancel') {
    if (!job) return;
    error = notice = '';
    busy = true;
    try {
      answer = await request<LaserJobAnswer>('POST', `/printers/${encodeURIComponent(printerID)}/jobs/${encodeURIComponent(job.id)}/${action}`);
      notice = {
        frame: 'The machine is tracing the outline with its pointer.',
        start: answer.job.state === 'ready' ? 'Sent ready: press the button on the machine to start.' : 'Started.',
        pause: 'Paused.',
        cancel: 'Cancelled.',
      }[action];
      await loadLaser();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  /** Shows a job the camera section prepared: the calibration marks. */
  async function showPrepared(a: LaserJobAnswer) {
    answer = a;
    confirmed = false;
    await showPreview(a);
    await loadLaser();
  }

  /** Reads the machine's status, and keeps the job shown up to date. */
  async function poll() {
    if (!printerID || !canPrint) return;
    try {
      status = await request<LaserStatus>('GET', `/printers/${encodeURIComponent(printerID)}/status`);
      const latest = status.job;
      if (latest && (!answer || latest.job.id === answer.job.id)) {
        const was = answer?.job.state;
        answer = latest;
        if (was !== latest.job.state) {
          if (!preview) await showPreview(latest);
          jobs = (await request<{ jobs: LaserJob[] }>('GET', `/printers/${encodeURIComponent(printerID)}/jobs`)).jobs ?? [];
        }
      }
    } catch {
      // The next poll tries again.
    }
  }

  let timer: ReturnType<typeof setInterval> | undefined;
  onMount(() => {
    void load();
    timer = setInterval(() => {
      if (underWay(answer?.job.state)) void poll();
    }, 1000);
  });
  onDestroy(() => {
    clearInterval(timer);
    if (preview) URL.revokeObjectURL(preview);
    if (view) URL.revokeObjectURL(view);
  });

  // The preview's drawing: the work area, the workpiece, the label and its cuts, in millimeters.
  const area = $derived(laser ? { w: laser.model.work_width_mm, h: laser.model.work_height_mm } : null);
  const problems = $derived((status?.status?.interlocks ?? []).filter((i) => !i.ok));
  // The photo is drawn under jobs the camera placed, and the calibration marks.
  const photo = $derived(!!view && !!job && (!!job.placement.camera || !!job.placement.calibration));
  const corners: Corner[] = ['center', 'top_left', 'top_right', 'bottom_left', 'bottom_right'];
</script>

<PageHeader title="Lasers">
  {#snippet lede()}
    A laser engraves a label onto a workpiece, one piece at a time. A person at the machine starts every job: with its own button, or with
    Start, which needs permission to start lasers. The machine’s interlocks are checked first (<a class="underline" href="/help/lasers">Lasers</a>).
  {/snippet}
</PageHeader>
<ErrorMessage {error} />
{#if notice}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{notice}</p>{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading lasers…</p>
{:else if lasers.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No lasers yet. Add one under <a class="underline" href="/printers">Printers</a>, with a laser’s model.</p>
{:else}
  <div class="grid gap-6 lg:grid-cols-2">
    <div class="space-y-6">
      {#if canPrint}
        <form class="card space-y-4" onsubmit={prepare} aria-labelledby="prepare-heading">
          <h2 id="prepare-heading" class="text-lg font-semibold">Prepare a job</h2>
          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <label class="label" for="laser">Laser</label>
              <select id="laser" class="input" bind:value={printerID} onchange={chooseLaser}>
                {#each lasers as l (l.printer.id)}<option value={l.printer.id}>{l.printer.name}</option>{/each}
              </select>
            </div>
            <div>
              <label class="label" for="material">Material</label>
              <select id="material" class="input" aria-describedby="material-help" bind:value={materialID}>
                {#each materials as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
              </select>
              {#if material}<p id="material-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">{describeMaterial(material)}.</p>{/if}
            </div>
            <div>
              <label class="label" for="template">Template</label>
              <select id="template" class="input" bind:value={templateID} onchange={sizeToTemplate}>
                {#each templates as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
              </select>
            </div>
            <div>
              <p class="label">Label</p>
              <p class="text-sm">
                {#if assetID}<a class="underline" href="/assets/{assetID}">{assetName || 'The asset'}</a>’s label{:else}The template’s placeholders{/if}
              </p>
            </div>
          </div>
          <fieldset class="grid gap-3 sm:grid-cols-5" oninput={() => (fromCamera = false)}>
            <legend class="label">Workpiece (mm){area ? `, on a work area of ${area.w} × ${area.h} mm` : ''}</legend>
            <div><label class="label" for="wp-width">Width</label><input id="wp-width" class="input" type="number" min="0.1" step="0.01" required bind:value={width} /></div>
            <div><label class="label" for="wp-height">Height</label><input id="wp-height" class="input" type="number" min="0.1" step="0.01" required bind:value={height} /></div>
            <div><label class="label" for="wp-x">From the left</label><input id="wp-x" class="input" type="number" step="0.01" required bind:value={x} /></div>
            <div><label class="label" for="wp-y">From the top</label><input id="wp-y" class="input" type="number" step="0.01" required bind:value={y} /></div>
            <div><label class="label" for="wp-angle">Turned (°)</label><input id="wp-angle" class="input" type="number" min="-180" max="180" step="0.01" bind:value={angle} /></div>
          </fieldset>
          {#if laser?.model.camera}
            <div class="flex flex-wrap items-end gap-2">
              <div>
                <label class="label" for="wp-z">Surface height (mm)</label>
                <input id="wp-z" class="input w-28" type="number" min="0" max="500" step="0.1" aria-describedby="wp-z-help" bind:value={z} oninput={() => (fromCamera = false)} />
              </div>
              <button type="button" class="btn-secondary" disabled={busy} onclick={measure}>Measure with the camera</button>
            </div>
            <p id="wp-z-help" class="text-xs text-zinc-600 dark:text-zinc-400">
              {fromCamera ? 'Measured by the camera at this height.' : 'Lay the workpiece anywhere on the bed, and the camera finds it. Its surface must stand as high as the calibration’s.'}
            </p>
          {/if}
          <label class="flex min-h-11 items-center gap-2"><input type="checkbox" bind:checked={placeByHand} /> Place the label on the workpiece myself</label>
          {#if !placeByHand}
            <div class="grid gap-3 sm:grid-cols-2">
              <div>
                <label class="label" for="label-corner">Label on the workpiece</label>
                <select id="label-corner" class="input" bind:value={corner}>
                  <option value="">As the template says</option>
                  {#each corners as c (c)}<option value={c}>{cornerName(c).replace(/^./, (l) => l.toUpperCase())}</option>{/each}
                </select>
              </div>
              {#if corner && corner !== 'center'}
                <div><label class="label" for="label-inset">In from its sides (mm)</label><input id="label-inset" class="input" type="number" min="0" step="0.1" bind:value={inset} /></div>
              {/if}
            </div>
          {:else}
            <div class="grid gap-3 sm:grid-cols-2">
              <div><label class="label" for="label-x">Label from the workpiece’s left (mm)</label><input id="label-x" class="input" type="number" min="0" step="0.1" bind:value={labelX} /></div>
              <div><label class="label" for="label-y">Label from the workpiece’s top (mm)</label><input id="label-y" class="input" type="number" min="0" step="0.1" bind:value={labelY} /></div>
            </div>
          {/if}
          <button type="submit" class={held || underWay(job?.state) ? 'btn-secondary' : 'btn-primary'} disabled={busy || !templateID || !materialID}>
            {held ? 'Prepare again' : 'Prepare'}
          </button>
        </form>
      {/if}

      {#if laser && canPrint && laser.model.camera}
        <CameraSection printerID={laser.printer.id} {materialID} {canManage} onprepared={showPrepared} oncalibrated={loadView} />
      {/if}

      {#if laser}
        <MaterialsSection printerID={laser.printer.id} {materials} {canManage} onchange={loadLaser} />
      {/if}
    </div>

    <div class="space-y-6">
      {#if job && area}
        <section class="card space-y-3" aria-labelledby="job-heading">
          <h2 id="job-heading" class="text-lg font-semibold">{job.placement.calibration ? job.template_name : `${job.asset_name || 'Placeholders'}, with ${job.template_name}`}</h2>
          <p class="text-sm" data-testid="job-state">{jobStateText(job.state)}.{job.message ? ` ${job.message}` : ''}</p>
          <p class="text-sm text-zinc-600 dark:text-zinc-400">
            On {job.material.name}{job.placement.mirror ? ', mirrored: lay the workpiece face down' : ''}{job.placement.cuts ? `, cutting ${job.placement.cuts} outline${job.placement.cuts === 1 ? '' : 's'}` : ''}.
          </p>
          <svg
            class="w-full rounded border border-zinc-300 bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-900"
            viewBox="-2 -2 {area.w + 4} {area.h + 4}"
            role="img"
            aria-label="Where the label goes: the {area.w} × {area.h} mm work area, the {job.placement.workpiece.width} × {job.placement.workpiece.height} mm workpiece{job.placement.angle_deg ? `, turned ${job.placement.angle_deg}°` : ''}, and the label on it{photo ? ', over the camera’s photo' : ''}"
          >
            {#if photo}
              <image href={view} x="0" y="0" width={area.w} height={area.h} preserveAspectRatio="none" />
            {/if}
            <rect x="0" y="0" width={area.w} height={area.h} fill="none" stroke="currentColor" stroke-width="0.4" stroke-dasharray="2 2" />
            <g transform={turnOf(job.placement)}>
              <rect
                x={job.placement.workpiece.x}
                y={job.placement.workpiece.y}
                width={job.placement.workpiece.width}
                height={job.placement.workpiece.height}
                fill={photo ? 'none' : '#e8d5b5'}
                stroke={photo ? '#2563eb' : '#8a6d3b'}
                stroke-width={photo ? 0.6 : 0.3}
                stroke-dasharray={photo ? '2 1' : undefined}
              />
              {#if !preview || photo}
                {@const placed = placedOf(job.placement)}
                <rect x={placed.x} y={placed.y} width={placed.width} height={placed.height} fill="none" stroke={photo ? '#2563eb' : '#333'} stroke-width="0.3" />
              {/if}
            </g>
            {#if preview}
              <image href={preview} x={job.placement.label.x} y={job.placement.label.y} width={job.placement.label.width} height={job.placement.label.height} preserveAspectRatio="none" style="mix-blend-mode: multiply" />
            {/if}
            {#each answer?.cuts ?? [] as c, i (i)}
              {#if c.closed}
                <polygon points={c.points.map((p) => `${p.x},${p.y}`).join(' ')} fill="none" stroke="#dc2626" stroke-width="0.3" />
              {:else}
                <polyline points={c.points.map((p) => `${p.x},${p.y}`).join(' ')} fill="none" stroke="#dc2626" stroke-width="0.3" />
              {/if}
            {/each}
          </svg>
          {#if (answer?.warnings ?? []).length}
            <ul class="list-disc pl-5 text-sm text-amber-700 dark:text-amber-400">
              {#each answer?.warnings ?? [] as w (w)}<li>{w}</li>{/each}
            </ul>
          {/if}
          {#if job.placement.camera && held}
            {#if !view}<p class="text-sm text-zinc-600 dark:text-zinc-400">This server holds no photo of the bed: measure the workpiece again to see the plan over it.</p>{/if}
            <label class="flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" bind:checked={confirmed} disabled={!view} /> The plan sits on the workpiece in the photo</label>
          {/if}
          {#if status?.status && underWay(job.state)}
            <label class="label" for="job-progress">Progress</label>
            <progress id="job-progress" class="w-full" max="1" value={status.status.progress}></progress>
          {/if}
          <div class="flex flex-wrap gap-2">
            {#if held && canRun && laser?.model.frames}
              <button type="button" class="btn-secondary" disabled={busy || needsConfirm} onclick={() => act('frame')}>Frame</button>
            {/if}
            {#if (held || job.state === 'paused') && canRun}
              <button type="button" class="btn-primary" disabled={busy || problems.length > 0 || needsConfirm} onclick={() => act('start')}>
                {job.state === 'paused' ? 'Resume' : laser?.model.start_button ? 'Send ready for the button' : 'Start'}
              </button>
            {/if}
            {#if job.state === 'running'}
              <button type="button" class="btn-secondary" disabled={busy} onclick={() => act('pause')}>Pause</button>
            {/if}
            {#if job.state === 'prepared' || underWay(job.state)}
              <button type="button" class="btn-secondary" disabled={busy} onclick={() => act('cancel')}>Cancel the job</button>
            {/if}
          </div>
          {#if held && !canRun}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">Someone allowed to start lasers frames and starts it, at the machine.</p>
          {:else if job.state === 'prepared' && !answer?.held}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">Prepare it again to start it.</p>
          {/if}
        </section>
      {/if}

      {#if laser && canPrint}
        <section class="card space-y-2" aria-labelledby="machine-heading">
          <h2 id="machine-heading" class="text-lg font-semibold">The machine</h2>
          {#if status?.unreachable}
            <p class="text-sm text-red-700 dark:text-red-400">Could not reach it: {status.unreachable}</p>
          {:else if status?.status}
            <p class="text-sm">State: {status.status.state}{status.status.message ? `. ${status.status.message}` : ''}</p>
            <ul class="space-y-1 text-sm" aria-label="Interlocks">
              {#each status.status.interlocks ?? [] as i (i.name)}
                <li class={i.ok ? '' : 'font-medium text-red-700 dark:text-red-400'}>{interlockName(i.name)}: {i.ok ? 'fine' : i.message || 'reports a problem'}</li>
              {/each}
            </ul>
            {#if problems.length}<p class="text-sm text-red-700 dark:text-red-400">It won’t start until every interlock is fine.</p>{/if}
          {/if}
          <button type="button" class="btn-secondary" onclick={poll}>Check again</button>
        </section>

        <section class="card space-y-2" aria-labelledby="history-heading">
          <h2 id="history-heading" class="text-lg font-semibold">History</h2>
          {#if jobs.length === 0}
            <p class="text-sm text-zinc-600 dark:text-zinc-400">No jobs yet.</p>
          {:else}
            <ul class="space-y-1 text-sm">
              {#each jobs as j (j.id)}
                <li>
                  {moment(j.created_at)}: {j.asset_name || 'placeholders'}, {j.template_name}, on {j.material.name} at {j.material.engrave.power}% and {j.material.engrave.speed} mm/s. {jobStateText(j.state)}.
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      {/if}
    </div>
  </div>
{/if}
