<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { byEnd, contractEnded, coverageText, kindLabel } from '../../lib/contracts';
  import { messageOf } from '../../lib/errors';
  import type { Contract } from '../../lib/models';
  import { navigate, router } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  // /contracts: the contracts someone may see (docs/contracts.md), by when
  // they end, and a search through them.
  let { params: _params }: { params: Record<string, string> } = $props();

  let contracts = $state<Contract[]>([]);
  let loading = $state(true);
  let error = $state('');
  let search = $state(router.query.get('q') ?? '');
  // The search the list shows.
  let applied = $state(router.query.get('q') ?? '');
  // Soonest first, unless turned round; those with no end stay last.
  let descending = $state(router.query.get('sort') === 'desc');
  const sorted = $derived(byEnd(contracts, descending));

  onMount(() => void load());

  // Each load is numbered, so a slow answer to an older one cannot replace
  // the list a newer one asked for.
  let loads = 0;

  async function load() {
    const mine = ++loads;
    loading = true;
    error = '';
    const q = search.trim();
    applied = q;
    try {
      const found = (await request<{ contracts: Contract[] }>('GET', q ? `/contracts?q=${encodeURIComponent(q)}` : '/contracts')).contracts ?? [];
      if (mine !== loads) return;
      contracts = found;
    } catch (err) {
      if (mine === loads) error = messageOf(err);
    } finally {
      if (mine === loads) loading = false;
    }
  }

  // Keep the search and the order in the address, so they survive reloads and links.
  function address() {
    const q = search.trim();
    const parts = [q ? `q=${encodeURIComponent(q)}` : '', descending ? 'sort=desc' : ''].filter(Boolean);
    return '/contracts' + (parts.length ? `?${parts.join('&')}` : '');
  }

  function applySearch(event: SubmitEvent) {
    event.preventDefault();
    navigate(address(), true);
    void load();
  }

  function turn() {
    descending = !descending;
    navigate(address(), true);
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
  <div>
    <h1 class="text-2xl font-bold">Contracts</h1>
    <p class="text-sm text-zinc-600 dark:text-zinc-400">Warranties, service and maintenance agreements, leases and licenses, by when they end.</p>
  </div>
  {#if session.can('vendors.manage')}
    <a class="btn-primary" href="/contracts/new">New contract</a>
  {/if}
</div>

<form class="mb-4 flex flex-wrap items-end gap-3" role="search" onsubmit={applySearch}>
  <div class="min-w-48 flex-1">
    <label class="label" for="contract-search">Search</label>
    <input id="contract-search" class="input" type="search" placeholder="Name, kind, reference, vendor or notes" bind:value={search} />
  </div>
  <button type="submit" class="btn-secondary">Search</button>
</form>

<ErrorMessage {error} />

{#if loading}
  <p role="status" class="text-sm text-zinc-600 dark:text-zinc-400">Loading contracts…</p>
{:else if contracts.length === 0}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">{applied ? 'No contracts match.' : 'No contracts yet.'}</p>
{:else}
  <div class="overflow-x-auto">
    <table class="w-full text-left text-sm" aria-label="Contracts">
      <thead>
        <tr class="border-b border-zinc-300 dark:border-zinc-700">
          <th scope="col" class="py-2 pr-4 font-semibold">Contract</th>
          <th scope="col" class="py-2 pr-4 font-semibold">Kind</th>
          <th scope="col" class="py-2 pr-4 font-semibold">Vendor</th>
          <th scope="col" class="py-2 pr-4 font-semibold" aria-sort={descending ? 'descending' : 'ascending'}>
            <button type="button" class="inline-flex min-h-11 items-center gap-1 font-semibold underline" onclick={turn}>
              Ends <span aria-hidden="true">{descending ? '↓' : '↑'}</span><span class="sr-only">{descending ? ', latest first' : ', soonest first'}</span>
            </button>
          </th>
          <th scope="col" class="py-2 font-semibold">Covers</th>
        </tr>
      </thead>
      <tbody>
        {#each sorted as c (c.id)}
          <tr class="border-b border-zinc-200 dark:border-zinc-800">
            <td class="pr-4"><a class="inline-flex min-h-11 items-center font-medium underline" href="/contracts/{encodeURIComponent(c.id)}">{c.name}</a></td>
            <td class="pr-4">{kindLabel(c.kind)}</td>
            <td class="pr-4">{c.vendor_name}</td>
            <td class="pr-4">
              {#if c.end_date}
                <span class={contractEnded(c) ? 'text-zinc-600 dark:text-zinc-400' : ''}>{c.end_date}{contractEnded(c) ? ' (ended)' : ''}</span>
                {#if c.auto_renews}
                  <span class="block text-xs text-zinc-600 dark:text-zinc-400">Renews automatically{c.notice_deadline ? `; notice by ${c.notice_deadline}` : ''}</span>
                {/if}
              {:else}
                <span class="text-zinc-600 dark:text-zinc-400">No end</span>
              {/if}
            </td>
            <td>{coverageText(c)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}
