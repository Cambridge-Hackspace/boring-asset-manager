<!--
  RFID tags (docs/rfid.md): the scheme that writes asset codes to the
  long-range tags of printers set to write them, and reads them back from
  what a reader types.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { RFIDSettings } from '../../lib/models';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  const schemes: { value: RFIDSettings['scheme']; label: string }[] = [
    { value: 'text', label: "Text: the code's own characters" },
    { value: 'giai-96', label: 'GIAI-96: GS1, numeric codes' },
    { value: 'giai-202', label: 'GIAI-202: GS1, codes of letters and digits' },
  ];
  const codeTypes = [
    { value: '', label: 'Its first code, as labels show it' },
    { value: 'qr', label: 'QR' },
    { value: 'micro_qr', label: 'Micro QR' },
    { value: 'code128', label: 'Code 128' },
  ];

  let scheme = $state<RFIDSettings['scheme']>('text');
  let companyPrefix = $state('');
  let filter = $state(0);
  let codeType = $state('');
  let error = $state('');
  let status = $state('');
  let busy = $state(false);
  let loaded = $state(false);
  const gs1 = $derived(scheme !== 'text');

  function show(s: RFIDSettings) {
    [scheme, companyPrefix, filter, codeType] = [s.scheme, s.company_prefix, s.filter, s.code_type];
    loaded = true;
  }

  onMount(async () => {
    try {
      show(await request<RFIDSettings>('GET', '/settings/rfid'));
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    busy = true;
    try {
      const body: RFIDSettings = { scheme, company_prefix: gs1 ? companyPrefix.trim() : '', filter: gs1 ? Number(filter) : 0, code_type: codeType };
      show(await request<RFIDSettings>('PUT', '/settings/rfid', body));
      status = 'Saved.';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }
</script>

<section class="card mb-6 max-w-3xl space-y-3" aria-labelledby="rfid-settings-heading">
  <h2 id="rfid-settings-heading" class="text-lg font-semibold">RFID tags</h2>
  <p class="text-sm text-zinc-600 dark:text-zinc-400">
    How an asset's code is written to the long-range tag in its label, by ZPL printers with <em>rfid</em> on, and read back from what a
    reader types on the Scan page.
  </p>
  <ErrorMessage {error} />
  <form class="space-y-3" onsubmit={save}>
    <div class="flex flex-wrap items-end gap-3">
      <div>
        <label class="label" for="rfid-scheme">Scheme</label>
        <select id="rfid-scheme" class="input" disabled={!loaded} bind:value={scheme}>
          {#each schemes as s (s.value)}<option value={s.value}>{s.label}</option>{/each}
        </select>
      </div>
      <div>
        <label class="label" for="rfid-code-type">Code a tag holds</label>
        <select id="rfid-code-type" class="input" disabled={!loaded} bind:value={codeType}>
          {#each codeTypes as c (c.value)}<option value={c.value}>{c.label}</option>{/each}
        </select>
      </div>
    </div>
    {#if gs1}
      <div class="flex flex-wrap items-end gap-3">
        <div>
          <label class="label" for="rfid-prefix">GS1 company prefix</label>
          <input
            id="rfid-prefix"
            class="input w-40 font-mono"
            inputmode="numeric"
            pattern={'[0-9]{6,12}'}
            title="6 to 12 digits"
            required
            disabled={!loaded}
            bind:value={companyPrefix}
          />
        </div>
        <div>
          <label class="label" for="rfid-filter">Filter</label>
          <input id="rfid-filter" class="input w-20" type="number" min="0" max="7" required disabled={!loaded} bind:value={filter} />
        </div>
      </div>
    {/if}
    <div class="flex items-center gap-3">
      <button type="submit" class="btn-secondary" disabled={busy || !loaded}>Save</button>
      {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
    </div>
  </form>
</section>
