<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { request } from '../../lib/api';
  import { amount, conditionLabel, describeChange, describeEvent, isWebURL, keptValues } from '../../lib/assets';
  import { messageOf } from '../../lib/errors';
  import { actorName } from '../../lib/people';
  import type { AssetEvent, AssetWithDetails, Condition, GeneratorInfo, GeneratorKey } from '../../lib/models';
  import { navigate } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import CategoryBadge from '../../components/CategoryBadge.svelte';
  import ConfirmButton from '../../components/ConfirmButton.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import ConditionBadge from '../../components/ConditionBadge.svelte';
  import LabelActions from '../../components/LabelActions.svelte';
  import LinksPanel from '../../components/LinksPanel.svelte';
  import PhotosPanel from '../../components/PhotosPanel.svelte';
  import LoanPanel from '../../components/LoanPanel.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const id = untrack(() => params.id ?? '');

  let asset = $state<AssetWithDetails | null>(null);
  let generators = $state<GeneratorInfo[]>([]);
  let history = $state<AssetEvent[]>([]);
  let olderHistory = $state(false);
  let error = $state('');
  let saved = $state('');
  let loading = $state(true);
  let busy = $state(false);
  let howMany = $state<number | null>(1);

  const path = `/assets/${encodeURIComponent(id)}`;

  onMount(async () => {
    try {
      [asset, generators] = await Promise.all([
        request<{ asset: AssetWithDetails }>('GET', path).then((r) => r.asset),
        request<{ generators: GeneratorInfo[] }>('GET', '/generators').then((r) => r.generators ?? []),
        loadHistory(),
      ]);
    } catch (err) {
      error = messageOf(err);
    } finally {
      loading = false;
    }
  });

  // Loads the newest page of history, or with before, the page older than it.
  async function loadHistory(before?: string) {
    const query = before ? `?before=${encodeURIComponent(before)}` : '';
    const page = await request<{ events: AssetEvent[]; more: boolean }>('GET', `${path}/history${query}`);
    history = before ? [...history, ...(page.events ?? [])] : (page.events ?? []);
    olderHistory = page.more;
  }

  async function showOlder() {
    error = '';
    try {
      await loadHistory(history[history.length - 1]?.id);
    } catch (err) {
      error = messageOf(err);
    }
  }

  const archived = $derived(!!asset?.asset.archived_at);

  const generatorName = (type: string) => generators.find((g) => g.type === type)?.name ?? type;

  // Runs a stock action, then shows the asset and history as they are now.
  async function stockAction(action: () => Promise<{ asset: AssetWithDetails }>, done: string) {
    error = '';
    saved = '';
    busy = true;
    try {
      asset = (await action()).asset;
      saved = done;
      await loadHistory();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  function adjust(sign: 1 | -1) {
    const n = howMany ?? 0;
    if (!Number.isInteger(n) || n < 1) {
      error = 'Enter a whole number of at least 1.';
      return;
    }
    const unit = asset?.asset.stock?.unit;
    void stockAction(
      () => request('POST', `${path}/quantity`, { adjust: sign * n }),
      sign < 0 ? `Used ${amount(n, unit)}.` : `Added ${amount(n, unit)}.`,
    );
  }

  function setRunningLow(low: boolean) {
    void stockAction(
      () => request('PUT', `${path}/running-low`, { running_low: low }),
      low ? 'Marked as running low.' : 'Marked as restocked.',
    );
  }

  const when = (iso: string) => new Date(iso).toLocaleString();

  // Reporting a problem, or clearing one.
  let reporting = $state(false);
  let reportCondition = $state<Condition>('needs_maintenance');
  let reportNote = $state('');

  function startReport() {
    reporting = true;
    reportCondition = asset?.asset.condition === 'out_of_service' ? 'out_of_service' : 'needs_maintenance';
    reportNote = asset?.asset.condition_note ?? '';
  }

  function report(event: SubmitEvent) {
    event.preventDefault();
    void stockAction(async () => {
      const r = await request<{ asset: AssetWithDetails }>('PUT', `${path}/condition`, { condition: reportCondition, note: reportNote });
      reporting = false;
      return r;
    }, `Reported: ${conditionLabel(reportCondition).toLowerCase()}.`);
  }

  // After a loan or a link changes: show the asset as it is now.
  function assetChanged(a: AssetWithDetails, done: string) {
    asset = a;
    saved = done;
    error = '';
    void loadHistory();
  }

  function markWorking() {
    void stockAction(() => request('PUT', `${path}/condition`, { condition: 'ok' }), 'Marked as working.');
  }

  async function restore() {
    error = '';
    saved = '';
    busy = true;
    try {
      const r = await request<{ asset: AssetWithDetails; skipped_codes: GeneratorKey[] }>('POST', `${path}/restore`);
      asset = r.asset;
      const skipped = (r.skipped_codes ?? []).map((k) => `${generatorName(k.generator_type)} ${k.assigned_code}`);
      saved = skipped.length ? `Restored. Other assets have taken ${skipped.join(', ')}, so it did not get them back.` : 'Restored.';
      await loadHistory();
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function archive() {
    try {
      await request('DELETE', `/assets/${encodeURIComponent(id)}`);
      navigate('/assets');
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<nav aria-label="Breadcrumb" class="mb-2 text-sm"><a class="underline" href="/assets">Assets</a></nav>
<ErrorMessage {error} />
{#if saved}<p class="mb-2 text-sm text-green-700 dark:text-green-400" role="status">{saved}</p>{/if}

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading…</p>
{:else if asset}
  <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
    <h1 class="text-2xl font-bold">{asset.asset.name}</h1>
    <div class="flex flex-wrap gap-2">
      {#if archived}
        {#if session.can('assets:delete')}
          <button type="button" class="btn-primary" disabled={busy} onclick={restore}>Restore</button>
        {/if}
      {:else}
        {#if session.can('assets:write')}<a class="btn-secondary" href="/assets/{encodeURIComponent(id)}/edit">Edit</a>{/if}
        {#if session.canAny('assets:report', 'assets:write') && !reporting}
          <button type="button" class="btn-secondary" onclick={startReport}>Report a problem</button>
        {/if}
        {#if asset.asset.condition !== 'ok' && session.can('assets:write')}
          <button type="button" class="btn-secondary" disabled={busy} onclick={markWorking}>Mark as working</button>
        {/if}
        {#if session.can('assets:delete')}
          <ConfirmButton label="Archive" question="Archive this asset? Its codes become free for reuse." onconfirm={archive} />
        {/if}
      {/if}
    </div>
  </div>
  {#if asset.location.length}
    <nav aria-label="Location" class="-mt-2 mb-4 text-sm text-zinc-600 dark:text-zinc-400">
      In
      {#each asset.location as place, i (place.id)}{#if i > 0}{' › '}{/if}<a class="underline" href="/assets/{encodeURIComponent(place.id)}">{place.name}</a>{/each}
    </nav>
  {/if}
  {#if asset.asset.archived_at}
    <p class="mb-4 rounded border border-zinc-300 bg-zinc-100 p-3 text-sm dark:border-zinc-700 dark:bg-zinc-800">
      Archived {when(asset.asset.archived_at)}. Its codes were released for other assets; restoring it gives back those no other asset has taken.
    </p>
  {/if}
  {#if asset.asset.condition !== 'ok'}
    <p class="mb-4 flex flex-wrap items-center gap-2 text-sm">
      <ConditionBadge condition={asset.asset.condition} />
      {#if asset.asset.condition_note}<span>{asset.asset.condition_note}</span>{/if}
    </p>
  {/if}
  {#if reporting}
    <form class="card mb-4 max-w-xl space-y-3" onsubmit={report}>
      <h2 class="text-lg font-semibold">Report a problem</h2>
      <fieldset>
        <legend class="label">How bad is it?</legend>
        <label class="mr-4 inline-flex items-center gap-2 text-sm">
          <input type="radio" name="condition" value="needs_maintenance" bind:group={reportCondition} /> Needs maintenance, still usable
        </label>
        <label class="inline-flex items-center gap-2 text-sm">
          <input type="radio" name="condition" value="out_of_service" bind:group={reportCondition} /> Out of service, do not use
        </label>
      </fieldset>
      <div>
        <label class="label" for="condition-note">What is wrong?</label>
        <input id="condition-note" class="input" maxlength="255" placeholder="The chuck slips" bind:value={reportNote} />
      </div>
      <div class="flex gap-2">
        <button type="submit" class="btn-primary" disabled={busy}>Report</button>
        <button type="button" class="btn-secondary" onclick={() => (reporting = false)}>Cancel</button>
      </div>
    </form>
  {/if}

  <div class="grid gap-6 lg:grid-cols-2">
    {#if asset.asset.stock}
      {@const stock = asset.asset.stock}
      <section class="card" aria-labelledby="stock-heading">
        <h2 id="stock-heading" class="mb-1 text-lg font-semibold">Stock</h2>
        <p class="text-sm">
          <strong>{stock.is_running_low ? 'Running low.' : 'In stock.'}</strong>
          {amount(stock.quantity, stock.unit)} left{stock.low_stock_threshold
            ? `; flagged automatically below ${amount(stock.low_stock_threshold, stock.unit)}`
            : ''}.
        </p>
        {#if session.can('stock:record') && !archived}
          <div class="mt-3 flex flex-wrap items-end gap-2">
            <div>
              <label class="label" for="stock-amount">How many{stock.unit ? ` (${stock.unit})` : ''}</label>
              <input id="stock-amount" class="input w-28" type="number" min="1" step="1" bind:value={howMany} />
            </div>
            <button type="button" class="btn-secondary" disabled={busy} onclick={() => adjust(-1)}>Use</button>
            <button type="button" class="btn-secondary" disabled={busy} onclick={() => adjust(1)}>Add</button>
            {#if stock.is_running_low}
              <button type="button" class="btn-secondary" disabled={busy} onclick={() => setRunningLow(false)}>Mark restocked</button>
            {:else}
              <button type="button" class="btn-secondary" disabled={busy} onclick={() => setRunningLow(true)}>Mark running low</button>
            {/if}
          </div>
        {/if}
      </section>
    {/if}

    {#if (asset.lendable || asset.loan) && !archived}
      <LoanPanel {asset} onchange={assetChanged} />
    {/if}

    <section class="card" aria-labelledby="details-heading">
      <h2 id="details-heading" class="mb-3 text-lg font-semibold">Details</h2>
      <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
        <dt class="font-medium">Categories</dt>
        <dd class="flex flex-wrap gap-1">
          {#each asset.categories as c (c.id)}<CategoryBadge category={c} href="/categories/{encodeURIComponent(c.id)}" />{/each}
        </dd>
        {#each asset.effective_fields as e (e.field.id)}
          <dt class="font-medium">{e.field.label}</dt>
          <dd>
            {#if asset.field_values[e.field.key]}
              {@const value = asset.field_values[e.field.key]!}
              {#if e.field.validation_type === 'url' && isWebURL(value)}
                <a class="underline" href={value} rel="noopener noreferrer" target="_blank">{value}</a>
              {:else if e.field.validation_type === 'select'}
                {e.field.options?.choices.find((c) => c.value === value)?.label ?? value}
              {:else}
                {value}
              {/if}
            {:else}
              <span class="text-zinc-600 dark:text-zinc-400">{e.is_required ? 'Missing' : '—'}</span>
            {/if}
          </dd>
        {/each}
        {#each asset.generator_keys as k (k.generator_type)}
          <dt class="font-medium">{generatorName(k.generator_type)}</dt>
          <dd class="font-mono">{k.assigned_code}</dd>
        {/each}
      </dl>
      {#if keptValues(asset.field_values, asset.effective_fields).length}
        <details class="mt-4 text-sm">
          <summary class="cursor-pointer">Values kept from former categories</summary>
          <dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
            {#each keptValues(asset.field_values, asset.effective_fields) as [key, value] (key)}
              <dt class="font-mono">{key}</dt>
              <dd>{value}</dd>
            {/each}
          </dl>
        </details>
      {/if}
    </section>

    <PhotosPanel {asset} onchange={assetChanged} />
    <LinksPanel {asset} onchange={assetChanged} />

    {#if !archived}<LabelActions assetIds={[id]} />{/if}

    {#if history.length}
      <section class="card" aria-labelledby="history-heading">
        <h2 id="history-heading" class="mb-3 text-lg font-semibold">History</h2>
        <ul class="space-y-2 text-sm">
          {#each history as e (e.id)}
            <li>
              <span class="font-medium">{describeEvent(e)}</span>
              {#if e.payload.changes?.length}
                <ul class="list-disc pl-5">
                  {#each e.payload.changes as c, i (i)}<li>{describeChange(c, e.kind)}</li>{/each}
                </ul>
              {/if}
              <span class="block text-xs text-zinc-600 dark:text-zinc-400">
                {when(e.created_at)}{e.payload.actor ? ` · ${actorName(e.payload.actor)}` : ''}
              </span>
            </li>
          {/each}
        </ul>
        {#if olderHistory}
          <button type="button" class="btn-secondary mt-3" onclick={showOlder}>Show older</button>
        {/if}
      </section>
    {/if}
  </div>
{/if}
