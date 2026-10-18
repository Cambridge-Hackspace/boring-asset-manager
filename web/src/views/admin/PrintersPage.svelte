<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import type { AssetWithDetails, DriverInfo, LabelStock, Printer, TemplateSummary } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PrinterForm from './PrinterForm.svelte';
  import PoolsSection from './PoolsSection.svelte';
  import StockSection from './StockSection.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  let printers = $state<Printer[]>([]);
  let drivers = $state<DriverInfo[]>([]);
  let stock = $state<LabelStock[]>([]);
  let loading = $state(true);
  let error = $state('');
  let editing = $state<string | null>(null);

  // Test printing: a template and an asset to print it for.
  let testing = $state<string | null>(null);
  let templates = $state<TemplateSummary[]>([]);
  let testTemplate = $state('');
  let assetQuery = $state('');
  let assetResults = $state<AssetWithDetails[]>([]);
  let testAsset = $state<AssetWithDetails | null>(null);
  let testStatus = $state('');

  const canManage = $derived(session.can('settings.manage'));
  const canPrint = $derived(session.can('labels.print'));

  async function load() {
    try {
      [printers, drivers, stock] = await Promise.all([
        request<{ printers: Printer[] }>('GET', '/printers').then((r) => r.printers ?? []),
        request<{ drivers: DriverInfo[] }>('GET', '/printers/drivers').then((r) => r.drivers ?? []),
        request<{ stock: LabelStock[] }>('GET', '/label-stock').then((r) => r.stock ?? []),
      ]);
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  async function remove(p: Printer) {
    error = '';
    try {
      await request('DELETE', `/printers/${encodeURIComponent(p.id)}`);
      await load();
    } catch (err) {
      error = messageOf(err);
    }
  }

  async function startTest(p: Printer) {
    testing = p.id;
    testStatus = '';
    if (!templates.length) {
      try {
        templates = (await request<{ templates: TemplateSummary[] }>('GET', '/templates')).templates ?? [];
        testTemplate = templates[0]?.id ?? '';
      } catch (err) {
        testStatus = messageOf(err);
      }
    }
  }

  async function findAssets(event: SubmitEvent) {
    event.preventDefault();
    try {
      assetResults = ((await request<{ assets: AssetWithDetails[] }>('GET', `/assets?${new URLSearchParams({ search: assetQuery })}`)).assets ?? []).slice(0, 8);
    } catch (err) {
      testStatus = messageOf(err);
    }
  }

  async function testPrint(p: Printer) {
    if (!testAsset) return;
    testStatus = '';
    try {
      const res = await request<{ labels: number; warnings?: string[] }>('POST', `/printers/${encodeURIComponent(p.id)}/print`, {
        template_id: testTemplate,
        asset_ids: [testAsset.asset.id],
        copies: 1,
      });
      testStatus = [`Sent ${res.labels} label to ${p.name}.`, ...(res.warnings ?? [])].join(' ');
    } catch (err) {
      testStatus = messageOf(err);
    }
  }

  const connLabel = (p: Printer) => `${p.connection.type}: ${p.connection.address}`;
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <h1 class="text-2xl font-bold">Printers</h1>
  {#if canManage && editing !== 'new'}
    <button type="button" class="btn-primary" onclick={() => (editing = 'new')} disabled={!drivers.length}>Add printer</button>
  {/if}
</div>
<ErrorMessage {error} />

{#if editing === 'new'}
  <div class="mb-4"><PrinterForm {drivers} {stock} onsaved={() => { editing = null; void load(); }} oncancel={() => (editing = null)} /></div>
{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading printers…</p>
{:else if printers.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">No printers yet.</p>
{:else}
  <ul class="space-y-3">
    {#each printers as p (p.id)}
      <li>
        {#if editing === p.id}
          <PrinterForm printer={p} {drivers} {stock} onsaved={() => { editing = null; void load(); }} oncancel={() => (editing = null)} />
        {:else}
          <article class="card space-y-3" aria-labelledby="printer-{p.id}">
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div>
                <h2 id="printer-{p.id}" class="font-semibold">{p.name}</h2>
                <p class="text-sm text-zinc-600 dark:text-zinc-400">{p.model} ({p.driver}), {p.loaded?.name ?? p.media}</p>
                <p class="font-mono text-xs text-zinc-600 dark:text-zinc-400">{connLabel(p)}</p>
              </div>
              <div class="flex flex-wrap gap-2">
                {#if canPrint}<button type="button" class="btn-secondary" onclick={() => startTest(p)}>Test print</button>{/if}
                {#if canManage}
                  <button type="button" class="btn-secondary" aria-label="Edit {p.name}" onclick={() => (editing = p.id)}>Edit</button>
                  <ConfirmButton label="Delete" question="Remove “{p.name}”?" onconfirm={() => remove(p)} />
                {/if}
              </div>
            </div>

            {#if testing === p.id}
              <div class="space-y-2 border-t border-zinc-200 pt-3 dark:border-zinc-800">
                <div class="flex flex-wrap items-end gap-2">
                  <div>
                    <label class="label" for="test-template-{p.id}">Template</label>
                    <select id="test-template-{p.id}" class="input" bind:value={testTemplate}>
                      {#each templates as t (t.id)}<option value={t.id}>{t.name}</option>{/each}
                    </select>
                  </div>
                  <form class="flex items-end gap-2" role="search" onsubmit={findAssets}>
                    <div>
                      <label class="label" for="test-asset-{p.id}">Asset</label>
                      <input id="test-asset-{p.id}" class="input" type="search" placeholder="Name or code" bind:value={assetQuery} />
                    </div>
                    <button type="submit" class="btn-secondary">Find</button>
                  </form>
                </div>
                {#if assetResults.length}
                  <ul class="text-sm">
                    {#each assetResults as a (a.asset.id)}
                      <li><button type="button" class="hover:underline" onclick={() => { testAsset = a; assetResults = []; }}>{a.asset.name}</button></li>
                    {/each}
                  </ul>
                {/if}
                {#if testAsset}<p class="text-sm">Printing the label for <strong>{testAsset.asset.name}</strong>.</p>{/if}
                <button type="button" class="btn-primary" disabled={!testAsset || !testTemplate} onclick={() => testPrint(p)}>Print test label</button>
                {#if testStatus}<p class="text-sm" role="status">{testStatus}</p>{/if}
              </div>
            {/if}
          </article>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

{#if !loading}
  <PoolsSection {printers} />
  <StockSection {stock} onchange={load} />
{/if}
