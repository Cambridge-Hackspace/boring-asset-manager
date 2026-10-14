<script lang="ts">
  import { untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { DriverInfo, Printer, PrinterConnection } from '../../lib/models';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // Creates a printer, or edits one when printer is given. The form is
  // generated from the drivers' own descriptions.
  let {
    printer,
    drivers,
    onsaved,
    oncancel,
  }: { printer?: Printer; drivers: DriverInfo[]; onsaved: () => void; oncancel: () => void } = $props();

  const connectionHelp: Record<PrinterConnection['type'], string> = {
    tcp: 'host or host:port (port 9100 if omitted): a network printer, or a print server such as p910nd.',
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

  const info = $derived(drivers.find((d) => d.driver === driver));

  // Keep model and media valid for the chosen driver.
  $effect(() => {
    if (!info) return;
    if (!info.models.includes(model)) model = info.models[0] ?? '';
    if (!info.media.some((m) => m.id === media)) media = info.media[0]?.id ?? '';
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
      if (initial) await request('PUT', `/printers/${encodeURIComponent(initial.id)}`, body);
      else await request('POST', '/printers', body);
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
      <label class="label" for="{pid}-driver">Driver</label>
      <select id="{pid}-driver" class="input" bind:value={driver}>
        {#each drivers as d (d.driver)}<option value={d.driver}>{d.driver}</option>{/each}
      </select>
    </div>
    <div>
      <label class="label" for="{pid}-model">Model</label>
      <select id="{pid}-model" class="input" bind:value={model}>
        {#each info?.models ?? [] as m (m)}<option value={m}>{m}{info?.model_dpi?.[m] ? ` (${info.model_dpi[m]} DPI)` : ''}</option>{/each}
      </select>
    </div>
    <div>
      <label class="label" for="{pid}-media">Loaded media</label>
      <select id="{pid}-media" class="input" bind:value={media}>
        {#each info?.media ?? [] as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
      </select>
    </div>
  </div>

  <fieldset class="grid gap-4 sm:grid-cols-[auto_1fr]">
    <legend class="label">Connection</legend>
    <div>
      <label class="text-xs" for="{pid}-conn">Type</label>
      <select id="{pid}-conn" class="input" bind:value={connType}>
        <option value="tcp">Network (raw TCP)</option>
        <option value="device">Device on the BAM server</option>
        <option value="ipp">IPP / CUPS queue</option>
        <option value="windows">Windows printer</option>
      </select>
    </div>
    <div>
      <label class="text-xs" for="{pid}-address">Address</label>
      <input id="{pid}-address" class="input font-mono" required aria-describedby="{pid}-address-help" bind:value={address} />
      <p id="{pid}-address-help" class="mt-1 text-xs text-zinc-500">{connectionHelp[connType]}</p>
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
              {opt.name}
            </label>
          {:else}
            <label class="text-xs" for={oid}>{opt.name}</label>
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
          <p id="{oid}-help" class="mt-1 text-xs text-zinc-500">{opt.description}</p>
        </div>
      {/each}
    </fieldset>
  {/if}

  <ErrorMessage {error} />
  <div class="flex gap-2">
    <button type="submit" class="btn-primary" disabled={busy}>{initial ? 'Save' : 'Add printer'}</button>
    <button type="button" class="btn-secondary" onclick={oncancel}>Cancel</button>
  </div>
</form>
