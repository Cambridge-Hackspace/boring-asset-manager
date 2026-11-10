<script lang="ts">
  // /shopping, the shopping list (docs/stock-and-lending.md#the-shopping-list),
  // and the installed app's Shopping list shortcut: what is running low, of
  // what the person can see, by category, with where to buy it. Checking
  // something off as bought, with how many, records a restock; with no
  // connection, the list is the offline copy's, and the restock is sent
  // later, refused if someone restocked it meanwhile (docs/installing.md#offline).
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import { messageOf } from '../lib/errors';
  import { isOffline } from '../lib/install/app.svelte';
  import { ago, shoppingGroups, suggestedRestock, type ShoppingItem, type ShoppingList } from '../lib/offline/copy';
  import { offline } from '../lib/offline/offline.svelte';
  import { pendingFor } from '../lib/offline/queue';
  import { session } from '../lib/session.svelte';
  import ErrorMessage from '../components/ErrorMessage.svelte';
  import PageHeader from '../components/PageHeader.svelte';
  import QueuedChanges from '../components/QueuedChanges.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  const link = 'inline-flex min-h-11 items-center underline';

  let list = $state<ShoppingList | null>(null);
  /** The list is the offline copy's: as it was when the copy was made. */
  let fromCopy = $state(false);
  let error = $state('');
  let status = $state('');
  let amounts = $state<Record<string, number | null>>({});
  let busy = $state('');

  const groups = $derived(list ? shoppingGroups(list.items) : []);

  function take(l: ShoppingList, copied: boolean) {
    list = l;
    fromCopy = copied;
    for (const item of l.items) if (item.asset_id && amounts[item.asset_id] === undefined) amounts[item.asset_id] = suggestedRestock(item);
  }

  async function load() {
    error = '';
    if (!session.offline) {
      try {
        take(await request<ShoppingList>('GET', '/shopping'), false);
        return;
      } catch (err) {
        if (!isOffline(err)) {
          error = messageOf(err);
          return;
        }
      }
    }
    await offline.load();
    if (offline.copy) take(offline.copy.shopping, true);
    else error = "You're offline, and no copy is kept on this device to show the list from.";
  }
  onMount(load);

  async function bought(event: SubmitEvent, item: ShoppingItem) {
    event.preventDefault();
    const id = item.asset_id!;
    const n = amounts[id];
    if (!n || n < 1 || !list || !session.user) return;
    status = '';
    busy = id;
    try {
      const held = await offline.enqueue({ owner: session.user.id, kind: 'restock', asset_id: id, name: item.name, unit: item.unit, amount: n, since: list.made_at });
      const unit = item.unit ? ` ${item.unit}` : '';
      if (!held) {
        status = `Restocked ${item.name} by ${n}${unit}.`;
        if (!fromCopy) await load();
      } else if (held.state === 'waiting') {
        status = `${item.name}: ${n}${unit} bought, recorded on this device. It is sent when there is a connection.`;
      } else {
        status = `${item.name} was not restocked; see Not yet recorded.`;
      }
    } finally {
      busy = '';
    }
  }

  function level(item: ShoppingItem): string {
    const unit = item.unit ? ` ${item.unit}` : item.category_id ? ' assets' : '';
    return item.threshold != null ? `${item.level} of ${item.threshold}${unit}` : `${item.level}${unit}`;
  }

  function pack(item: ShoppingItem): string {
    const v = item.vendor!;
    const parts = [v.vendor_name];
    if (v.part_number) parts.push(v.part_number);
    if (v.pack_size) parts.push(`packs of ${v.pack_size}${v.pack_unit ? ` ${v.pack_unit}` : ''}`);
    if (v.price) parts.push(`${v.price} ${v.currency ?? ''}`.trim());
    return parts.join(' · ');
  }
</script>

<PageHeader title="Shopping list" lede="What is running low, by category, with where to buy it. Check off what you bought to restock it, even with no signal.">
  {#snippet actions()}
    {#if list?.can_order && !fromCopy}<a class="btn-secondary" href="/orders/new?from=running-low">Start orders</a>{/if}
  {/snippet}
</PageHeader>

<ErrorMessage {error} />
{#if fromCopy && list}
  <p role="status" class="mb-4 rounded border-2 border-zinc-900 bg-highlight px-3 py-2 text-sm text-zinc-900 dark:border-zinc-600">
    You're offline. This is the list as it was {ago(list.made_at)}, from the copy kept on this device.
  </p>
{/if}
{#if status}<p class="mb-4 text-sm text-green-700 dark:text-green-400" role="status">{status}</p>{/if}

<div class="space-y-6">
  <QueuedChanges />
  {#if list && !list.items.length}
    <p>Nothing is running low.</p>
  {/if}
  {#each groups as g, i (g.group)}
    <section class="space-y-3" aria-labelledby="shopping-group-{i}">
      <h2 id="shopping-group-{i}" class="text-lg font-semibold">{g.group || 'In no category'}</h2>
      <ul class="space-y-3">
        {#each g.items as item ((item.asset_id ?? '') + (item.category_id ?? ''))}
          {@const pending = item.asset_id ? pendingFor(offline.queue, item.asset_id) : 0}
          <li class="card space-y-2">
            <div class="flex flex-wrap items-baseline justify-between gap-x-4">
              <p class="font-medium">
                {#if fromCopy || !item.asset_id}{item.name}{:else}<a class={link} href="/assets/{encodeURIComponent(item.asset_id)}">{item.name}</a>{/if}
              </p>
              <p class="text-sm">{level(item)}{#if pending}<span class="text-zinc-600 dark:text-zinc-400"> · +{pending} waiting to send</span>{/if}</p>
            </div>
            {#if item.where}<p class="text-sm text-zinc-600 dark:text-zinc-400">In {item.where}</p>{/if}
            {#if item.vendor}
              <p class="text-sm">
                {#if item.vendor.url && !fromCopy}<a class={link} href={item.vendor.url} rel="noopener noreferrer" target="_blank">{pack(item)}</a>{:else}{pack(item)}{/if}
              </p>
            {/if}
            {#if item.on_order}
              <p class="text-sm">
                Already on order:
                {#if fromCopy}{item.on_order.reference}{:else}<a class={link} href="/orders/{encodeURIComponent(item.on_order.id)}">{item.on_order.reference}</a>{/if}
              </p>
            {/if}
            {#if item.asset_id && item.can_restock}
              <form class="flex flex-wrap items-end gap-2" onsubmit={(e) => bought(e, item)}>
                <div>
                  <label class="label" for="bought-{item.asset_id}">Bought{item.unit ? ` (${item.unit})` : ''}</label>
                  <input id="bought-{item.asset_id}" class="input w-28" type="number" min="1" step="1" required bind:value={amounts[item.asset_id]} />
                </div>
                <button type="submit" class="btn-secondary" disabled={busy === item.asset_id}>Check off {item.name}</button>
              </form>
            {:else if item.category_id && !fromCopy && session.can('assets.write')}
              <p class="text-sm">
                Counted by its assets: <a class={link} href="/assets/new?category={encodeURIComponent(item.category_id)}">add what you bought</a>.
              </p>
            {/if}
          </li>
        {/each}
      </ul>
    </section>
  {/each}
</div>
