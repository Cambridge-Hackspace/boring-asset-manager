<script lang="ts">
  import { untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { DriverInfo, LabelStock, Printer, PrinterChanges, PrinterConnection } from '../../lib/models';
  import { describeChanges, describeStock, driverName, optionLabel, stockFor } from '../../lib/stock';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // Creates a printer, or edits one when printer is given. The form is
  // generated from the drivers' own descriptions.
  let {
    printer,
    drivers,
    stock = [],
    onsaved,
    oncancel,
  }: { printer?: Printer; drivers: DriverInfo[]; stock?: LabelStock[]; onsaved: () => void; oncancel: () => void } = $props();

  const connectionHelp: Record<PrinterConnection['type'], string> = {
    tcp: 'The printer’s IP address or host name (port 9100 if none is given): a network printer, or a print server such as p910nd.',
    device: 'A device path on the BAM server, such as /dev/usb/lp0. It must be allowed in the server’s [printers] devices setting.',
    ipp: 'An IPP queue URI, such as ipp://cups.lan/printers/labels. The queue must be raw.',
    windows: 'A Windows printer name or \\\\host\\share. Only when BAM runs on Windows.',
  };

  const initial = untrack(() => $state.snapshot(printer));
  let name = $state(initial?.name ?? '');
  let driver = $state(initial?.driver ?? untrack(() => drivers[0]?.driver) ?? '');
  let model = $state(initial?.model ?? '');
  let media = $state(initial?.media ?? '');
  let connType = $state<PrinterConnection['type']>(initial?.connection.type ?? 'tcp');
  let address = $state(initial?.connection.address ?? '');
  // Options as entered: '' means unset (the driver's default).
  let options = $state<Record<string, string | boolean>>(
    Object.fromEntries(Object.entries(initial?.options ?? {}).map(([k, v]) => [k, typeof v === 'boolean' ? v : String(v)])),
  );
  let error = $state('');
  let busy = $state(false);
  // What saving would affect, shown before saving a change that affects
  // anything; saving again saves it.
  let affects = $state<string[]>([]);
  // Changing the printer again needs asking again.
  $effect(() => {
    void [name, driver, model, media, connType, address];
    affects = [];
  });

  const info = $derived(drivers.find((d) => d.driver === driver));

  // The model, chosen from every driver's, sets the driver behind it.
  function chooseModel(key: string) {
    const at = key.indexOf('|');
    driver = key.slice(0, at);
    model = key.slice(at + 1);
  }
  // Custom stock this model takes, if its driver takes any.
  const customStock = $derived(stockFor(info, model, stock));
  const customChosen = $derived(customStock.some((s) => s.id === media));

  // Keep model and media valid for the chosen driver.
  $effect(() => {
    if (!info) return;
    if (!info.models.includes(model)) model = info.models[0] ?? '';
    if (!info.media.some((m) => m.id === media) && !customStock.some((s) => s.id === media)) media = info.media[0]?.id ?? '';
  });

  function optionPayload(): Record<string, unknown> | null {
    const out: Record<string, unknown> = {};
    for (const opt of info?.options ?? []) {
      const v = options[opt.name];
      if (v === undefined || v === '') continue;
      if (opt.type === 'bool') {
        if (v !== opt.default) out[opt.name] = v;
      } else if (opt.type === 'int') {
        out[opt.name] = Number(v);
      } else if (v !== opt.default) {
        out[opt.name] = v;
      }
    }
    return Object.keys(out).length ? out : null;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = '';
    busy = true;
    const body = { name, driver, model, media, connection: { type: connType, address }, options: optionPayload() };
    try {
      if (initial) {
        const path = `/printers/${encodeURIComponent(initial.id)}`;
        if (!affects.length) {
          const { changes } = await request<{ changes: PrinterChanges }>('POST', `${path}/changes`, body);
          affects = describeChanges(changes);
          if (affects.length) return;
        }
        await request('PUT', path, body);
      } else {
        await request('POST', '/printers', body);
      }
      onsaved();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  const pid = initial?.id ?? 'new';
</script>

<form class="card space-y-4" onsubmit={save}>
  <h2 class="text-lg font-semibold">{initial ? `Edit “${initial.name}”` : 'New printer'}</h2>
  <div class="grid gap-4 sm:grid-cols-2">
    <div>
      <label class="label" for="{pid}-name">Name</label>
      <input id="{pid}-name" class="input" required bind:value={name} />
    </div>
    <div>
      <label class="label" for="{pid}-model">Printer model</label>
      <select id="{pid}-model" class="input" value="{driver}|{model}" onchange={(e) => chooseModel(e.currentTarget.value)}>
        {#each drivers as d (d.driver)}
          <optgroup label={driverName(d.driver)}>
            {#each d.models as m (m)}<option value="{d.driver}|{m}">{m}{d.model_dpi?.[m] ? ` (${d.model_dpi[m]} DPI)` : ''}</option>{/each}
          </optgroup>
        {/each}
      </select>
    </div>
    <div>
      <label class="label" for="{pid}-media">Roll loaded</label>
      <select id="{pid}-media" class="input" aria-describedby="{pid}-media-help" bind:value={media}>
        {#if customStock.length}
          <optgroup label="Built-in rolls">
            {#each info?.media ?? [] as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
          </optgroup>
          <optgroup label="Custom stock">
            {#each customStock as s (s.id)}<option value={s.id}>{s.name} ({describeStock(s)})</option>{/each}
          </optgroup>
        {:else}
          {#each info?.media ?? [] as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
        {/if}
      </select>
      <p id="{pid}-media-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        The labels in it now: die-cut rolls hold pre-cut labels, continuous ones are cut to length.
        {#if !info?.custom_stock}
          This driver takes only its own rolls.
        {:else if customChosen && info.custom_stock.note}
          {info.custom_stock.note}
        {:else}
          Or custom stock up to {info.custom_stock.max_width_mm[model] ?? '?'} mm wide, added under Label stock.
        {/if}
      </p>
    </div>
  </div>

  <fieldset class="grid gap-4 sm:grid-cols-[auto_1fr]">
    <legend class="label">Connection</legend>
    <div>
      <label class="text-xs" for="{pid}-conn">Type</label>
      <select id="{pid}-conn" class="input" bind:value={connType}>
        <option value="tcp">Network (most label printers)</option>
        <option value="device">Device on the BAM server</option>
        <option value="ipp">IPP / CUPS queue</option>
        <option value="windows">Windows printer</option>
      </select>
    </div>
    <div>
      <label class="text-xs" for="{pid}-address">Address</label>
      <input id="{pid}-address" class="input font-mono" required aria-describedby="{pid}-address-help" bind:value={address} />
      <p id="{pid}-address-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">{connectionHelp[connType]}</p>
    </div>
  </fieldset>

  {#if info?.options?.length}
    <fieldset class="grid gap-4 sm:grid-cols-2">
      <legend class="label">Options</legend>
      {#each info.options as opt (opt.name)}
        {@const oid = `${pid}-opt-${opt.name}`}
        <div>
          {#if opt.type === 'bool'}
            <label class="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={(options[opt.name] ?? opt.default) === true}
                onchange={(e) => (options[opt.name] = e.currentTarget.checked)}
                aria-describedby="{oid}-help"
              />
              {optionLabel(opt.name)}
            </label>
          {:else}
            <label class="text-xs" for={oid}>{optionLabel(opt.name)}</label>
            {#if opt.values?.length}
              <select id={oid} class="input" aria-describedby="{oid}-help" value={String(options[opt.name] ?? opt.default ?? '')} onchange={(e) => (options[opt.name] = e.currentTarget.value)}>
                {#each opt.values as v (v)}<option value={v}>{v}{v === opt.default ? ' (default)' : ''}</option>{/each}
              </select>
            {:else}
              <input
                id={oid}
                class="input"
                type={opt.type === 'int' ? 'number' : 'text'}
                placeholder={opt.default === null || opt.default === undefined ? 'Printer default' : String(opt.default)}
                aria-describedby="{oid}-help"
                value={String(options[opt.name] ?? '')}
                oninput={(e) => (options[opt.name] = e.currentTarget.value)}
              />
            {/if}
          {/if}
          <p id="{oid}-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">{opt.description}</p>
        </div>
      {/each}
    </fieldset>
  {/if}

  <ErrorMessage {error} />
  {#if affects.length}
    <div class="space-y-2 rounded-md border border-amber-300 p-3 dark:border-amber-700" role="group" aria-labelledby="{pid}-affects">
      <p id="{pid}-affects" class="text-sm font-medium">Saving this change would affect:</p>
      <ul class="list-disc space-y-1 pl-5 text-sm text-amber-900 dark:text-amber-200">
        {#each affects as a (a)}<li>{a}</li>{/each}
      </ul>
    </div>
  {/if}
  <div class="flex gap-2">
    <button type="submit" class="btn-primary" disabled={busy}>{initial ? (affects.length ? 'Save anyway' : 'Save') : 'Create printer'}</button>
    {#if affects.length}
      <button type="button" class="btn-secondary" onclick={() => (affects = [])}>Keep editing</button>
    {/if}
    <button type="button" class="btn-secondary" onclick={oncancel}>Cancel</button>
  </div>
</form>
