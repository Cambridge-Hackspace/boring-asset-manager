<script lang="ts">
  // An asset as the offline copy has it (docs/installing.md#offline): what
  // scanning it shows with no connection, marked as possibly out of date,
  // with using and restocking it recorded to send later.
  import { ago, categoryPaths, type OfflineAsset } from '../lib/offline/copy';
  import { offline } from '../lib/offline/offline.svelte';
  import { pendingFor } from '../lib/offline/queue';
  import { session } from '../lib/session.svelte';
  import QueuedChanges from './QueuedChanges.svelte';

  let { asset }: { asset: OfflineAsset } = $props();

  // Cleared while it is shown, as on signing out: what was shown goes too.
  const copy = $derived(offline.copy);
  const paths = $derived(copy ? categoryPaths(copy, asset) : []);
  const pending = $derived(pendingFor(offline.queue, asset.id));
  const unit = $derived(asset.stock?.unit ? ` ${asset.stock.unit}` : '');
  const conditions: Record<string, string> = { needs_maintenance: 'Needs maintenance', out_of_service: 'Out of service', problem: 'A problem is reported' };

  let thumb = $state<string | null>(null);
  $effect(() => {
    const photo = asset.photo;
    thumb = null;
    if (photo) void offline.thumb(asset.id, photo).then((url) => (thumb = url));
  });

  let amount = $state<number | null>(1);
  let status = $state('');

  async function record(kind: 'use' | 'restock') {
    if (!amount || amount < 1 || !session.user || !copy) return;
    const n = amount;
    const held = await offline.enqueue({ owner: session.user.id, kind, asset_id: asset.id, name: asset.name, unit: asset.stock?.unit, amount: n, since: copy.made_at });
    status = !held ? (kind === 'use' ? `Used ${n}${unit}.` : `Restocked ${n}${unit}.`) : held.state === 'waiting' ? 'Recorded on this device; it is sent when there is a connection.' : 'Not recorded; see below.';
  }
</script>

{#if copy}
<p role="status" class="mb-4 rounded border-2 border-zinc-900 bg-highlight px-3 py-2 text-sm text-zinc-900 dark:border-zinc-600">
  From the copy kept on this device, made {ago(copy.made_at)} ({new Date(copy.made_at).toLocaleString()}). It may be out of date.
</p>
<h1 class="mb-4 text-3xl font-bold">{asset.name}</h1>
<div class="flex flex-wrap items-start gap-6">
  {#if thumb}<img src={thumb} alt="" width="160" height="160" class="h-40 w-40 rounded border-2 border-zinc-900 object-cover dark:border-zinc-600" />{/if}
  <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
    {#if asset.codes?.length}<dt class="font-medium">Code</dt><dd class="font-mono">{asset.codes.join(', ')}</dd>{/if}
    {#if paths.length}<dt class="font-medium">Category</dt><dd>{paths.join('; ')}</dd>{/if}
    {#if asset.where}<dt class="font-medium">Where</dt><dd>{asset.where}</dd>{/if}
    {#if asset.stock}
      <dt class="font-medium">Stock</dt>
      <dd>
        {asset.stock.quantity}{unit}{#if asset.stock.threshold}, running low below {asset.stock.threshold}{/if}{#if asset.stock.low}<strong> · running low</strong>{/if}
        {#if pending}<span class="text-sm text-zinc-600 dark:text-zinc-400"> ({pending > 0 ? '+' : ''}{pending} waiting to send)</span>{/if}
      </dd>
    {/if}
    {#if asset.condition}<dt class="font-medium">Condition</dt><dd>{conditions[asset.condition] ?? asset.condition}</dd>{/if}
    {#if asset.loan}
      <dt class="font-medium">Lent</dt>
      <dd>{asset.loan.borrower ? `to ${asset.loan.borrower}` : 'out'}{asset.loan.due ? `, due back ${new Date(asset.loan.due).toLocaleDateString()}` : ''}</dd>
    {/if}
    {#if asset.review}<dt class="font-medium">Review</dt><dd class="capitalize">{asset.review}</dd>{/if}
  </dl>
</div>

{#if asset.record && asset.stock && session.user}
  <form class="mt-6 flex flex-wrap items-end gap-2" onsubmit={(e) => e.preventDefault()}>
    <div>
      <label class="label" for="offline-amount">How many{unit ? ` (${unit.trim()})` : ''}</label>
      <input id="offline-amount" class="input w-24" type="number" min="1" step="1" required bind:value={amount} />
    </div>
    <button type="button" class="btn-secondary" onclick={() => record('use')}>Use</button>
    <button type="button" class="btn-secondary" onclick={() => record('restock')}>Restock</button>
  </form>
{/if}
{#if status}<p class="mt-3 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}
<p class="mt-4 text-sm text-zinc-600 dark:text-zinc-400">Borrowing and returning need a connection, so that nothing is lent twice.</p>
<div class="mt-6"><QueuedChanges /></div>
{/if}
