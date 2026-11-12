<script lang="ts">
  import { untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { DriverInfo, LabelStock, Printer, PrinterChanges, PrinterConnection } from '../../lib/models';
  import { describeChanges, describeStock, driverName, mediaFor, optionLabel, optionValueLabel, stockFor } from '../../lib/stock';
  import { bleAddress, parseBLEAddress } from '../../lib/bluetooth';
  import { workArea } from '../../lib/lasers';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import type { Channel } from '../../lib/models';

  // Creates a printer, or edits one when printer is given. The form is
  // generated from the drivers' own descriptions.
  let {
    printer,
    drivers,
    stock = [],
    relays = [],
    onsaved,
    oncancel,
  }: { printer?: Printer; drivers: DriverInfo[]; stock?: LabelStock[]; relays?: string[]; onsaved: () => void; oncancel: () => void } = $props();

  const connectionHelp: Record<PrinterConnection['type'], string> = {
    tcp: 'The printer’s IP address or host name (port 9100 if none is given): a network printer, or a print server such as p910nd.',
    device: 'A device path on the BAM server, such as /dev/usb/lp0. It must be allowed in the server’s [printers] devices setting.',
    ipp: 'An IPP queue URI, such as ipp://cups.lan/printers/labels. The queue must be raw.',
    windows: 'A Windows printer name or \\\\host\\share. Only when BAM runs on Windows.',
    ble: 'The printer’s Bluetooth address, such as 7C:2F:80:A1:B2:C3. It must be allowed in the server’s [printers] bluetooth setting, and the server needs Bluetooth (BlueZ, on Linux).',
    browser:
      'Printed to from a browser with Web Bluetooth, such as Chrome on Android or a computer, over the browser’s own Bluetooth: whoever prints picks the printer then. The server needs no Bluetooth, and the spool cannot print to it.',
    openepaperlink: 'The access point, such as 192.168.1.50, then ?tag= and the tag’s MAC address, such as 192.168.1.50?tag=0000021E3D6F1234.',
    pull: 'The display fetches its image from BAM!, with a token made under Displays.',
    webhook: 'The webhook channel BAM! posts the image through, set up under Notifications.',
  };

  // Connection types as the form offers them.
  const connectionNames: Record<PrinterConnection['type'], string> = {
    tcp: 'Network (most label printers)',
    device: 'Device on the BAM server',
    ipp: 'IPP / CUPS queue',
    windows: 'Windows printer',
    ble: 'Bluetooth LE',
    browser: 'A browser’s Bluetooth',
    openepaperlink: 'OpenEPaperLink access point',
    pull: 'The display fetches it',
    webhook: 'Webhook',
  };
  const printerConnections: PrinterConnection['type'][] = ['tcp', 'device', 'ipp', 'windows', 'ble', 'browser'];

  const initial = untrack(() => $state.snapshot(printer));
  let name = $state(initial?.name ?? '');
  let driver = $state(initial?.driver ?? untrack(() => drivers[0]?.driver) ?? '');
  let model = $state(initial?.model ?? '');
  let media = $state(initial?.media ?? '');
  let connType = $state<PrinterConnection['type']>(initial?.connection.type ?? 'tcp');
  // A Bluetooth LE printer's address is entered in parts: its Bluetooth
  // address, and the service and characteristics where they differ from
  // the driver's. A browser's has no Bluetooth address, and may set how
  // much one write carries.
  const bluetoothType = (t: PrinterConnection['type'] | undefined) => t === 'ble' || t === 'browser';
  const initialBLE = parseBLEAddress(bluetoothType(initial?.connection.type) ? initial!.connection.address : '');
  let address = $state(bluetoothType(initial?.connection.type) ? initialBLE.mac : (initial?.connection.address ?? ''));
  let ble = $state({ service: initialBLE.service, write: initialBLE.write, notify: initialBLE.notify, chunk: initialBLE.chunk });
  // The relay the printer is reached through (docs/relays.md); '' is this server.
  let relay = $state(initial?.connection.relay ?? '');
  // The relays offered: those paired, and the one the printer names even if it is not.
  const relayChoices = $derived([...new Set([...relays, ...(initial?.connection.relay ? [initial.connection.relay] : [])])]);
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
    void [name, driver, model, media, connType, address, relay, ble.service, ble.write, ble.notify, ble.chunk];
    affects = [];
  });

  const info = $derived(drivers.find((d) => d.driver === driver));
  // A display's driver takes only its own connections, and a printer's none of those.
  const connections = $derived(info?.display?.connections ?? printerConnections);
  $effect(() => {
    if (!connections.includes(connType)) connType = connections[0] ?? 'tcp';
  });
  // A display that pulls has no address, but a TRMNL's is its MAC address.
  const needsAddress = $derived((connType !== 'pull' || driver === 'trmnl') && connType !== 'browser');
  // The server's own connections never go through a relay, nor a browser's, nor a laser.
  const relayable = $derived(connType !== 'pull' && connType !== 'webhook' && connType !== 'browser' && !info?.one_shot);
  // A laser's model, with its work area (docs/printing.md#lasers).
  const laser = $derived(info?.one_shot?.find((m) => m.id === model));
  // Its work area as the options set it, for a driver whose machines come in many sizes.
  const area = $derived(laser ? workArea(laser, { work_width_mm: Number(options.work_width_mm) || undefined, work_height_mm: Number(options.work_height_mm) || undefined }) : null);

  // Webhook channels, for a display pushed through one; read when first needed.
  let channels = $state<Channel[] | null>(null);
  $effect(() => {
    if (connType !== 'webhook' || channels !== null) return;
    channels = [];
    void request<{ channels: Channel[] }>('GET', '/notifiers').then(
      (r) => (channels = (r.channels ?? []).filter((c) => c.driver === 'webhook')),
      () => (channels = []),
    );
  });

  // The model, chosen from every driver's, sets the driver behind it.
  function chooseModel(key: string) {
    const at = key.indexOf('|');
    driver = key.slice(0, at);
    model = key.slice(at + 1);
  }
  // The built-in rolls this model takes.
  const builtIn = $derived(mediaFor(info, model));
  // Custom stock this model takes, if its driver takes any.
  const customStock = $derived(stockFor(info, model, stock));
  const customChosen = $derived(customStock.some((s) => s.id === media));

  // Keep model and media valid for the chosen driver.
  $effect(() => {
    if (!info) return;
    if (!info.models.includes(model)) model = info.models[0] ?? '';
    if (!builtIn.some((m) => m.id === media) && !customStock.some((s) => s.id === media)) media = builtIn[0]?.id ?? '';
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
    const sentAddress =
      connType === 'ble'
        ? bleAddress({ ...initialBLE, ...ble, mac: address, chunk: '' })
        : connType === 'browser'
          ? bleAddress({ ...initialBLE, ...ble, mac: '' })
          : needsAddress
            ? address
            : '';
    const body = { name, driver, model, media, connection: { type: connType, address: sentAddress, ...(relay && relayable ? { relay } : {}) }, options: optionPayload() };
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
      <label class="label" for="{pid}-model">{info?.display ? 'Display model' : 'Printer model'}</label>
      <select id="{pid}-model" class="input" value="{driver}|{model}" onchange={(e) => chooseModel(e.currentTarget.value)}>
        {#each drivers as d (d.driver)}
          <optgroup label={driverName(d.driver)}>
            {#each d.models as m (m)}<option value="{d.driver}|{m}">{m}{d.model_dpi?.[m] ? ` (${d.model_dpi[m]} DPI)` : ''}</option>{/each}
          </optgroup>
        {/each}
      </select>
    </div>
    {#if info?.one_shot}
      <p class="text-sm text-zinc-600 dark:text-zinc-400 sm:self-end">
        A laser has no roll: each job names its material and workpiece.{area ? ` Its work area is ${area.work_width_mm} × ${area.work_height_mm} mm.` : ''}
      </p>
    {:else}
    <div>
      <label class="label" for="{pid}-media">{info?.display ? 'Screen' : 'Roll loaded'}</label>
      <select id="{pid}-media" class="input" aria-describedby="{pid}-media-help" bind:value={media}>
        {#if customStock.length}
          <optgroup label="Built-in rolls">
            {#each builtIn as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
          </optgroup>
          <optgroup label="Custom stock">
            {#each customStock as s (s.id)}<option value={s.id}>{s.name} ({describeStock(s)})</option>{/each}
          </optgroup>
        {:else}
          {#each builtIn as m (m.id)}<option value={m.id}>{m.name}</option>{/each}
        {/if}
      </select>
      <p id="{pid}-media-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
        {#if info?.display}
          A display’s screen is a label of its size: templates of that size show on it.
        {:else}
          The labels in it now: die-cut rolls hold pre-cut labels, continuous ones are cut to length.
        {/if}
        {#if info?.display}
          <!-- Nothing more to say of a screen. -->
        {:else if !info?.custom_stock}
          This driver takes only its own rolls.
        {:else if customChosen && info.custom_stock.note}
          {info.custom_stock.note}
        {:else}
          Or custom stock up to {info.custom_stock.max_width_mm[model] ?? '?'} mm wide, added under Label stock.
        {/if}
      </p>
    </div>
    {/if}
  </div>

  <fieldset class="grid gap-4 sm:grid-cols-[auto_1fr]">
    <legend class="label">Connection</legend>
    <div>
      <label class="text-xs" for="{pid}-conn">Type</label>
      <select id="{pid}-conn" class="input" bind:value={connType}>
        {#each connections as c (c)}<option value={c}>{connectionNames[c]}</option>{/each}
      </select>
    </div>
    <div>
      {#if connType === 'webhook'}
        <label class="text-xs" for="{pid}-address">Webhook channel</label>
        <select id="{pid}-address" class="input" required aria-describedby="{pid}-address-help" bind:value={address}>
          <option value="" disabled>Choose a channel</option>
          {#each channels ?? [] as c (c.id)}<option value={c.id}>{c.name}{c.enabled ? '' : ' (off)'}</option>{/each}
        </select>
        <p id="{pid}-address-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">{connectionHelp[connType]}</p>
      {:else if needsAddress}
        <label class="text-xs" for="{pid}-address">{connType === 'pull' ? 'MAC address' : 'Address'}</label>
        <input id="{pid}-address" class="input font-mono" required aria-describedby="{pid}-address-help" bind:value={address} />
        <p id="{pid}-address-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
          {#if relay && relayable}
            On {relay}’s network: the relay’s own allowlist decides whether it may reach it.
          {:else if connType === 'pull'}
            The TRMNL’s MAC address, such as AA:BB:CC:DD:EE:FF, which it gives when it sets itself up.
          {:else}
            {connectionHelp[connType]}
          {/if}
        </p>
      {:else}
        <p class="text-xs text-zinc-600 dark:text-zinc-400" id="{pid}-address-help">{connectionHelp[connType]}</p>
      {/if}
    </div>
    {#if connType === 'ble' || connType === 'browser'}
      <div class="grid gap-4 sm:col-span-2 sm:grid-cols-3">
        <div>
          <label class="text-xs" for="{pid}-ble-service">Service</label>
          <input id="{pid}-ble-service" class="input font-mono" aria-describedby="{pid}-ble-help" placeholder={info?.ble?.service ?? 'Required'} bind:value={ble.service} />
        </div>
        <div>
          <label class="text-xs" for="{pid}-ble-write">Write characteristic</label>
          <input id="{pid}-ble-write" class="input font-mono" aria-describedby="{pid}-ble-help" placeholder={info?.ble?.write ?? 'Required'} bind:value={ble.write} />
        </div>
        <div>
          <label class="text-xs" for="{pid}-ble-notify">Notify characteristic</label>
          <input id="{pid}-ble-notify" class="input font-mono" aria-describedby="{pid}-ble-help" placeholder={info?.ble ? (info.ble.notify ?? 'None') : 'Optional'} bind:value={ble.notify} />
        </div>
        {#if connType === 'browser'}
          <div>
            <label class="text-xs" for="{pid}-ble-chunk">Bytes per write</label>
            <input id="{pid}-ble-chunk" class="input" inputmode="numeric" pattern="[0-9]*" aria-describedby="{pid}-chunk-help" placeholder="20" bind:value={ble.chunk} />
            <p id="{pid}-chunk-help" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
              A browser cannot learn how much its link to the printer carries, so it writes 20 bytes at a time, which every link takes. Raise it, up to 512, if the printer takes more.
            </p>
          </div>
        {/if}
        <p id="{pid}-ble-help" class="text-xs text-zinc-600 sm:col-span-3 dark:text-zinc-400">
          {#if info?.ble}
            UUIDs, such as fff0 or 0000fff0-0000-1000-8000-00805f9b34fb. Leave them blank for the driver’s own.
          {:else}
            UUIDs, such as fff0 or 0000fff0-0000-1000-8000-00805f9b34fb, from the printer’s documentation: this driver has none of its own.
          {/if}
        </p>
      </div>
    {/if}
    {#if relayChoices.length && relayable}
      <div class="sm:col-span-2">
        <label class="text-xs" for="{pid}-relay">Reached through</label>
        <select id="{pid}-relay" class="input" bind:value={relay}>
          <option value="">This server</option>
          {#each relayChoices as r (r)}<option value={r}>Relay {r}</option>{/each}
        </select>
      </div>
    {/if}
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
              <select
                id={oid}
                class="input"
                aria-describedby="{oid}-help"
                value={String(options[opt.name] ?? opt.default ?? '')}
                onchange={(e) => {
                  // Blank, for an option with no default, keeps the printer's own setting.
                  if (e.currentTarget.value === '') delete options[opt.name];
                  else options[opt.name] = e.currentTarget.value;
                }}
              >
                {#if opt.default === null || opt.default === undefined}<option value="">Printer default</option>{/if}
                {#each opt.values as v (v)}<option value={v}>{optionValueLabel(v)}{v === opt.default ? ' (default)' : ''}</option>{/each}
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
